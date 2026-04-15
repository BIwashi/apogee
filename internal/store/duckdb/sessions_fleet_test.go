package duckdb

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/BIwashi/apogee/internal/otel"
)

// seedSession writes a session row with a baseline set of fields so each
// fleet-view test can focus on the columns it cares about.
func seedSession(t *testing.T, s *Store, id, app string, lastSeen time.Time) {
	t.Helper()
	require.NoError(t, s.UpsertSession(t.Context(), Session{
		SessionID:  id,
		SourceApp:  app,
		StartedAt:  lastSeen.Add(-time.Minute),
		LastSeenAt: lastSeen,
	}))
}

func TestUpdateSessionAttentionRoundTrip(t *testing.T) {
	ctx := t.Context()
	s := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	seedSession(t, s, "sess-1", "demo", now)
	require.NoError(t, s.UpdateSessionAttention(ctx,
		"sess-1",
		"intervene_now",
		"HITL pending 45s",
		0.82,
		"turn-current",
		"debug",
		"live",
	))

	got, err := s.GetSession(ctx, "sess-1")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "intervene_now", got.AttentionState)
	require.Equal(t, "HITL pending 45s", got.AttentionReason)
	require.NotNil(t, got.AttentionScore)
	require.InDelta(t, 0.82, *got.AttentionScore, 1e-9)
	require.Equal(t, "turn-current", got.CurrentTurnID)
	require.Equal(t, "debug", got.CurrentPhase)
	require.Equal(t, "live", got.LiveState)
}

func TestUpdateSessionLiveStatusRoundTrip(t *testing.T) {
	ctx := t.Context()
	s := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	seedSession(t, s, "sess-1", "demo", now)
	require.NoError(t, s.UpdateSessionLiveStatus(ctx, "sess-1", "Editing TriageRail.tsx", "claude-haiku-4-5", now))

	got, err := s.GetSession(ctx, "sess-1")
	require.NoError(t, err)
	require.Equal(t, "Editing TriageRail.tsx", got.LiveStatusText)
	require.Equal(t, "claude-haiku-4-5", got.LiveStatusModel)
	require.NotNil(t, got.LiveStatusAt)
}

func TestListActiveSessionsAttentionOrdering(t *testing.T) {
	ctx := t.Context()
	s := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	// Seed three sessions at different attention levels. ListActiveSessions
	// should hoist the most urgent to the front regardless of recency.
	seedSession(t, s, "calm", "demo", now)
	require.NoError(t, s.UpdateSessionAttention(ctx, "calm", "healthy", "", 0.1, "t-calm", "implement", "live"))

	seedSession(t, s, "warn", "demo", now.Add(-time.Minute))
	require.NoError(t, s.UpdateSessionAttention(ctx, "warn", "watch", "1 error streak", 0.5, "t-warn", "debug", "live"))

	seedSession(t, s, "urgent", "demo", now.Add(-2*time.Minute))
	require.NoError(t, s.UpdateSessionAttention(ctx, "urgent", "intervene_now", "HITL pending 60s", 0.9, "t-urgent", "debug", "live"))

	out, err := s.ListActiveSessions(ctx, SessionFilter{}, 10)
	require.NoError(t, err)
	require.Len(t, out, 3)
	require.Equal(t, "urgent", out[0].SessionID)
	require.Equal(t, "warn", out[1].SessionID)
	require.Equal(t, "calm", out[2].SessionID)
}

func TestListActiveSessionsSinceFilter(t *testing.T) {
	ctx := t.Context()
	s := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	seedSession(t, s, "in-window", "demo", now)
	seedSession(t, s, "out-of-window", "demo", now.Add(-2*time.Hour))

	since := now.Add(-time.Hour)
	out, err := s.ListActiveSessions(ctx, SessionFilter{Since: &since}, 10)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, "in-window", out[0].SessionID)
}

func TestCountSessionAttentionBuckets(t *testing.T) {
	ctx := t.Context()
	s := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	seedSession(t, s, "a", "demo", now)
	require.NoError(t, s.UpdateSessionAttention(ctx, "a", "intervene_now", "", 0.9, "t", "", "live"))
	seedSession(t, s, "b", "demo", now)
	require.NoError(t, s.UpdateSessionAttention(ctx, "b", "intervene_now", "", 0.9, "t", "", "live"))
	seedSession(t, s, "c", "demo", now)
	require.NoError(t, s.UpdateSessionAttention(ctx, "c", "watch", "", 0.5, "t", "", "live"))
	seedSession(t, s, "d", "demo", now)
	require.NoError(t, s.UpdateSessionAttention(ctx, "d", "healthy", "", 0.1, "t", "", "live"))
	// One session with no attention at all — should land in healthy.
	seedSession(t, s, "legacy", "demo", now)

	counts, err := s.CountSessionAttention(ctx, SessionFilter{})
	require.NoError(t, err)
	require.Equal(t, 2, counts.InterveneNow)
	require.Equal(t, 1, counts.Watch)
	require.Equal(t, 0, counts.Watchlist)
	require.Equal(t, 2, counts.Healthy) // d + legacy
	require.Equal(t, 5, counts.Total)
}

