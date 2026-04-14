package duckdb

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSessionRollupsUpsertAndGet(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	cases := []struct {
		name   string
		rollup SessionRollup
	}{
		{
			name: "full-fields",
			rollup: SessionRollup{
				SessionID:   "sess-a",
				GeneratedAt: now,
				Model:       "claude-sonnet-4-6",
				FromTurnID:  "turn-a",
				ToTurnID:    "turn-b",
				TurnCount:   4,
				RollupJSON:  `{"headline":"did lots of things","narrative":"…"}`,
			},
		},
		{
			name: "no-turn-ids",
			rollup: SessionRollup{
				SessionID:   "sess-b",
				GeneratedAt: now.Add(time.Minute),
				Model:       "claude-sonnet-4-6",
				TurnCount:   2,
				RollupJSON:  `{"headline":"x"}`,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, s.UpsertSessionRollup(ctx, tc.rollup))
			got, ok, err := s.GetSessionRollup(ctx, tc.rollup.SessionID)
			require.NoError(t, err)
			require.True(t, ok)
			require.Equal(t, tc.rollup.SessionID, got.SessionID)
			require.Equal(t, tc.rollup.Model, got.Model)
			require.Equal(t, tc.rollup.TurnCount, got.TurnCount)
			require.Equal(t, tc.rollup.RollupJSON, got.RollupJSON)
			require.Equal(t, tc.rollup.FromTurnID, got.FromTurnID)
			require.Equal(t, tc.rollup.ToTurnID, got.ToTurnID)
		})
	}

	// Missing row.
	_, ok, err := s.GetSessionRollup(ctx, "nope")
	require.NoError(t, err)
	require.False(t, ok)

	// Upsert overwrites.
	replacement := SessionRollup{
		SessionID:   "sess-a",
		GeneratedAt: now.Add(5 * time.Minute),
		Model:       "claude-sonnet-4-6",
		TurnCount:   9,
		RollupJSON:  `{"headline":"overwritten"}`,
	}
	require.NoError(t, s.UpsertSessionRollup(ctx, replacement))
	got, ok, err := s.GetSessionRollup(ctx, "sess-a")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 9, got.TurnCount)
	require.Contains(t, got.RollupJSON, "overwritten")
}

func TestListRollupCandidates(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	// Session A: 3 turns, no rollup yet. Should be picked.
	require.NoError(t, s.UpsertSession(ctx, Session{
		SessionID:  "sess-a",
		SourceApp:  "test",
		StartedAt:  now.Add(-time.Hour),
		LastSeenAt: now,
		TurnCount:  3,
	}))
	// Session B: 1 turn — below minTurns. Should be skipped.
	require.NoError(t, s.UpsertSession(ctx, Session{
		SessionID:  "sess-b",
		SourceApp:  "test",
		StartedAt:  now.Add(-time.Hour),
		LastSeenAt: now,
		TurnCount:  1,
	}))
	// Session C: 4 turns, fresh rollup (just now). Should be skipped —
	// rollup is newer than the min-age cutoff.
	require.NoError(t, s.UpsertSession(ctx, Session{
		SessionID:  "sess-c",
		SourceApp:  "test",
		StartedAt:  now.Add(-time.Hour),
		LastSeenAt: now,
		TurnCount:  4,
	}))
	require.NoError(t, s.UpsertSessionRollup(ctx, SessionRollup{
		SessionID:   "sess-c",
		GeneratedAt: now,
		Model:       "claude-sonnet-4-6",
		TurnCount:   4,
		RollupJSON:  `{}`,
	}))
	// Session D: 5 turns, old rollup but new activity. Should be picked.
	require.NoError(t, s.UpsertSession(ctx, Session{
		SessionID:  "sess-d",
		SourceApp:  "test",
		StartedAt:  now.Add(-2 * time.Hour),
		LastSeenAt: now,
		TurnCount:  5,
	}))
	require.NoError(t, s.UpsertSessionRollup(ctx, SessionRollup{
		SessionID:   "sess-d",
		GeneratedAt: now.Add(-2 * time.Hour),
		Model:       "claude-sonnet-4-6",
		TurnCount:   4,
		RollupJSON:  `{}`,
	}))

	got, err := s.ListRollupCandidates(ctx, 2, 30*time.Minute, 10)
	require.NoError(t, err)
	ids := map[string]bool{}
	for _, c := range got {
		ids[c.SessionID] = true
	}
	require.True(t, ids["sess-a"], "sess-a should be a candidate")
	require.True(t, ids["sess-d"], "sess-d should be a candidate")
	require.False(t, ids["sess-b"], "sess-b has too few turns")
	require.False(t, ids["sess-c"], "sess-c was just rolled up")
}
