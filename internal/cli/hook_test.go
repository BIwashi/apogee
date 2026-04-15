package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// newHookServer spins up a test HTTP server and captures every request
// body indexed by path. Use stop() to shut it down and access events /
// claims / delivered maps while the server is idle.
type hookServer struct {
	t *testing.T

	srv *httptest.Server

	mu        sync.Mutex
	reqs      map[string][][]byte
	claim     func() (int, []byte) // per-request claim override
	delivered func()               // hook for delivered requests
	events    func(int) (int, []byte)
	eventsN   int
}

func newHookServer(t *testing.T) *hookServer {
	t.Helper()
	h := &hookServer{t: t, reqs: map[string][][]byte{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/events", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		h.record(r.URL.Path, body)
		h.mu.Lock()
		h.eventsN++
		n := h.eventsN
		cb := h.events
		h.mu.Unlock()
		if cb != nil {
			status, respBody := cb(n)
			w.WriteHeader(status)
			_, _ = w.Write(respBody)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/v1/sessions/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		h.record(r.URL.Path, body)
		h.mu.Lock()
		cb := h.claim
		h.mu.Unlock()
		if cb == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		status, respBody := cb()
		w.WriteHeader(status)
		_, _ = w.Write(respBody)
	})
	mux.HandleFunc("/v1/interventions/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		h.record(r.URL.Path, body)
		h.mu.Lock()
		cb := h.delivered
		h.mu.Unlock()
		if cb != nil {
			cb()
		}
		w.WriteHeader(http.StatusOK)
	})
	h.srv = httptest.NewServer(mux)
	t.Cleanup(h.srv.Close)
	return h
}

func (h *hookServer) record(path string, body []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.reqs[path] = append(h.reqs[path], body)
}

func (h *hookServer) requestsMatching(prefix string) [][]byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out [][]byte
	for p, reqs := range h.reqs {
		if strings.HasPrefix(p, prefix) {
			out = append(out, reqs...)
		}
	}
	return out
}

func (h *hookServer) URL() string { return h.srv.URL + "/v1/events" }

func newHookOpts(t *testing.T, srv *hookServer, event, stdin string) *hookOptions {
	t.Helper()
	return &hookOptions{
		Event:     event,
		ServerURL: srv.URL(),
		SourceApp: "pinned-app",
		Timeout:   500 * time.Millisecond,
		Stdin:     strings.NewReader(stdin),
		Stdout:    &bytes.Buffer{},
		Stderr:    &bytes.Buffer{},
		HTTPClient: &http.Client{
			Timeout: time.Second,
		},
		NowMillis:       func() int64 { return 1_700_000_000_000 },
		DeriveSourceApp: func() string { return "derive-should-not-fire" },
	}
}

func decodeLastEvent(t *testing.T, srv *hookServer) map[string]any {
	t.Helper()
	reqs := srv.requestsMatching("/v1/events")
	if len(reqs) == 0 {
		t.Fatalf("no events POST observed")
	}
	var out map[string]any
	if err := json.Unmarshal(reqs[len(reqs)-1], &out); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	return out
}

func TestRunHook_DefaultSourceAppIsDerived(t *testing.T) {
	srv := newHookServer(t)
	opts := newHookOpts(t, srv, "PostToolUse", `{"session_id":"sess-1"}`)
	opts.SourceApp = ""
	derived := "derive-fired"
	opts.DeriveSourceApp = func() string { return derived }

	if err := runHook(t.Context(), opts); err != nil {
		t.Fatalf("runHook: %v", err)
	}
	body := decodeLastEvent(t, srv)
	if body["source_app"] != derived {
		t.Errorf("source_app: got %v, want %q", body["source_app"], derived)
	}
}

func TestRunHook_PinnedSourceAppOverridesDerive(t *testing.T) {
	srv := newHookServer(t)
	opts := newHookOpts(t, srv, "PostToolUse", `{"session_id":"sess-1"}`)
	opts.SourceApp = "pinned"
	opts.DeriveSourceApp = func() string {
		t.Fatal("DeriveSourceApp should not be called when SourceApp is pinned")
		return ""
	}
	if err := runHook(t.Context(), opts); err != nil {
		t.Fatalf("runHook: %v", err)
	}
	body := decodeLastEvent(t, srv)
	if body["source_app"] != "pinned" {
		t.Errorf("source_app: got %v, want %q", body["source_app"], "pinned")
	}
}

