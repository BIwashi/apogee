package summarizer

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/BIwashi/apogee/internal/store/duckdb"
)

func TestParseTopicDecision_Envelope(t *testing.T) {
	got, err := ParseTopicDecision(`{"topic_decision":{"kind":"new","confidence":0.91,"goal":"ship the docs"}}`)
	require.NoError(t, err)
	require.Equal(t, TopicKindNew, got.Kind)
	require.InDelta(t, 0.91, got.Confidence, 0.001)
	require.Equal(t, "ship the docs", got.Goal)
}

func TestParseTopicDecision_Bare(t *testing.T) {
	got, err := ParseTopicDecision(`{"kind":"continue","confidence":0.7,"goal":"continue refactor"}`)
	require.NoError(t, err)
	require.Equal(t, TopicKindContinue, got.Kind)
}

func TestParseTopicDecision_RejectsInvalidKind(t *testing.T) {
	_, err := ParseTopicDecision(`{"kind":"fork","confidence":0.9}`)
	require.Error(t, err)
}

func TestParseTopicDecision_StripsCodeFences(t *testing.T) {
	raw := "```json\n{\"topic_decision\":{\"kind\":\"resume\",\"target_topic_ref\":\"recent:1\",\"confidence\":0.8,\"goal\":\"x\"}}\n```"
	got, err := ParseTopicDecision(raw)
	require.NoError(t, err)
	require.Equal(t, TopicKindResume, got.Kind)
	require.Equal(t, "recent:1", got.TargetTopicRef)
}

// stubRunner implements the Runner interface with a queued canned
// response per call. Lets the backfill test exercise the full
// loop without spawning a real `claude` subprocess.
type stubRunner struct {
	responses []string
	idx       int
}

func (s *stubRunner) Run(_ context.Context, _, _ string) (string, error) {
	if s.idx >= len(s.responses) {
		return "", nil
	}
	out := s.responses[s.idx]
	s.idx++
	return out, nil
}

func TestBackfillTopics_AssignsTopicsChronologically(t *testing.T) {
	ctx := t.Context()
	store := newTestStoreForBackfill(t)
	t0 := time.Now().UTC().Truncate(time.Millisecond)

	// Two recapped turns in the same session, oldest first by start time.
	insertRecappedTurn(t, store, "sess-1", "turn-A", t0, recapWithHeadline("started the docs"))
	insertRecappedTurn(t, store, "sess-1", "turn-B", t0.Add(2*time.Minute), recapWithHeadline("finished the section"))

	runner := &stubRunner{
		responses: []string{
			// turn-A: first turn, must be classified as "new".
			`{"topic_decision":{"kind":"new","confidence":0.95,"goal":"ship the docs"}}`,
			// turn-B: continues the same topic.
			`{"topic_decision":{"kind":"continue","confidence":0.93,"goal":"ship the docs"}}`,
		},
	}

	res, err := BackfillTopics(ctx, store, runner, Defaults(), BackfillOptions{}, nil)
	require.NoError(t, err)
	require.Equal(t, 2, res.TurnsClassified)
	require.Equal(t, 1, res.SessionsConsidered)
	require.Equal(t, 0, res.TurnsErrored)

	open, err := store.ListOpenTopicsForSession(ctx, "sess-1", 5)
	require.NoError(t, err)
	require.Len(t, open, 1, "continue must reuse the new topic, not open a second one")
	require.Equal(t, "ship the docs", open[0].Goal)

	turnA, err := store.GetTurn(ctx, "turn-A")
	require.NoError(t, err)
	require.Equal(t, open[0].TopicID, turnA.TopicID)

	turnB, err := store.GetTurn(ctx, "turn-B")
	require.NoError(t, err)
	require.Equal(t, open[0].TopicID, turnB.TopicID)
}

func TestBackfillTopics_DryRunMakesNoChanges(t *testing.T) {
	ctx := t.Context()
	store := newTestStoreForBackfill(t)
	t0 := time.Now().UTC().Truncate(time.Millisecond)
	insertRecappedTurn(t, store, "sess-1", "turn-A", t0, recapWithHeadline("anything"))

	runner := &stubRunner{}
	res, err := BackfillTopics(ctx, store, runner, Defaults(), BackfillOptions{DryRun: true}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, res.TurnsConsidered)
	require.Equal(t, 0, res.TurnsClassified)

	open, err := store.ListOpenTopicsForSession(ctx, "sess-1", 5)
	require.NoError(t, err)
	require.Empty(t, open)
}

// ---------------------------------------------------------------------
// Test helpers.
// ---------------------------------------------------------------------

func newTestStoreForBackfill(t *testing.T) *duckdb.Store {
	t.Helper()
	s, err := duckdb.Open(t.Context(), ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func recapWithHeadline(headline string) string {
	return `{"headline":"` + headline + `","outcome":"success","phases":[],"key_steps":["a","b"],"failure_cause":null,"notable_events":[]}`
}

func insertRecappedTurn(t *testing.T, s *duckdb.Store, sessionID, turnID string, startedAt time.Time, recapJSON string) {
	t.Helper()
	ctx := t.Context()
	require.NoError(t, s.UpsertSession(ctx, duckdb.Session{
		SessionID: sessionID, SourceApp: "demo", StartedAt: startedAt, LastSeenAt: startedAt,
	}))
	require.NoError(t, s.InsertTurn(ctx, duckdb.Turn{
		TurnID: turnID, TraceID: turnID + "-trace",
		SessionID: sessionID, SourceApp: "demo",
		StartedAt: startedAt, Status: "completed",
	}))
	end := startedAt.Add(time.Minute)
	dur := end.Sub(startedAt).Milliseconds()
	require.NoError(t, s.UpdateTurnStatus(ctx, turnID, "completed", &end, &dur, 0, 0, 0))
	require.NoError(t, s.UpdateTurnRecap(ctx, turnID, recapJSON, "claude-haiku-4-5", end))
}
