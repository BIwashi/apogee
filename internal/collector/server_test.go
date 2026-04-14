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
	ctx := context.Background()
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
