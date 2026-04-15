package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// Hook claim / delivery constants.
//
// Only a subset of Claude Code hook events can carry an operator
// intervention decision. The rest of the hook lifecycle is pure
// telemetry and the claim step is skipped entirely for those events.
const (
	hookEventPreToolUse       = "PreToolUse"
	hookEventUserPromptSubmit = "UserPromptSubmit"

	interventionModeInterrupt = "interrupt"
	interventionModeContext   = "context"
	interventionModeBoth      = "both"

	// DefaultHookTimeout is the per-POST HTTP timeout for the events
	// endpoint. Short on purpose — the hook runs inline with Claude Code
	// and any blocking here is user-visible latency.
	DefaultHookTimeout = 2 * time.Second

	// Interventions get their own tighter budget so they never dominate
	// the overall hook latency. The claim+deliver pair happens before the
	// events POST on PreToolUse / UserPromptSubmit.
	interventionTimeout = 1500 * time.Millisecond

	// Fallback label stamped on events when every source_app derivation
	// step fails (no env var, no git repo, no CWD).
	defaultHookSourceApp = "unknown"

	hookUserAgent = "apogee-hook/0.0.0-dev"

	// HookSkipEnvVar is the environment variable the apogee hook honors
	// to short-circuit. When it is set to "1" the hook mirrors stdin to
	// stdout (so the Claude Code pipeline keeps flowing) and returns
	// without POSTing a telemetry event or claiming an intervention.
	//
	// Why this exists: the apogee summarizer spawns `claude` subprocesses
	// to generate per-turn recaps, session rollups, and phase narratives.
	// Those subprocesses inherit `~/.claude/settings.json`, which points
	// every hook event back at `apogee hook`, which POSTs to /v1/events,
	// which ingests as a new fake "session" under
	// source_app=".apogee" (the cwd basename of the daemon's working
	// directory). Left unchecked the sessions and agents lists balloon
	// with synthetic rows and the summarizer recurses on its own output.
	//
	// The summarizer runner sets HookSkipEnvVar=1 on its child process
	// env so apogee's own hook invocations silently skip the POST. The
	// value intentionally does NOT affect other Claude Code hook
	// implementations — they won't read this var.
	HookSkipEnvVar = "APOGEE_HOOK_SKIP"
)

// flatHookFields lists the top-level keys promoted out of the Claude Code
// hook payload onto HookEvent's top-level JSON object. Keep in sync with
// internal/ingest/payload.go::HookEvent and the disler reference list.
var flatHookFields = []string{
	"tool_name",
	"tool_use_id",
	"error",
	"is_interrupt",
	"permission_suggestions",
	"agent_id",
	"agent_type",
	"agent_transcript_path",
	"stop_hook_active",
	"notification_type",
	"custom_instructions",
	"source",
	"reason",
	"model_name",
	"prompt",
}

// hookOptions captures every knob of `apogee hook`. The network + time +
// derivation side-effects are injected as function values so hook_test.go
// can stub them without poking globals.
type hookOptions struct {
	Event     string
	ServerURL string
	SourceApp string
	Timeout   time.Duration

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	// HTTPClient is reused for every network call (events POST, claim,
	// delivered). When nil, runHook constructs a default client with
	// Timeout=Timeout. Tests inject their own to point at httptest.
	HTTPClient *http.Client

	// NowMillis returns the event timestamp in ms since epoch.
	NowMillis func() int64

	// DeriveSourceApp is the runtime derivation function invoked when
	// SourceApp is empty. Defaults to deriveSourceAppRuntime.
	DeriveSourceApp func() string
}

