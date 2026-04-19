package duckdb

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSessionTopicsLifecycle(t *testing.T) {
	ctx := t.Context()
	s := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	// Seed a session so the FK-style references read sensibly
	// even though there is no enforced foreign key.
	require.NoError(t, s.UpsertSession(ctx, Session{
		SessionID:  "sess-topic",
		SourceApp:  "demo",
		StartedAt:  now,
		LastSeenAt: now,
	}))

	// Insert two topics.
	root := SessionTopic{
		TopicID:    "topic-root",
		SessionID:  "sess-topic",
		Goal:       "ship the docs",
		OpenedAt:   now,
		LastSeenAt: now,
	}
	require.NoError(t, s.UpsertSessionTopic(ctx, root))

	branch := SessionTopic{
		TopicID:       "topic-branch",
		SessionID:     "sess-topic",
		ParentTopicID: sql.NullString{String: "topic-root", Valid: true},
		Goal:          "fix the UI bug",
		OpenedAt:      now.Add(time.Minute),
		LastSeenAt:    now.Add(time.Minute),
	}
	require.NoError(t, s.UpsertSessionTopic(ctx, branch))

	got, ok, err := s.GetSessionTopic(ctx, "topic-branch")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "topic-root", got.ParentTopicID.String)
	require.Equal(t, "fix the UI bug", got.Goal)

	// Bumping last_seen_at must not overwrite goal.
	bump := branch
	bump.Goal = "this should be ignored"
	bump.LastSeenAt = now.Add(2 * time.Minute)
	require.NoError(t, s.UpsertSessionTopic(ctx, bump))
	got, _, err = s.GetSessionTopic(ctx, "topic-branch")
	require.NoError(t, err)
	require.Equal(t, "fix the UI bug", got.Goal)
	require.True(t, got.LastSeenAt.After(now.Add(time.Minute)))

	// ListOpenTopicsForSession orders by last_seen_at DESC.
	open, err := s.ListOpenTopicsForSession(ctx, "sess-topic", 5)
	require.NoError(t, err)
	require.Len(t, open, 2)
	require.Equal(t, "topic-branch", open[0].TopicID)
	require.Equal(t, "topic-root", open[1].TopicID)
}

func TestRecordTopicTransitionUpsert(t *testing.T) {
	ctx := t.Context()
	s := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	tr := TopicTransition{
		TurnID:        "turn-1",
		SessionID:     "sess-1",
		Kind:          "new",
		Confidence:    sql.NullFloat64{Float64: 0.92, Valid: true},
		Model:         "claude-haiku-4-5",
		PromptVersion: "topic-v1",
		DecisionJSON:  `{"kind":"new","confidence":0.92}`,
		CreatedAt:     now,
		ToTopicID:     sql.NullString{String: "topic-1", Valid: true},
	}
	require.NoError(t, s.RecordTopicTransition(ctx, tr))

	// Re-record with a different decision — must overwrite, not duplicate.
	tr.Kind = "continue"
	tr.DecisionJSON = `{"kind":"continue","confidence":0.5}`
	tr.Confidence = sql.NullFloat64{Float64: 0.5, Valid: true}
	require.NoError(t, s.RecordTopicTransition(ctx, tr))

	// One row by turn_id.
	var count int
	require.NoError(t, s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM topic_transitions WHERE turn_id = ?`, "turn-1",
	).Scan(&count))
	require.Equal(t, 1, count)
}

func TestSetTurnTopic(t *testing.T) {
	ctx := t.Context()
	s := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	require.NoError(t, s.UpsertSession(ctx, Session{
		SessionID: "sess-1", SourceApp: "demo", StartedAt: now, LastSeenAt: now,
	}))
	require.NoError(t, s.InsertTurn(ctx, Turn{
		TurnID: "turn-1", TraceID: "00112233445566778899aabbccddeeff",
		SessionID: "sess-1", SourceApp: "demo", StartedAt: now, Status: "completed",
	}))

	require.NoError(t, s.SetTurnTopic(ctx, "turn-1", "topic-A"))

	var got string
	require.NoError(t, s.db.QueryRowContext(ctx,
		`SELECT topic_id FROM turns WHERE turn_id = ?`, "turn-1",
	).Scan(&got))
	require.Equal(t, "topic-A", got)
}
