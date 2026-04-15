package collector

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/BIwashi/apogee/internal/ingest"
	"github.com/BIwashi/apogee/internal/sse"
	"github.com/BIwashi/apogee/internal/store/duckdb"
)

func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	ctx := t.Context()
	store, err := duckdb.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	srv := New(Config{HTTPAddr: ":0", DBPath: ":memory:"}, store, nil)
	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)
	return srv, ts
}

func loadSamples(t *testing.T) []ingest.HookEvent {
	t.Helper()
	path := filepath.Join("testdata", "hook_samples.json")
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	var out []ingest.HookEvent
	require.NoError(t, json.Unmarshal(b, &out))
	require.GreaterOrEqual(t, len(out), 12, "need at least 12 samples to cover all 12 hook event types")
	return out
}

func TestIntegrationHealthz(t *testing.T) {
	_, ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/v1/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestStartBackgroundIdempotent pins the sync.Once guard on StartBackground.
// A regression where the guard is dropped would silently double-start the
// metrics sampler (duplicate metric_points rows) and the HITL ticker, which
// is hard to notice in integration tests — so the assertion here is
// structural: after the first call, the unexported sync.Once is already
// "done", and any later Do invocation is a no-op.
func TestStartBackgroundIdempotent(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	store, err := duckdb.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	srv := New(Config{HTTPAddr: ":0", DBPath: ":memory:"}, store, nil)

	srv.StartBackground(ctx)

	// The Once is shared state — a fresh Do with a sentinel func cannot
	// run its body if StartBackground has already fired the Once, so the
	// flag must stay false. This is the same mechanism StartBackground
	// itself relies on.
	sentinelRan := false
	srv.startBackground.Do(func() { sentinelRan = true })
	require.False(t, sentinelRan, "sync.Once must already be done after first StartBackground")

	// A second explicit call must also be a silent no-op (no panic, no
	// goroutines started, no duplicate subscribers).
	require.NotPanics(t, func() { srv.StartBackground(ctx) })

	// Tear everything down so the test stays hermetic and the race
	// detector sees a clean exit.
	cancel()
	stopCtx, stopCancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer stopCancel()
	srv.StopBackground(stopCtx)
}

func TestIntegrationReceiveValidation(t *testing.T) {
	_, ts := newTestServer(t)
	// Missing required fields.
	resp, err := http.Post(ts.URL+"/v1/events", "application/json", bytes.NewBufferString(`{}`))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestIntegrationFullSamplePipeline(t *testing.T) {
	_, ts := newTestServer(t)
	samples := loadSamples(t)

	for _, ev := range samples {
		body, err := json.Marshal(ev)
		require.NoError(t, err)
		resp, err := http.Post(ts.URL+"/v1/events", "application/json", bytes.NewBuffer(body))
		require.NoError(t, err)
		require.Equal(t, http.StatusAccepted, resp.StatusCode, "event %s", ev.HookEventType)
		resp.Body.Close()
	}

	// Sessions endpoint.
	{
		resp, err := http.Get(ts.URL + "/v1/sessions/recent")
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var out struct {
			Sessions []map[string]any `json:"sessions"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
		require.Len(t, out.Sessions, 1)
	}

	// Recent turns.
	var turnID string
	{
		resp, err := http.Get(ts.URL + "/v1/turns/recent")
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var out struct {
			Turns []map[string]any `json:"turns"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
		require.Len(t, out.Turns, 1)
		turnID = out.Turns[0]["turn_id"].(string)
		require.NotEmpty(t, turnID)
	}

	// Single turn.
	{
		resp, err := http.Get(ts.URL + "/v1/turns/" + turnID)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
	}

	// Spans for the turn.
	{
		resp, err := http.Get(ts.URL + "/v1/turns/" + turnID + "/spans")
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var out struct {
			Spans []map[string]any `json:"spans"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
		// turn root + 2 successful tools (Bash, Grep) + 1 failed tool (Read)
		// + 1 subagent + 1 hitl permission = 6
		require.GreaterOrEqual(t, len(out.Spans), 5)
	}

	// Filter options.
	{
		resp, err := http.Get(ts.URL + "/v1/filter-options")
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var opts duckdb.FilterOptions
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&opts))
		require.NotEmpty(t, opts.HookEvents)
		require.NotEmpty(t, opts.SourceApps)
	}
}

func TestIntegrationSSEStream(t *testing.T) {
	_, ts := newTestServer(t)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/v1/events/stream", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	reader := bufio.NewReader(resp.Body)

	// First frame must be the synthetic `initial` event (pre-broadcast).
	initial := readSSEFrame(t, reader)
	require.Equal(t, sse.EventTypeInitial, initial.Type)
	var initialPayload sse.InitialPayload
	require.NoError(t, json.Unmarshal(initial.Data, &initialPayload))
	// Fresh store: both recent lists are empty but non-nil.
	require.NotNil(t, initialPayload.RecentTurns)
	require.NotNil(t, initialPayload.RecentSessions)
	require.Len(t, initialPayload.RecentTurns, 0)

	// Post a single hook event; expect broadcasts to arrive on the stream.
	ev := ingest.HookEvent{
		SourceApp:     "demo",
		SessionID:     "sess-sse",
		HookEventType: "SessionStart",
		Timestamp:     time.Now().UnixMilli(),
	}
	body, err := json.Marshal(ev)
	require.NoError(t, err)
	postResp, err := http.Post(ts.URL+"/v1/events", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	postResp.Body.Close()
	require.Equal(t, http.StatusAccepted, postResp.StatusCode)

	// We should see at least one session.updated broadcast.
	found := false
	for i := 0; i < 5; i++ {
		next := readSSEFrame(t, reader)
		if next.Type == sse.EventTypeSessionUpdated {
			var payload sse.SessionPayload
			require.NoError(t, json.Unmarshal(next.Data, &payload))
			require.Equal(t, "sess-sse", payload.Session.SessionID)
			found = true
			break
		}
	}
	require.True(t, found, "expected session.updated broadcast on stream")
}

func TestIntegrationHITLLifecycle(t *testing.T) {
	_, ts := newTestServer(t)
	samples := loadSamples(t)

	// Post the entire sample set so SessionStart and UserPromptSubmit run
	// before PermissionRequest. The reconstructor wires the hitl_events
	// row inline.
	for _, ev := range samples {
		if ev.HookEventType == "Stop" {
			continue // leave the turn open so the HITL row stays pending
		}
		body, err := json.Marshal(ev)
		require.NoError(t, err)
		resp, err := http.Post(ts.URL+"/v1/events", "application/json", bytes.NewBuffer(body))
		require.NoError(t, err)
		require.Equal(t, http.StatusAccepted, resp.StatusCode, ev.HookEventType)
		resp.Body.Close()
	}

	// Discover the session id from the sample stream so we can hit the
	// session-scoped pending endpoint.
	var sessionID string
	for _, ev := range samples {
		if ev.HookEventType == "PermissionRequest" {
			sessionID = ev.SessionID
			break
		}
	}
	require.NotEmpty(t, sessionID)

	// Pending HITL list for the session — should contain exactly one row.
	pendingURL := ts.URL + "/v1/sessions/" + sessionID + "/hitl/pending"
	var pending struct {
		HITL []sse.HITLSnapshot `json:"hitl"`
	}
	resp, err := http.Get(pendingURL)
	require.NoError(t, err)
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&pending))
	resp.Body.Close()
	require.Len(t, pending.HITL, 1)
	hitlID := pending.HITL[0].HitlID
	require.NotEmpty(t, hitlID)

	// Respond.
	respBody, _ := json.Marshal(duckdb.HITLResponse{
		Decision:       "allow",
		ReasonCategory: "scope",
		OperatorNote:   "ok",
		ResumeMode:     "continue",
	})
	resp, err = http.Post(ts.URL+"/v1/hitl/"+hitlID+"/respond", "application/json", bytes.NewReader(respBody))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// Pending list is now empty.
	resp, err = http.Get(pendingURL)
	require.NoError(t, err)
	pending.HITL = nil
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&pending))
	resp.Body.Close()
	require.Len(t, pending.HITL, 0)

	// Filtered list — status=responded — returns the row.
	resp, err = http.Get(ts.URL + "/v1/hitl?session_id=" + sessionID + "&status=responded")
	require.NoError(t, err)
	var listOut struct {
		HITL []sse.HITLSnapshot `json:"hitl"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&listOut))
	resp.Body.Close()
	require.Len(t, listOut.HITL, 1)
	require.Equal(t, "responded", listOut.HITL[0].Status)
	require.NotNil(t, listOut.HITL[0].Decision)
	require.Equal(t, "allow", *listOut.HITL[0].Decision)

	// Second respond should yield 409.
	resp, err = http.Post(ts.URL+"/v1/hitl/"+hitlID+"/respond", "application/json", bytes.NewReader(respBody))
	require.NoError(t, err)
	require.Equal(t, http.StatusConflict, resp.StatusCode)
	resp.Body.Close()

	// Missing id yields 404.
	resp, err = http.Post(ts.URL+"/v1/hitl/missing/respond", "application/json", bytes.NewReader(respBody))
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	resp.Body.Close()
}

func TestIntegrationEventsBatchAccepted(t *testing.T) {
	_, ts := newTestServer(t)
	samples := loadSamples(t)[:3]
	body, err := json.Marshal(samples)
	require.NoError(t, err)
	resp, err := http.Post(ts.URL+"/v1/events", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
	var out map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Equal(t, float64(3), out["accepted"])
}

// readSSEFrame reads one `data: <json>\n\n` frame from an SSE stream,
// skipping any heartbeat comment lines that precede it. It fails the test on
// parse error so callers stay concise.
func readSSEFrame(t *testing.T, r *bufio.Reader) sse.Event {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		line, err := r.ReadString('\n')
		require.NoError(t, err)
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			continue // frame separator
		}
		if strings.HasPrefix(line, ":") {
			continue // heartbeat comment
		}
		if !strings.HasPrefix(line, "data: ") {
			t.Fatalf("unexpected SSE line: %q", line)
		}
		var ev sse.Event
		require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev))
		return ev
	}
	t.Fatalf("timed out waiting for SSE frame")
	return sse.Event{}
}