func TestRunHook_FlattenFields(t *testing.T) {
	srv := newHookServer(t)
	stdin := `{"session_id":"sess-1","tool_name":"Bash","tool_use_id":"toolu-1","error":"boom","prompt":"write more tests"}`
	opts := newHookOpts(t, srv, "PostToolUse", stdin)
	if err := runHook(t.Context(), opts); err != nil {
		t.Fatalf("runHook: %v", err)
	}
	body := decodeLastEvent(t, srv)
	for _, k := range []string{"tool_name", "tool_use_id", "error", "prompt"} {
		if _, ok := body[k]; !ok {
			t.Errorf("flat field %q missing from event body", k)
		}
	}
	if body["tool_name"] != "Bash" {
		t.Errorf("tool_name wrong: %v", body["tool_name"])
	}
	if body["tool_use_id"] != "toolu-1" {
		t.Errorf("tool_use_id wrong: %v", body["tool_use_id"])
	}
	if body["error"] != "boom" {
		t.Errorf("error wrong: %v", body["error"])
	}
	if body["prompt"] != "write more tests" {
		t.Errorf("prompt wrong: %v", body["prompt"])
	}
	if body["session_id"] != "sess-1" {
		t.Errorf("session_id wrong: %v", body["session_id"])
	}
	if _, ok := body["payload"]; !ok {
		t.Errorf("event body missing payload: %v", body)
	}
}

func TestRunHook_StdinEcho(t *testing.T) {
	srv := newHookServer(t)
	stdin := `{"session_id":"sess-1","tool_name":"Read"}`
	opts := newHookOpts(t, srv, "PostToolUse", stdin)
	if err := runHook(t.Context(), opts); err != nil {
		t.Fatalf("runHook: %v", err)
	}
	out := opts.Stdout.(*bytes.Buffer).String()
	if out != stdin {
		t.Errorf("stdout should echo stdin verbatim.\n got: %q\nwant: %q", out, stdin)
	}
}

// TestRunHook_SkipEnvVarShortCircuits guards the summarizer
// feedback-loop guard: when the APOGEE_HOOK_SKIP=1 env var is set on
// the current process, runHook mirrors stdin and returns without
// POSTing to /v1/events. Exists because the summarizer spawns
// `claude` subprocesses that inherit ~/.claude/settings.json and
// would otherwise re-ingest their own telemetry into apogee under a
// fake ".apogee" source_app, inflating the sessions/agents lists
// with synthetic rows.
func TestRunHook_SkipEnvVarShortCircuits(t *testing.T) {
	t.Setenv("APOGEE_HOOK_SKIP", "1")
	srv := newHookServer(t)
	stdin := `{"session_id":"sess-1","tool_name":"Read"}`
	opts := newHookOpts(t, srv, "PostToolUse", stdin)
	if err := runHook(t.Context(), opts); err != nil {
		t.Fatalf("runHook: %v", err)
	}
	// stdin must still be echoed — the Claude Code pipeline expects
	// every hook to be a passthrough even when skipping apogee.
	out := opts.Stdout.(*bytes.Buffer).String()
	if out != stdin {
		t.Errorf("stdout should echo stdin verbatim.\n got: %q\nwant: %q", out, stdin)
	}
	// But no event POST should have happened.
	if len(srv.requestsMatching("/v1/events")) != 0 {
		t.Errorf("events POST should be skipped under APOGEE_HOOK_SKIP=1, got %d requests",
			len(srv.requestsMatching("/v1/events")))
	}
}

func TestRunHook_InterruptInterventionEmitsDecisionJSON(t *testing.T) {
	srv := newHookServer(t)
	srv.claim = func() (int, []byte) {
		body, _ := json.Marshal(map[string]any{
			"intervention": map[string]any{
				"intervention_id": "iv-1",
				"message":         "stop and reconsider",
				"delivery_mode":   "interrupt",
			},
		})
		return http.StatusOK, body
	}
	delivered := make(chan struct{}, 1)
	srv.delivered = func() { delivered <- struct{}{} }
	stdin := `{"session_id":"sess-1","turn_id":"turn-1","tool_name":"Bash"}`
	opts := newHookOpts(t, srv, "PreToolUse", stdin)
	if err := runHook(t.Context(), opts); err != nil {
		t.Fatalf("runHook: %v", err)
	}
	out := strings.TrimSpace(opts.Stdout.(*bytes.Buffer).String())
	want := `{"decision":"block","reason":"stop and reconsider"}`
	if out != want {
		t.Errorf("stdout: got %q, want %q", out, want)
	}
	// The events POST should still have happened.
	if len(srv.requestsMatching("/v1/events")) != 1 {
		t.Errorf("events POST did not fire; got %d requests", len(srv.requestsMatching("/v1/events")))
	}
	// The delivered callback should have fired.
	select {
	case <-delivered:
	case <-time.After(200 * time.Millisecond):
		t.Errorf("delivered callback did not fire within deadline")
	}
}

