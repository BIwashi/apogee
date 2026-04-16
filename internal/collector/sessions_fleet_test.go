package collector

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/BIwashi/apogee/internal/store/duckdb"
)

// TestSessionsActiveEndpoint walks the /v1/sessions/active fleet endpoint
// end-to-end: it seeds two sessions at different attention levels,
// verifies that the enriched card fields come through, and that the
// since filter scopes the window.
func TestSessionsActiveEndpoint(t *testing.T) {
	srv, ts := newTestServer(t)
	_ = srv
	ctx := t.Context()
	store := srv.store
	now := time.Now().UTC().Truncate(time.Millisecond)

	require.NoError(t, store.UpsertSession(ctx, duckdb.Session{
		SessionID:  "urgent",
		SourceApp:  "demo",
		StartedAt:  now.Add(-time.Minute),
		LastSeenAt: now,
	}))
	require.NoError(t, store.UpdateSessionAttention(ctx, "urgent", "intervene_now", "HITL pending", 0.9, "turn-urgent", "debug", "live"))

	require.NoError(t, store.UpsertSession(ctx, duckdb.Session{
		SessionID:  "calm",
		SourceApp:  "demo",
		StartedAt:  now.Add(-time.Minute),
		LastSeenAt: now.Add(-30 * time.Second),
	}))
	require.NoError(t, store.UpdateSessionAttention(ctx, "calm", "healthy", "", 0.1, "turn-calm", "implement", "live"))

	resp, err := http.Get(ts.URL + "/v1/sessions/active")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body struct {
		Sessions []duckdb.SessionCard `json:"sessions"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Len(t, body.Sessions, 2)
	// Urgent should sort first.
	require.Equal(t, "urgent", body.Sessions[0].SessionID)
	require.Equal(t, "intervene_now", body.Sessions[0].AttentionState)
	require.Equal(t, "turn-urgent", body.Sessions[0].CurrentTurnID)

	// Apply a since filter that excludes the older calm session.
	q := url.Values{}
	q.Set("since", now.Add(-10*time.Second).Format(time.RFC3339Nano))
	resp2, err := http.Get(ts.URL + "/v1/sessions/active?" + q.Encode())
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusOK, resp2.StatusCode)

	var body2 struct {
		Sessions []duckdb.SessionCard `json:"sessions"`
	}
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&body2))
	require.Len(t, body2.Sessions, 1)
	require.Equal(t, "urgent", body2.Sessions[0].SessionID)
}

// TestSessionsAttentionCountsEndpoint verifies the fleet-scoped counts
// endpoint buckets sessions (not turns) correctly.
func TestSessionsAttentionCountsEndpoint(t *testing.T) {
	srv, ts := newTestServer(t)
	_ = srv
	ctx := t.Context()
	store := srv.store
	now := time.Now().UTC().Truncate(time.Millisecond)

	for _, tc := range []struct {
		id, state string
	}{
		{"a", "intervene_now"},
		{"b", "intervene_now"},
		{"c", "watch"},
		{"d", "healthy"},
	} {
		require.NoError(t, store.UpsertSession(ctx, duckdb.Session{
			SessionID:  tc.id,
			SourceApp:  "demo",
			StartedAt:  now,
			LastSeenAt: now,
		}))
		require.NoError(t, store.UpdateSessionAttention(ctx, tc.id, tc.state, "", 0.5, "t", "", "live"))
	}

	resp, err := http.Get(ts.URL + "/v1/sessions/attention/counts")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var counts duckdb.AttentionCounts
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&counts))
	require.Equal(t, 2, counts.InterveneNow)
	require.Equal(t, 1, counts.Watch)
	require.Equal(t, 1, counts.Healthy)
	require.Equal(t, 4, counts.Total)
}