// NewHookCmd builds the `apogee hook` cobra subcommand. The binary is the
// hook entry point written into .claude/settings.json by `apogee init`.
//
// Reads a Claude Code hook payload from stdin, optionally claims an
// operator intervention, writes the Claude Code decision / passthrough
// JSON to stdout, and POSTs the telemetry event to the collector. Exit
// code is always 0 — a failing hook would break Claude Code and there
// is no user-facing benefit to surfacing transport errors.
func NewHookCmd() *cobra.Command {
	opts := &hookOptions{}
	cmd := &cobra.Command{
		Use:   "hook --event <HookEventType> [flags]",
		Short: "Forward a Claude Code hook payload to the apogee collector",
		Long: `Forward a Claude Code hook payload to the apogee collector.

Reads a JSON hook payload from stdin, forwards it to the apogee collector
via POST /v1/events, and echoes stdin back to stdout so the rest of the
Claude Code hook pipeline is unaffected.

On PreToolUse and UserPromptSubmit, the hook first calls
POST /v1/sessions/<session_id>/interventions/claim and, on success,
writes the Claude Code decision JSON to stdout in place of the echo.

Exit code is always 0: a failing hook would break Claude Code and there
is no user-facing benefit to surfacing transport errors.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.Stdin = cmd.InOrStdin()
			opts.Stdout = cmd.OutOrStdout()
			opts.Stderr = cmd.ErrOrStderr()
			return runHook(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.Event, "event", "", "Hook event name (e.g. PreToolUse, PostToolUse, UserPromptSubmit)")
	cmd.Flags().StringVar(&opts.ServerURL, "server-url", DefaultServerURL, "Collector endpoint")
	cmd.Flags().StringVar(&opts.SourceApp, "source-app", "", "Pin the source_app label. Leave empty to derive from $APOGEE_SOURCE_APP → git toplevel → $PWD.")
	cmd.Flags().DurationVar(&opts.Timeout, "timeout", DefaultHookTimeout, "HTTP timeout for the events POST")
	_ = cmd.MarkFlagRequired("event")
	return cmd
}

// runHook is the testable entry point. It never returns a non-nil error
// except for programmer-level mistakes (like a nil writer). Every network
// or decode failure is logged to the injected stderr and swallowed.
func runHook(ctx context.Context, opts *hookOptions) error {
	if opts == nil {
		return fmt.Errorf("hook: options are nil")
	}
	if opts.Stdin == nil {
		opts.Stdin = os.Stdin
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultHookTimeout
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: opts.Timeout}
	}
	if opts.NowMillis == nil {
		opts.NowMillis = func() int64 { return time.Now().UnixMilli() }
	}
	if opts.DeriveSourceApp == nil {
		opts.DeriveSourceApp = deriveSourceAppRuntime
	}
	if opts.ServerURL == "" {
		opts.ServerURL = DefaultServerURL
	}
	if ctx == nil {
		ctx = context.Background()
	}

	rawStdin, err := io.ReadAll(opts.Stdin)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "apogee hook: failed to read stdin: %v\n", err)
		rawStdin = nil
	}

	// Short-circuit when the parent process set APOGEE_HOOK_SKIP=1.
	// This is the feedback-loop guard for the summarizer subprocess:
	// apogee's own claude spawns should not post their own hook
	// events back into /v1/events. We still mirror stdin so the
	// Claude Code pipeline behaves the same as a passthrough hook;
	// only the telemetry POST and the intervention claim are skipped.
	if os.Getenv(HookSkipEnvVar) == "1" {
		if len(rawStdin) > 0 {
			if _, werr := opts.Stdout.Write(rawStdin); werr != nil {
				fmt.Fprintf(opts.Stderr, "apogee hook: failed to echo stdin: %v\n", werr)
			}
		}
		return nil
	}

	inputData := parseHookInput(rawStdin, opts.Stderr)

	sourceApp := opts.SourceApp
	if sourceApp == "" {
		sourceApp = opts.DeriveSourceApp()
	}
	if sourceApp == "" {
		sourceApp = defaultHookSourceApp
	}

	// Step 1: try to claim an operator intervention. Only PreToolUse and
	// UserPromptSubmit can carry a decision, so other events skip this
	// step entirely.
	decisionJSON := maybeClaimIntervention(ctx, opts, inputData)

	// Step 2: write stdout. Decision JSON replaces the passthrough echo
	// when a claim succeeded, otherwise mirror stdin verbatim so the rest
	// of the hook pipeline sees the same payload we received.
	if decisionJSON != nil {
		if _, werr := opts.Stdout.Write(decisionJSON); werr != nil {
			fmt.Fprintf(opts.Stderr, "apogee hook: failed to write decision: %v\n", werr)
		}
		if !bytes.HasSuffix(decisionJSON, []byte("\n")) {
			_, _ = opts.Stdout.Write([]byte("\n"))
		}
	} else if len(rawStdin) > 0 {
		if _, werr := opts.Stdout.Write(rawStdin); werr != nil {
			fmt.Fprintf(opts.Stderr, "apogee hook: failed to echo stdin: %v\n", werr)
		}
	}

	// Step 3: POST the event. Always happens, even when we emitted a
	// decision — the collector still wants the telemetry row.
	postHookEvent(ctx, opts, sourceApp, inputData, rawStdin)

	return nil
}

// parseHookInput unmarshals a JSON object from the raw stdin bytes. An
// empty body, non-JSON, or non-object payload all degrade to an empty
// map. The error is logged but never returned.
func parseHookInput(raw []byte, stderr io.Writer) map[string]any {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return map[string]any{}
	}
	var parsed any
	if err := json.Unmarshal(trimmed, &parsed); err != nil {
		fmt.Fprintf(stderr, "apogee hook: invalid JSON on stdin: %v\n", err)
		return map[string]any{}
	}
	obj, ok := parsed.(map[string]any)
	if !ok {
		fmt.Fprintf(stderr, "apogee hook: expected JSON object on stdin, got %T\n", parsed)
		return map[string]any{}
	}
	return obj
}

// deriveSourceAppRuntime mirrors hooks/apogee_hook.py::derive_source_app
// exactly: $APOGEE_SOURCE_APP > basename(git toplevel) > basename(cwd) >
// "unknown". Every probe is best-effort and never raises.
func deriveSourceAppRuntime() string {
	if env := strings.TrimSpace(os.Getenv("APOGEE_SOURCE_APP")); env != "" {
		return env
	}
	if top := gitToplevelBasename(); top != "" {
		return top
	}
	if wd, err := os.Getwd(); err == nil {
		if base := filepath.Base(wd); base != "" && base != "." && base != string(filepath.Separator) {
			return base
		}
	}
	return defaultHookSourceApp
}

// gitToplevelBasename runs `git rev-parse --show-toplevel` with a 1s
// budget and returns the basename, or "" on any failure.
func gitToplevelBasename() string {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	top := strings.TrimSpace(string(out))
	if top == "" {
		return ""
	}
	base := filepath.Base(top)
	if base == "" || base == "." || base == string(filepath.Separator) {
		return ""
	}
	return base
}

// maybeClaimIntervention runs the claim + deliver flow for the two
// claim-capable hook events. Returns the Claude Code decision JSON bytes
// to write to stdout, or nil when nothing was claimed (or the claim did
// not match the hook's delivery mode).
func maybeClaimIntervention(ctx context.Context, opts *hookOptions, input map[string]any) []byte {
	if opts.Event != hookEventPreToolUse && opts.Event != hookEventUserPromptSubmit {
		return nil
	}
	sessionID := stringField(input, "session_id")
	if sessionID == "" {
		return nil
	}
	turnID := stringField(input, "turn_id")

	base := stripEventsSuffix(opts.ServerURL)
	if base == "" {
		return nil
	}

	claimURL := base + "/v1/sessions/" + sessionID + "/interventions/claim"
	body, _ := json.Marshal(map[string]any{
		"hook_event": opts.Event,
		"turn_id":    turnID,
	})

	iv, ok := postClaim(ctx, opts, claimURL, body)
	if !ok || iv == nil {
		return nil
	}

	message := stringField(iv, "message")
	mode := stringField(iv, "delivery_mode")
	interventionID := stringField(iv, "intervention_id")
	if message == "" {
		return nil
	}

	decision := decisionForMode(opts.Event, mode, message)
	if decision == nil {
		fmt.Fprintln(opts.Stderr, "apogee hook: claimed intervention does not match hook mode")
		return nil
	}

	encoded, err := json.Marshal(decision)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "apogee hook: failed to encode decision: %v\n", err)
		return nil
	}

	// Best-effort delivered callback. The operator dashboard flips the
	// row to "delivered" on the other side of this POST.
	if interventionID != "" {
		markDelivered(ctx, opts, base, interventionID)
	}
	return encoded
}

// postClaim POSTs to the claim endpoint and decodes the response. The
// returned map is the intervention snapshot (the body's "intervention"
// field), or nil on any failure / 204.
func postClaim(ctx context.Context, opts *hookOptions, url string, body []byte) (map[string]any, bool) {
	reqCtx, cancel := context.WithTimeout(ctx, interventionTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		fmt.Fprintf(opts.Stderr, "apogee hook: claim build request: %v\n", err)
		return nil, false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", hookUserAgent)
	resp, err := opts.HTTPClient.Do(req)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "apogee hook: claim network error: %v\n", err)
		return nil, false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNoContent {
		return nil, false
	}
	if resp.StatusCode >= 400 {
		fmt.Fprintf(opts.Stderr, "apogee hook: claim returned HTTP %d\n", resp.StatusCode)
		return nil, false
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "apogee hook: claim read body: %v\n", err)
		return nil, false
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, false
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		fmt.Fprintf(opts.Stderr, "apogee hook: claim decode body: %v\n", err)
		return nil, false
	}
	iv, _ := parsed["intervention"].(map[string]any)
	if iv == nil {
		return nil, false
	}
	return iv, true
}

// markDelivered fires the best-effort POST that flips the intervention
// row to "delivered" and broadcasts the SSE lifecycle event.
func markDelivered(ctx context.Context, opts *hookOptions, base, interventionID string) {
	url := base + "/v1/interventions/" + interventionID + "/delivered"
	body, _ := json.Marshal(map[string]any{"hook_event": opts.Event})
	reqCtx, cancel := context.WithTimeout(ctx, interventionTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		fmt.Fprintf(opts.Stderr, "apogee hook: delivered build request: %v\n", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", hookUserAgent)
	resp, err := opts.HTTPClient.Do(req)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "apogee hook: delivered network error: %v\n", err)
		return
	}
	_ = resp.Body.Close()
}

// decisionForMode returns the Claude Code hook decision JSON for a given
// delivery mode and hook event. Returns nil when the mode does not match
// the hook (e.g. "context" on PreToolUse — the sweeper eventually expires
// the row).
func decisionForMode(event, mode, message string) map[string]any {
	switch event {
	case hookEventPreToolUse:
		if mode == interventionModeInterrupt || mode == interventionModeBoth {
			return map[string]any{
				"decision": "block",
				"reason":   message,
			}
		}
	case hookEventUserPromptSubmit:
		if mode == interventionModeContext || mode == interventionModeBoth {
			return map[string]any{
				"hookSpecificOutput": map[string]any{
					"additionalContext": message,
				},
			}
		}
	}
	return nil
}

// postHookEvent builds the HookEvent body and POSTs it to --server-url.
// Every error is swallowed and logged to stderr.
func postHookEvent(ctx context.Context, opts *hookOptions, sourceApp string, input map[string]any, rawStdin []byte) {
	event := buildHookEventBody(opts.Event, sourceApp, opts.NowMillis(), input, rawStdin)

	body, err := json.Marshal(event)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "apogee hook: encode event: %v\n", err)
		return
	}

	reqCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, opts.ServerURL, bytes.NewReader(body))
	if err != nil {
		fmt.Fprintf(opts.Stderr, "apogee hook: events build request: %v\n", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", hookUserAgent)
	resp, err := opts.HTTPClient.Do(req)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "apogee hook: events network error: %v\n", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		fmt.Fprintf(opts.Stderr, "apogee hook: collector returned HTTP %d\n", resp.StatusCode)
	}
}

// buildHookEventBody assembles the HookEvent JSON body. It mirrors the Go
// struct in internal/ingest/payload.go — including the flat-field
// promotion — so a non-Go hook never has to touch the collector's wire
// contract.
func buildHookEventBody(event, sourceApp string, nowMillis int64, input map[string]any, rawStdin []byte) map[string]any {
	sessionID := stringField(input, "session_id")
	if sessionID == "" {
		sessionID = "unknown"
	}

	body := map[string]any{
		"source_app":      sourceApp,
		"session_id":      sessionID,
		"hook_event_type": event,
		"timestamp":       nowMillis,
	}

	// Preserve the original stdin as payload when available. This lets
	// the collector re-parse the exact bytes the hook saw rather than
	// a re-serialisation that might differ in key order.
	if payload := hookPayloadRaw(input, rawStdin); payload != nil {
		body["payload"] = payload
	}

	for _, key := range flatHookFields {
		if v, ok := input[key]; ok {
			body[key] = v
		}
	}
	return body
}

// hookPayloadRaw returns the `payload` field for the outbound event,
// preferring the original stdin bytes when they parsed cleanly. Falls
// back to encoding the input map so the POST always carries a payload
// (unless stdin was truly empty).
func hookPayloadRaw(input map[string]any, rawStdin []byte) json.RawMessage {
	if len(bytes.TrimSpace(rawStdin)) > 0 {
		// Validate it's a real JSON object before passing through so we
		// never write garbage to the collector.
		var probe any
		if err := json.Unmarshal(rawStdin, &probe); err == nil {
			if _, ok := probe.(map[string]any); ok {
				return json.RawMessage(rawStdin)
			}
		}
	}
	if len(input) == 0 {
		return nil
	}
	b, err := json.Marshal(input)
	if err != nil {
		return nil
	}
	return json.RawMessage(b)
}

// stripEventsSuffix normalises --server-url to a bare collector base.
// Trailing slashes and a literal "/v1/events" suffix are trimmed so the
// intervention endpoints — which live alongside /v1/events — can be
// reached with a consistent prefix.
func stripEventsSuffix(url string) string {
	if url == "" {
		return ""
	}
	for strings.HasSuffix(url, "/") {
		url = strings.TrimSuffix(url, "/")
	}
	for _, suffix := range []string{"/v1/events", "/v1/events/"} {
		if strings.HasSuffix(url, suffix) {
			return strings.TrimSuffix(url, suffix)
		}
	}
	return url
}

// stringField extracts a string field from a map[string]any, returning ""
// if the key is missing or not a string.
func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}
