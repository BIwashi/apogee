package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/BIwashi/apogee/internal/ingest"
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