func TestIntegrationSessionTurns(t *testing.T) {
	_, ts := newTestServer(t)
	samples := loadSamples(t)
	for _, ev := range samples {
		body, _ := json.Marshal(ev)
		resp, err := http.Post(ts.URL+"/v1/events", "application/json", bytes.NewBuffer(body))
		require.NoError(t, err)
		resp.Body.Close()
	}
	resp, err := http.Get(ts.URL + "/v1/sessions/sess-alpha/turns")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var out struct {
		Turns []map[string]any `json:"turns"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Turns, 1)
}

// TestIntegrationAgentDetail covers the new GET /v1/agents/:id/detail
// endpoint introduced by PR #36 to feed the cross-cutting AgentDrawer. The
// test leans on the bundled hook samples which carry one subagent
// (`sub-1`) so the helpers exercise the parent / children / turns / tools
// branches.
func TestIntegrationAgentDetail(t *testing.T) {
	_, ts := newTestServer(t)
	samples := loadSamples(t)
	for _, ev := range samples {
		body, _ := json.Marshal(ev)
		resp, err := http.Post(ts.URL+"/v1/events", "application/json", bytes.NewBuffer(body))
		require.NoError(t, err)
		resp.Body.Close()
	}

	// Discover an agent id from the recent agents list.
	resp, err := http.Get(ts.URL + "/v1/agents/recent")
	require.NoError(t, err)
	var agentList struct {
		Agents []map[string]any `json:"agents"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&agentList))
	resp.Body.Close()
	require.NotEmpty(t, agentList.Agents)
	agentID, _ := agentList.Agents[0]["agent_id"].(string)
	require.NotEmpty(t, agentID)

	// Detail endpoint returns the bundle.
	resp, err = http.Get(ts.URL + "/v1/agents/" + agentID + "/detail")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var detail struct {
		Agent      map[string]any   `json:"agent"`
		Parent     map[string]any   `json:"parent"`
		Children   []map[string]any `json:"children"`
		Turns      []map[string]any `json:"turns"`
		ToolCounts []map[string]any `json:"tool_counts"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&detail))
	require.NotNil(t, detail.Agent)
	require.NotNil(t, detail.Children)
	require.NotNil(t, detail.Turns)
	require.NotNil(t, detail.ToolCounts)

	// Unknown agent → 404.
	resp, err = http.Get(ts.URL + "/v1/agents/does-not-exist/detail")
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	resp.Body.Close()
}

// TestIntegrationSpanDetail covers the new GET /v1/spans/:trace_id/:span_id
// /detail endpoint that powers the cross-cutting SpanDrawer (PR #36).
func TestIntegrationSpanDetail(t *testing.T) {
	_, ts := newTestServer(t)
	samples := loadSamples(t)
	for _, ev := range samples {
		body, _ := json.Marshal(ev)
		resp, err := http.Post(ts.URL+"/v1/events", "application/json", bytes.NewBuffer(body))
		require.NoError(t, err)
		resp.Body.Close()
	}

	// Use the spans listing for the alpha turn to grab a (trace_id, span_id)
	// pair that we know exists.
	resp, err := http.Get(ts.URL + "/v1/turns/recent")
	require.NoError(t, err)
	var turns struct {
		Turns []map[string]any `json:"turns"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&turns))
	resp.Body.Close()
	require.NotEmpty(t, turns.Turns)
	turnID, _ := turns.Turns[0]["turn_id"].(string)

	resp, err = http.Get(ts.URL + "/v1/turns/" + turnID + "/spans")
	require.NoError(t, err)
	var spansResp struct {
		Spans []map[string]any `json:"spans"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&spansResp))
	resp.Body.Close()
	require.NotEmpty(t, spansResp.Spans)

	// Pick the first span that has a parent so the parent branch is also
	// exercised. Fall back to the first span if no children exist.
	var traceID, spanID string
	for _, sp := range spansResp.Spans {
		if parent, ok := sp["parent_span_id"].(string); ok && parent != "" {
			traceID, _ = sp["trace_id"].(string)
			spanID, _ = sp["span_id"].(string)
			break
		}
	}
	if spanID == "" {
		traceID, _ = spansResp.Spans[0]["trace_id"].(string)
		spanID, _ = spansResp.Spans[0]["span_id"].(string)
	}
	require.NotEmpty(t, traceID)
	require.NotEmpty(t, spanID)

	resp, err = http.Get(ts.URL + "/v1/spans/" + traceID + "/" + spanID + "/detail")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var detail struct {
		Span     map[string]any   `json:"span"`
		Parent   map[string]any   `json:"parent"`
		Children []map[string]any `json:"children"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&detail))
	require.Equal(t, spanID, detail.Span["span_id"])

	// Missing span → 404.
	resp, err = http.Get(ts.URL + "/v1/spans/" + traceID + "/missing-span/detail")
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	resp.Body.Close()
}