func TestRunHook_ContextInterventionEmitsAdditionalContext(t *testing.T) {
	srv := newHookServer(t)
	srv.claim = func() (int, []byte) {
		body, _ := json.Marshal(map[string]any{
			"intervention": map[string]any{
				"intervention_id": "iv-2",
				"message":         "heads up",
				"delivery_mode":   "context",
			},
		})
		return http.StatusOK, body
	}
	stdin := `{"session_id":"sess-1"}`
	opts := newHookOpts(t, srv, "UserPromptSubmit", stdin)
	if err := runHook(t.Context(), opts); err != nil {
		t.Fatalf("runHook: %v", err)
	}
	out := strings.TrimSpace(opts.Stdout.(*bytes.Buffer).String())
	if !strings.Contains(out, `"hookSpecificOutput"`) {
		t.Errorf("stdout missing hookSpecificOutput: %q", out)
	}
	if !strings.Contains(out, `"additionalContext":"heads up"`) {
		t.Errorf("stdout missing additionalContext: %q", out)
	}
}

func TestRunHook_BothModePreToolUse(t *testing.T) {
	srv := newHookServer(t)
	srv.claim = func() (int, []byte) {
		body, _ := json.Marshal(map[string]any{
			"intervention": map[string]any{
				"intervention_id": "iv-3",
				"message":         "switch delivery",
				"delivery_mode":   "both",
			},
		})
		return http.StatusOK, body
	}
	stdin := `{"session_id":"sess-1"}`
	opts := newHookOpts(t, srv, "PreToolUse", stdin)
	if err := runHook(t.Context(), opts); err != nil {
		t.Fatalf("runHook: %v", err)
	}
	out := strings.TrimSpace(opts.Stdout.(*bytes.Buffer).String())
	if !strings.Contains(out, `"decision":"block"`) {
		t.Errorf("both-mode on PreToolUse should emit a block decision: %q", out)
	}
}

func TestRunHook_NoClaimOnPostToolUse(t *testing.T) {
	srv := newHookServer(t)
	srv.claim = func() (int, []byte) {
		t.Fatal("claim endpoint must not be called for PostToolUse")
		return 500, nil
	}
	stdin := `{"session_id":"sess-1"}`
	opts := newHookOpts(t, srv, "PostToolUse", stdin)
	if err := runHook(t.Context(), opts); err != nil {
		t.Fatalf("runHook: %v", err)
	}
	if len(srv.requestsMatching("/v1/sessions")) != 0 {
		t.Errorf("claim endpoint should not be hit on PostToolUse")
	}
	// Stdin is still echoed verbatim.
	if opts.Stdout.(*bytes.Buffer).String() != stdin {
		t.Errorf("stdin should be echoed on PostToolUse")
	}
}

func TestRunHook_NetworkErrorDoesNotFail(t *testing.T) {
	srv := newHookServer(t)
	srv.events = func(_ int) (int, []byte) {
		return http.StatusInternalServerError, []byte(`{"error":"boom"}`)
	}
	stdin := `{"session_id":"sess-1"}`
	opts := newHookOpts(t, srv, "PostToolUse", stdin)
	if err := runHook(t.Context(), opts); err != nil {
		t.Fatalf("runHook should not return an error on HTTP 500: %v", err)
	}
	stderr := opts.Stderr.(*bytes.Buffer).String()
	if !strings.Contains(stderr, "HTTP 500") {
		t.Errorf("stderr should mention HTTP 500: %q", stderr)
	}
}

func TestRunHook_BlankStdin(t *testing.T) {
	srv := newHookServer(t)
	opts := newHookOpts(t, srv, "SessionStart", "")
	if err := runHook(t.Context(), opts); err != nil {
		t.Fatalf("runHook: %v", err)
	}
	body := decodeLastEvent(t, srv)
	if body["session_id"] != "unknown" {
		t.Errorf("session_id: got %v, want %q", body["session_id"], "unknown")
	}
	if body["hook_event_type"] != "SessionStart" {
		t.Errorf("hook_event_type: got %v", body["hook_event_type"])
	}
	if opts.Stdout.(*bytes.Buffer).String() != "" {
		t.Errorf("stdout should be empty on blank stdin, got %q", opts.Stdout.(*bytes.Buffer).String())
	}
}

