package duckdb

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// seedThreeTurns inserts three turns across two sessions so the filter tests
// have a small but meaningful dataset to filter against.
func seedThreeTurns(t *testing.T, s *Store) time.Time {
	t.Helper()
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Millisecond)

	require.NoError(t, s.UpsertSession(ctx, Session{
		SessionID:  "sess-a",
		SourceApp:  "app-a",
		StartedAt:  now,
		LastSeenAt: now,
	}))
	require.NoError(t, s.UpsertSession(ctx, Session{
		SessionID:  "sess-b",
		SourceApp:  "app-b",
		StartedAt:  now,
		LastSeenAt: now,
	}))

	// Three turns: two in sess-a (one old, one fresh), one in sess-b.
	require.NoError(t, s.InsertTurn(ctx, Turn{
		TurnID:    "turn-a-old",
		TraceID:   "trace-a-old",
		SessionID: "sess-a",
		SourceApp: "app-a",
		StartedAt: now.Add(-2 * time.Hour),
		Status:    "running",
	}))
	require.NoError(t, s.InsertTurn(ctx, Turn{
		TurnID:    "turn-a-new",
		TraceID:   "trace-a-new",
		SessionID: "sess-a",
		SourceApp: "app-a",
		StartedAt: now.Add(-5 * time.Minute),
		Status:    "running",
	}))
	require.NoError(t, s.InsertTurn(ctx, Turn{
		TurnID:    "turn-b-new",
		TraceID:   "trace-b-new",
		SessionID: "sess-b",
		SourceApp: "app-b",
		StartedAt: now.Add(-3 * time.Minute),
		Status:    "running",
	}))
	return now
}

func turnIDs(turns []Turn) []string {
	out := make([]string, len(turns))
	for i, t := range turns {
		out[i] = t.TurnID
	}
	return out
}

func TestListRecentTurnsFiltered_BySession(t *testing.T) {
	s := newTestStore(t)
	seedThreeTurns(t, s)
	ctx := t.Context()

	got, err := s.ListRecentTurnsFiltered(ctx, TurnFilter{SessionID: "sess-a"}, 100)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"turn-a-old", "turn-a-new"}, turnIDs(got))
}

func TestListRecentTurnsFiltered_BySourceApp(t *testing.T) {
	s := newTestStore(t)
	seedThreeTurns(t, s)
	ctx := t.Context()

	got, err := s.ListRecentTurnsFiltered(ctx, TurnFilter{SourceApp: "app-b"}, 100)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"turn-b-new"}, turnIDs(got))
}

func TestListRecentTurnsFiltered_ByTimeRange(t *testing.T) {
	s := newTestStore(t)
	now := seedThreeTurns(t, s)
	ctx := t.Context()

	since := now.Add(-10 * time.Minute)
	got, err := s.ListRecentTurnsFiltered(ctx, TurnFilter{Since: &since}, 100)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"turn-a-new", "turn-b-new"}, turnIDs(got))
}

func TestListRecentTurnsFiltered_Combined(t *testing.T) {
	s := newTestStore(t)
	now := seedThreeTurns(t, s)
	ctx := t.Context()

	since := now.Add(-10 * time.Minute)
	got, err := s.ListRecentTurnsFiltered(ctx, TurnFilter{
		SessionID: "sess-a",
		Since:     &since,
	}, 100)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"turn-a-new"}, turnIDs(got))
}

func TestCountAttentionFiltered(t *testing.T) {
	s := newTestStore(t)
	seedThreeTurns(t, s)
	ctx := t.Context()

	// Include ended rows so the count matches every inserted turn.
	counts, err := s.CountAttentionFiltered(ctx, TurnFilter{SessionID: "sess-a"}, true)
	require.NoError(t, err)
	require.Equal(t, 2, counts.Total)

	counts, err = s.CountAttentionFiltered(ctx, TurnFilter{SourceApp: "app-b"}, true)
	require.NoError(t, err)
	require.Equal(t, 1, counts.Total)
}

func TestSearchSessions_FuzzyMatch(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Millisecond)

	// Two sessions with distinctive source apps + latest prompt text.
	require.NoError(t, s.UpsertSession(ctx, Session{
		SessionID:  "019d8a000001",
		SourceApp:  "orchestra",
		StartedAt:  now,
		LastSeenAt: now,
	}))
	require.NoError(t, s.InsertTurn(ctx, Turn{
		TurnID:     "t1",
		TraceID:    "tr1",
		SessionID:  "019d8a000001",
		SourceApp:  "orchestra",
		StartedAt:  now,
		Status:     "running",
		PromptText: "refactor auth module",
	}))

	require.NoError(t, s.UpsertSession(ctx, Session{
		SessionID:  "019f20000002",
		SourceApp:  "dashboards",
		StartedAt:  now,
		LastSeenAt: now,
	}))
	require.NoError(t, s.InsertTurn(ctx, Turn{
		TurnID:     "t2",
		TraceID:    "tr2",
		SessionID:  "019f20000002",
		SourceApp:  "dashboards",
		StartedAt:  now,
		Status:     "running",
		PromptText: "fix widget layout",
	}))

	// Session id prefix.
	hits, err := s.SearchSessions(ctx, "019d", 10)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	require.Equal(t, "019d8a000001", hits[0].SessionID)

	// Source app match.
	hits, err = s.SearchSessions(ctx, "orch", 10)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	require.Equal(t, "019d8a000001", hits[0].SessionID)

	// Prompt text match.
	hits, err = s.SearchSessions(ctx, "auth", 10)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	require.Equal(t, "019d8a000001", hits[0].SessionID)
	require.Contains(t, hits[0].LatestPromptSnippet, "auth")
}

func TestGetSessionSummary(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Millisecond)

	require.NoError(t, s.UpsertSession(ctx, Session{
		SessionID:  "sess-a",
		SourceApp:  "app",
		StartedAt:  now,
		LastSeenAt: now,
		Model:      "sonnet",
	}))
	require.NoError(t, s.InsertTurn(ctx, Turn{
		TurnID:    "t-run",
		TraceID:   "tr-run",
		SessionID: "sess-a",
		SourceApp: "app",
		StartedAt: now,
		Status:    "running",
	}))
	require.NoError(t, s.InsertTurn(ctx, Turn{
		TurnID:    "t-done",
		TraceID:   "tr-done",
		SessionID: "sess-a",
		SourceApp: "app",
		StartedAt: now.Add(-time.Minute),
		Status:    "completed",
	}))

	sum, err := s.GetSessionSummary(ctx, "sess-a")
	require.NoError(t, err)
	require.NotNil(t, sum)
	require.Equal(t, 1, sum.RunningCount)
	require.Equal(t, 1, sum.CompletedCount)
	require.Equal(t, "sonnet", sum.Model)
	require.NotEmpty(t, sum.LatestTurnID)
}