func TestIntegrationWatchdogSignalsHTTP(t *testing.T) {
	srv, ts := newTestServer(t)

	// Seed a single signal directly via the store — the background
	// worker is only driven by a ticker, so bypassing it keeps the test
	// deterministic.
	now := time.Now().UTC().Truncate(time.Millisecond)
	store := srv.store
	ctx := t.Context()

	row, err := store.InsertWatchdogSignal(ctx, duckdb.WatchdogSignal{
		DetectedAt:     now,
		MetricName:     "apogee.tools.rate",
		LabelsJSON:     `{}`,
		ZScore:         6.5,
		BaselineMean:   2.0,
		BaselineStddev: 0.5,
		WindowValue:    15.0,
		Severity:       duckdb.WatchdogSeverityWarning,
		Headline:       "Unusual tool activity — 15.0/s vs baseline 2.0/s",
		EvidenceJSON:   `{"window":[],"baseline":{"mean":2.0,"stddev":0.5}}`,
	})
	require.NoError(t, err)
	require.Greater(t, row.ID, int64(0))

	// GET /v1/watchdog/signals — unacked filter returns the row.
	{
		resp, err := http.Get(ts.URL + "/v1/watchdog/signals?status=unacked&limit=20")
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var out struct {
			Signals []map[string]any `json:"signals"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
		require.Len(t, out.Signals, 1)
		require.Equal(t, "apogee.tools.rate", out.Signals[0]["metric_name"])
		require.Equal(t, float64(row.ID), out.Signals[0]["id"])
		require.Equal(t, false, out.Signals[0]["acknowledged"])
	}

	// POST /v1/watchdog/signals/{id}/ack flips the row to acknowledged.
	{
		url := ts.URL + "/v1/watchdog/signals/" + strconv.FormatInt(row.ID, 10) + "/ack"
		resp, err := http.Post(url, "application/json", nil)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var out struct {
			Signal map[string]any `json:"signal"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
		require.Equal(t, true, out.Signal["acknowledged"])
	}

	// The unacked filter should now be empty.
	{
		resp, err := http.Get(ts.URL + "/v1/watchdog/signals?status=unacked")
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var out struct {
			Signals []map[string]any `json:"signals"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
		require.Len(t, out.Signals, 0)
	}

	// Unknown ids return 404.
	{
		resp, err := http.Post(ts.URL+"/v1/watchdog/signals/99999/ack", "application/json", nil)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusNotFound, resp.StatusCode)
	}
}