func TestRunHook_InvalidJSONStdin(t *testing.T) {
	srv := newHookServer(t)
	opts := newHookOpts(t, srv, "SessionStart", `{not valid json`)
	if err := runHook(t.Context(), opts); err != nil {
		t.Fatalf("runHook: %v", err)
	}
	body := decodeLastEvent(t, srv)
	if body["session_id"] != "unknown" {
		t.Errorf("session_id: got %v, want %q", body["session_id"], "unknown")
	}
	stderr := opts.Stderr.(*bytes.Buffer).String()
	if !strings.Contains(stderr, "invalid JSON") {
		t.Errorf("stderr should mention invalid JSON: %q", stderr)
	}
}

func TestRunHook_NonObjectStdin(t *testing.T) {
	srv := newHookServer(t)
	opts := newHookOpts(t, srv, "SessionStart", `["array", "payload"]`)
	if err := runHook(t.Context(), opts); err != nil {
		t.Fatalf("runHook: %v", err)
	}
	body := decodeLastEvent(t, srv)
	if body["session_id"] != "unknown" {
		t.Errorf("session_id: got %v, want %q", body["session_id"], "unknown")
	}
	stderr := opts.Stderr.(*bytes.Buffer).String()
	if !strings.Contains(stderr, "expected JSON object") {
		t.Errorf("stderr should mention expected JSON object: %q", stderr)
	}
}

func TestRunHook_DeliveredCallback(t *testing.T) {
	srv := newHookServer(t)
	srv.claim = func() (int, []byte) {
		body, _ := json.Marshal(map[string]any{
			"intervention": map[string]any{
				"intervention_id": "iv-delivered",
				"message":         "hello",
				"delivery_mode":   "interrupt",
			},
		})
		return http.StatusOK, body
	}
	received := make(chan struct{}, 1)
	srv.delivered = func() { received <- struct{}{} }
	stdin := `{"session_id":"sess-1"}`
	opts := newHookOpts(t, srv, "PreToolUse", stdin)
	if err := runHook(t.Context(), opts); err != nil {
		t.Fatalf("runHook: %v", err)
	}
	select {
	case <-received:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("delivered callback was not observed")
	}
	// Check the delivered request targeted the right ID.
	for _, b := range srv.requestsMatching("/v1/interventions/iv-delivered/delivered") {
		var body map[string]any
		if err := json.Unmarshal(b, &body); err != nil {
			t.Fatalf("decode delivered body: %v", err)
		}
		if body["hook_event"] != "PreToolUse" {
			t.Errorf("delivered body hook_event: got %v", body["hook_event"])
		}
	}
}

func TestRunHook_ClaimNoInterventionFallsThroughToEcho(t *testing.T) {
	srv := newHookServer(t)
	srv.claim = func() (int, []byte) {
		return http.StatusNoContent, nil
	}
	stdin := `{"session_id":"sess-1"}`
	opts := newHookOpts(t, srv, "PreToolUse", stdin)
	if err := runHook(t.Context(), opts); err != nil {
		t.Fatalf("runHook: %v", err)
	}
	out := opts.Stdout.(*bytes.Buffer).String()
	if out != stdin {
		t.Errorf("stdin should be echoed when claim returns 204; got %q", out)
	}
}

func TestRunHook_ContextModeOnPreToolUseIsNoDecision(t *testing.T) {
	// A context-mode intervention claimed on PreToolUse should NOT emit a
	// decision — the hook writes the stdin echo instead.
	srv := newHookServer(t)
	srv.claim = func() (int, []byte) {
		body, _ := json.Marshal(map[string]any{
			"intervention": map[string]any{
				"intervention_id": "iv-5",
				"message":         "note only",
				"delivery_mode":   "context",
			},
		})
		return http.StatusOK, body
	}
	stdin := `{"session_id":"sess-1"}`
	opts := newHookOpts(t, srv, "PreToolUse", stdin)
	if err := runHook(t.Context(), opts); err != nil {
		t.Fatalf("runHook: %v", err)
	}
	out := opts.Stdout.(*bytes.Buffer).String()
	if out != stdin {
		t.Errorf("expected passthrough echo, got %q", out)
	}
}