func TestRepresentativeTurnPrefersRunning(t *testing.T) {
	ctx := t.Context()
	s := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	seedSession(t, s, "sess-1", "demo", now)

	// A closed turn from earlier.
	require.NoError(t, s.InsertTurn(ctx, Turn{
		TurnID:    "closed",
		TraceID:   "trace-closed",
		SessionID: "sess-1",
		SourceApp: "demo",
		StartedAt: now.Add(-10 * time.Minute),
		Status:    "completed",
	}))
	// A running turn — should win.
	require.NoError(t, s.InsertTurn(ctx, Turn{
		TurnID:    "running",
		TraceID:   "trace-running",
		SessionID: "sess-1",
		SourceApp: "demo",
		StartedAt: now.Add(-time.Minute),
		Status:    "running",
	}))

	rep, err := s.RepresentativeTurn(ctx, "sess-1")
	require.NoError(t, err)
	require.NotNil(t, rep)
	require.Equal(t, "running", rep.TurnID)
}

func TestRepresentativeTurnFallsBackToClosed(t *testing.T) {
	ctx := t.Context()
	s := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	seedSession(t, s, "sess-1", "demo", now)
	require.NoError(t, s.InsertTurn(ctx, Turn{
		TurnID:    "closed",
		TraceID:   "trace-closed",
		SessionID: "sess-1",
		SourceApp: "demo",
		StartedAt: now.Add(-time.Minute),
		Status:    "completed",
	}))

	rep, err := s.RepresentativeTurn(ctx, "sess-1")
	require.NoError(t, err)
	require.NotNil(t, rep)
	require.Equal(t, "closed", rep.TurnID)
}

func TestListActiveSessionCardsEnrichment(t *testing.T) {
	ctx := t.Context()
	s := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	seedSession(t, s, "sess-1", "demo", now)
	require.NoError(t, s.UpdateSessionAttention(ctx, "sess-1", "watch", "", 0.5, "turn-1", "implement", "live"))

	// Attach a tool span — current_tool should reflect the most recent
	// non-empty tool_name.
	require.NoError(t, s.InsertSpan(ctx, &otel.Span{
		TraceID:    otel.TraceID("00112233445566778899aabbccddeeff"),
		SpanID:     otel.SpanID("aaaaaaaaaaaaaaaa"),
		Name:       "claude_code.tool.Bash",
		Kind:       otel.SpanKindInternal,
		StartTime:  now.Add(-30 * time.Second),
		StatusCode: otel.StatusUnset,
		SessionID:  "sess-1",
		TurnID:     "turn-1",
		ToolName:   "Bash",
		HookEvent:  "PreToolUse",
	}))
	require.NoError(t, s.InsertSpan(ctx, &otel.Span{
		TraceID:    otel.TraceID("00112233445566778899aabbccddeeff"),
		SpanID:     otel.SpanID("bbbbbbbbbbbbbbbb"),
		Name:       "claude_code.tool.Edit",
		Kind:       otel.SpanKindInternal,
		StartTime:  now.Add(-5 * time.Second),
		StatusCode: otel.StatusUnset,
		SessionID:  "sess-1",
		TurnID:     "turn-1",
		ToolName:   "Edit",
		HookEvent:  "PreToolUse",
	}))

	// One pending HITL, one already-responded HITL.
	require.NoError(t, s.InsertHITL(ctx, HITLEvent{
		HitlID:      "hitl-pending",
		SpanID:      "span-1",
		TraceID:     "trace-1",
		SessionID:   "sess-1",
		TurnID:      "turn-1",
		Kind:        "permission",
		Status:      HITLStatusPending,
		RequestedAt: now,
		Question:    "Allow?",
	}))
	require.NoError(t, s.InsertHITL(ctx, HITLEvent{
		HitlID:      "hitl-done",
		SpanID:      "span-2",
		TraceID:     "trace-2",
		SessionID:   "sess-1",
		TurnID:      "turn-1",
		Kind:        "permission",
		Status:      "responded",
		RequestedAt: now,
		Question:    "Allow?",
	}))

	// One queued intervention — should count; one consumed — should not.
	_, err := s.InsertIntervention(ctx, InterventionRequest{
		SessionID:    "sess-1",
		Message:      "hey",
		DeliveryMode: InterventionModeContext,
		Scope:        InterventionScopeSession,
		Urgency:      InterventionUrgencyNormal,
	})
	require.NoError(t, err)

	cards, err := s.ListActiveSessionCards(ctx, SessionFilter{}, 10)
	require.NoError(t, err)
	require.Len(t, cards, 1)
	card := cards[0]
	require.Equal(t, "sess-1", card.SessionID)
	require.Equal(t, "watch", card.AttentionState)
	require.Equal(t, "Edit", card.CurrentTool, "current_tool should pick the newest span")
	require.Equal(t, 1, card.HITLPendingCount, "only pending HITL should count")
	require.Equal(t, 1, card.InterventionPendingCount)
	require.NotNil(t, card.LastSpanAt)
}
