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

func TestListSessionTopicSummariesAndTransitions(t *testing.T) {
	ctx := t.Context()
	s := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	// Two sessions: sess-A has 3 turns / 2 topics (one of which is
	// closed), sess-B has 1 turn / 1 open topic. Plus one
	// low-confidence transition recorded as "unknown".
	require.NoError(t, s.UpsertSession(ctx, Session{
		SessionID: "sess-A", SourceApp: "demo-A",
		StartedAt: now.Add(-2 * time.Hour), LastSeenAt: now,
	}))
	require.NoError(t, s.UpsertSession(ctx, Session{
		SessionID: "sess-B", SourceApp: "demo-B",
		StartedAt: now.Add(-30 * time.Minute), LastSeenAt: now.Add(-1 * time.Minute),
	}))

	// sess-A topics: root (closed) + branch (open, most recent).
	require.NoError(t, s.UpsertSessionTopic(ctx, SessionTopic{
		TopicID:    "topic-A-root",
		SessionID:  "sess-A",
		Goal:       "ship the docs",
		OpenedAt:   now.Add(-2 * time.Hour),
		LastSeenAt: now.Add(-90 * time.Minute),
		ClosedAt:   sql.NullTime{Time: now.Add(-90 * time.Minute), Valid: true},
	}))
	require.NoError(t, s.UpsertSessionTopic(ctx, SessionTopic{
		TopicID:       "topic-A-branch",
		SessionID:     "sess-A",
		ParentTopicID: sql.NullString{String: "topic-A-root", Valid: true},
		Goal:          "fix the auth bug",
		OpenedAt:      now.Add(-1 * time.Hour),
		LastSeenAt:    now,
	}))
	for i, tid := range []string{"turn-A1", "turn-A2", "turn-A3"} {
		require.NoError(t, s.InsertTurn(ctx, Turn{
			TurnID: tid, TraceID: tid + "-trace",
			SessionID: "sess-A", SourceApp: "demo-A",
			StartedAt: now.Add(-time.Duration(180-i*30) * time.Minute),
			Status:    "completed",
		}))
	}
	require.NoError(t, s.SetTurnTopic(ctx, "turn-A1", "topic-A-root"))
	require.NoError(t, s.SetTurnTopic(ctx, "turn-A2", "topic-A-branch"))

	require.NoError(t, s.RecordTopicTransition(ctx, TopicTransition{
		TurnID: "turn-A1", SessionID: "sess-A", Kind: "new",
		Confidence:    sql.NullFloat64{Float64: 0.95, Valid: true},
		Model:         "claude-haiku-4-5",
		PromptVersion: "topic-v1",
		DecisionJSON:  `{"kind":"new"}`,
		CreatedAt:     now.Add(-150 * time.Minute),
		ToTopicID:     sql.NullString{String: "topic-A-root", Valid: true},
	}))
	require.NoError(t, s.RecordTopicTransition(ctx, TopicTransition{
		TurnID: "turn-A2", SessionID: "sess-A", Kind: "resume",
		Confidence:    sql.NullFloat64{Float64: 0.8, Valid: true},
		Model:         "claude-haiku-4-5",
		PromptVersion: "topic-v1",
		DecisionJSON:  `{"kind":"resume","target_topic_ref":"recent:1"}`,
		CreatedAt:     now.Add(-100 * time.Minute),
		FromTopicID:   sql.NullString{String: "topic-A-root", Valid: true},
		ToTopicID:     sql.NullString{String: "topic-A-branch", Valid: true},
	}))
	require.NoError(t, s.RecordTopicTransition(ctx, TopicTransition{
		TurnID: "turn-A3", SessionID: "sess-A", Kind: "unknown",
		Confidence:    sql.NullFloat64{Float64: 0.4, Valid: true},
		Model:         "claude-haiku-4-5",
		PromptVersion: "topic-v1",
		DecisionJSON:  `{"kind":"continue","confidence":0.4}`,
		CreatedAt:     now.Add(-30 * time.Minute),
	}))

	// sess-B: one open topic, one classified turn.
	require.NoError(t, s.UpsertSessionTopic(ctx, SessionTopic{
		TopicID: "topic-B", SessionID: "sess-B",
		Goal:     "investigate CI failure",
		OpenedAt: now.Add(-30 * time.Minute), LastSeenAt: now.Add(-1 * time.Minute),
	}))
	require.NoError(t, s.InsertTurn(ctx, Turn{
		TurnID: "turn-B1", TraceID: "turn-B1-trace",
		SessionID: "sess-B", SourceApp: "demo-B",
		StartedAt: now.Add(-30 * time.Minute), Status: "completed",
	}))
	require.NoError(t, s.SetTurnTopic(ctx, "turn-B1", "topic-B"))

	summaries, err := s.ListSessionTopicSummaries(ctx, 0)
	require.NoError(t, err)
	require.Len(t, summaries, 2)

	a := summaries[0]
	require.Equal(t, "sess-A", a.SessionID)
	require.Equal(t, "demo-A", a.SourceApp)
	require.Equal(t, 2, a.TopicCount)
	require.Equal(t, 1, a.OpenTopicCount)
	require.Equal(t, 3, a.TurnsTotal)
	require.Equal(t, 2, a.TurnsClassified)
	require.Equal(t, 1, a.TurnsLowConfidence)
	require.True(t, a.ActiveGoal.Valid)
	require.Equal(t, "fix the auth bug", a.ActiveGoal.String)

	b := summaries[1]
	require.Equal(t, "sess-B", b.SessionID)
	require.Equal(t, 1, b.TopicCount)
	require.Equal(t, 1, b.OpenTopicCount)

	tr, err := s.ListTopicTransitions(ctx, "sess-A", 0)
	require.NoError(t, err)
	require.Len(t, tr, 3)
	require.Equal(t, "turn-A1", tr[0].TurnID)
	require.Equal(t, "new", tr[0].Kind)
	require.Equal(t, "turn-A2", tr[1].TurnID)
	require.Equal(t, "resume", tr[1].Kind)
	require.Equal(t, "unknown", tr[2].Kind)
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