func TestStripEventsSuffix(t *testing.T) {
	cases := map[string]string{
		"http://localhost:4100/v1/events":  "http://localhost:4100",
		"http://localhost:4100/v1/events/": "http://localhost:4100",
		"http://localhost:4100":            "http://localhost:4100",
		"http://localhost:4100/":           "http://localhost:4100",
		"":                                 "",
		"http://proxy/api/v1/events/":      "http://proxy/api",
	}
	for in, want := range cases {
		if got := stripEventsSuffix(in); got != want {
			t.Errorf("stripEventsSuffix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRunHook_ExitCodeAlwaysZeroOnClientError(t *testing.T) {
	// Point at a closed port so every HTTP call fails fast.
	opts := &hookOptions{
		Event:     "PostToolUse",
		ServerURL: "http://127.0.0.1:1/v1/events",
		SourceApp: "pinned",
		Timeout:   50 * time.Millisecond,
		Stdin:     strings.NewReader(`{"session_id":"sess-1"}`),
		Stdout:    &bytes.Buffer{},
		Stderr:    &bytes.Buffer{},
		HTTPClient: &http.Client{
			Timeout: 50 * time.Millisecond,
		},
		NowMillis:       func() int64 { return 1 },
		DeriveSourceApp: func() string { return "x" },
	}
	if err := runHook(t.Context(), opts); err != nil {
		t.Errorf("runHook should swallow dial errors, got %v", err)
	}
	// Stdin still echoed.
	if opts.Stdout.(*bytes.Buffer).String() != `{"session_id":"sess-1"}` {
		t.Errorf("stdout should echo stdin even on network error")
	}
}

// Compile-time assertion that NewHookCmd is sane: runs --help through
// the cobra command tree.
func TestNewHookCmd_HelpRuns(t *testing.T) {
	cmd := NewHookCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("hook --help: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"--event", "--server-url", "--source-app", "--timeout"} {
		if !strings.Contains(out, want) {
			t.Errorf("hook --help output missing %q: %q", want, out)
		}
	}
}

// Make fmt usable inside quick sanity checks that don't need real data.
var _ = fmt.Sprintf

func TestDeriveSourceAppRuntime_EnvOverride(t *testing.T) {
	t.Setenv("APOGEE_SOURCE_APP", "  pinned-via-env  ")
	if got := deriveSourceAppRuntime(); got != "pinned-via-env" {
		t.Errorf("env override: got %q, want %q", got, "pinned-via-env")
	}
}

func TestDeriveSourceAppRuntime_FallsBackToCwdOrUnknown(t *testing.T) {
	t.Setenv("APOGEE_SOURCE_APP", "")
	// Force git lookup to fail by pointing PATH somewhere with no git.
	t.Setenv("PATH", "/nonexistent-for-tests")
	got := deriveSourceAppRuntime()
	// Should be either the CWD basename or "unknown"; never empty.
	if got == "" {
		t.Errorf("derive returned empty string")
	}
}

func TestGitToplevelBasename_Empty(t *testing.T) {
	t.Setenv("PATH", "/nonexistent-for-tests")
	if got := gitToplevelBasename(); got != "" {
		t.Errorf("git toplevel with broken PATH should be empty, got %q", got)
	}
}

func TestHookPayloadRaw_NonObjectFallsBackToMap(t *testing.T) {
	in := map[string]any{"a": 1}
	raw := json.RawMessage(`"just a string"`)
	got := hookPayloadRaw(in, raw)
	if len(got) == 0 {
		t.Fatal("expected non-nil raw payload from map fallback")
	}
	var decoded map[string]any
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := decoded["a"]; !ok {
		t.Errorf("fallback payload missing map key: %v", decoded)
	}
}

func TestHookPayloadRaw_EmptyEverything(t *testing.T) {
	if got := hookPayloadRaw(nil, nil); got != nil {
		t.Errorf("expected nil payload when both inputs are empty, got %s", string(got))
	}
}

func TestRunHook_NilOptsError(t *testing.T) {
	if err := runHook(t.Context(), nil); err == nil {
		t.Error("nil opts should return an error (programmer-level)")
	}
}

func TestRunHook_DefaultSourceAppFallsBackToUnknown(t *testing.T) {
	srv := newHookServer(t)
	opts := newHookOpts(t, srv, "PostToolUse", `{"session_id":"sess-1"}`)
	opts.SourceApp = ""
	opts.DeriveSourceApp = func() string { return "" } // every probe returned empty
	if err := runHook(t.Context(), opts); err != nil {
		t.Fatalf("runHook: %v", err)
	}
	body := decodeLastEvent(t, srv)
	if body["source_app"] != "unknown" {
		t.Errorf("source_app fallback: got %v, want %q", body["source_app"], "unknown")
	}
}
