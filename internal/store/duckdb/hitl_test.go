package duckdb

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func mkHITL(id string, sessionID, turnID string, requestedAt time.Time) HITLEvent {
	return HITLEvent{
		HitlID:          id,
		SpanID:          "span-" + id,
		TraceID:         "trace-" + id,
		SessionID:       sessionID,
		TurnID:          turnID,
		Kind:            "permission",
		Status:          HITLStatusPending,
		RequestedAt:     requestedAt,
		Question:        "Allow Bash to run rm -rf /?",
		SuggestionsJSON: `["allow once","always allow"]`,
		ContextJSON:     `{"tool_name":"Bash","command_preview":"rm -rf /"}`,
	}
}

func TestHITLInsertAndGet(t *testing.T) {
	ctx := t.Context()
	s := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	ev := mkHITL("hitl-1", "sess-1", "turn-1", now)
	require.NoError(t, s.InsertHITL(ctx, ev))

	got, ok, err := s.GetHITL(ctx, "hitl-1")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "hitl-1", got.HitlID)
	require.Equal(t, "permission", got.Kind)
	require.Equal(t, HITLStatusPending, got.Status)
	require.Equal(t, "Allow Bash to run rm -rf /?", got.Question)
	require.Equal(t, `["allow once","always allow"]`, got.SuggestionsJSON)
	require.Equal(t, `{"tool_name":"Bash","command_preview":"rm -rf /"}`, got.ContextJSON)

	// Missing row
	_, ok, err = s.GetHITL(ctx, "missing")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestHITLRespondTransitions(t *testing.T) {
	ctx := t.Context()
	s := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	require.NoError(t, s.InsertHITL(ctx, mkHITL("hitl-1", "sess-1", "turn-1", now)))

	resp := HITLResponse{
		Decision:       "allow",
		ReasonCategory: "scope",
		OperatorNote:   "ok for staging",
		ResumeMode:     "continue",
		OperatorID:     "operator-1",
	}
	respondedAt := now.Add(2 * time.Second)
	out, err := s.RespondHITL(ctx, "hitl-1", resp, respondedAt)
	require.NoError(t, err)
	require.Equal(t, HITLStatusResponded, out.Status)
	require.True(t, out.Decision.Valid)
	require.Equal(t, "allow", out.Decision.String)
	require.True(t, out.ReasonCategory.Valid)
	require.Equal(t, "scope", out.ReasonCategory.String)
	require.True(t, out.OperatorNote.Valid)
	require.Equal(t, "ok for staging", out.OperatorNote.String)
	require.True(t, out.ResumeMode.Valid)
	require.Equal(t, "continue", out.ResumeMode.String)
	require.True(t, out.OperatorID.Valid)
	require.Equal(t, "operator-1", out.OperatorID.String)
	require.True(t, out.RespondedAt.Valid)
	require.True(t, out.RespondedAt.Time.Equal(respondedAt))

	// Second respond fails — already finalised.
	_, err = s.RespondHITL(ctx, "hitl-1", resp, respondedAt)
	require.ErrorIs(t, err, ErrHITLAlreadyResponded)

	// Missing row -> ErrHITLNotFound.
	_, err = s.RespondHITL(ctx, "missing", resp, respondedAt)
	require.ErrorIs(t, err, ErrHITLNotFound)
}

func TestHITLExpire(t *testing.T) {
	ctx := t.Context()
	s := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	require.NoError(t, s.InsertHITL(ctx, mkHITL("hitl-1", "sess-1", "turn-1", now)))

	require.NoError(t, s.ExpireHITL(ctx, "hitl-1", now.Add(time.Minute)))
	got, ok, err := s.GetHITL(ctx, "hitl-1")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, HITLStatusExpired, got.Status)

	// Expiring an already-expired row is a no-op.
	require.NoError(t, s.ExpireHITL(ctx, "hitl-1", now.Add(2*time.Minute)))
	got, _, err = s.GetHITL(ctx, "hitl-1")
	require.NoError(t, err)
	require.Equal(t, HITLStatusExpired, got.Status)
}

func TestHITLPendingListings(t *testing.T) {
	ctx := t.Context()
	s := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	require.NoError(t, s.InsertHITL(ctx, mkHITL("hitl-1", "sess-1", "turn-1", now)))
	require.NoError(t, s.InsertHITL(ctx, mkHITL("hitl-2", "sess-1", "turn-1", now.Add(time.Second))))
	require.NoError(t, s.InsertHITL(ctx, mkHITL("hitl-3", "sess-1", "turn-2", now.Add(2*time.Second))))
	require.NoError(t, s.InsertHITL(ctx, mkHITL("hitl-other", "sess-2", "turn-x", now)))

	// Mark hitl-2 responded so the pending lists exclude it.
	_, err := s.RespondHITL(ctx, "hitl-2", HITLResponse{Decision: "deny"}, now.Add(3*time.Second))
	require.NoError(t, err)

	pendingSess, err := s.ListPendingHITLBySession(ctx, "sess-1")
	require.NoError(t, err)
	require.Len(t, pendingSess, 2)
	require.Equal(t, "hitl-1", pendingSess[0].HitlID)
	require.Equal(t, "hitl-3", pendingSess[1].HitlID)

	pendingTurn, err := s.ListPendingHITLByTurn(ctx, "turn-1")
	require.NoError(t, err)
	require.Len(t, pendingTurn, 1)
	require.Equal(t, "hitl-1", pendingTurn[0].HitlID)

	all, err := s.ListHITLByTurn(ctx, "turn-1")
	require.NoError(t, err)
	require.Len(t, all, 2)

	// Filter listings.
	responded, err := s.ListRecentHITL(ctx, HITLFilter{SessionID: "sess-1", Status: HITLStatusResponded}, 0)
	require.NoError(t, err)
	require.Len(t, responded, 1)
	require.Equal(t, "hitl-2", responded[0].HitlID)

	byKind, err := s.ListRecentHITL(ctx, HITLFilter{Kind: "permission"}, 0)
	require.NoError(t, err)
	require.Len(t, byKind, 4)
}

func TestHITLExpiredCandidates(t *testing.T) {
	ctx := t.Context()
	s := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	require.NoError(t, s.InsertHITL(ctx, mkHITL("old-1", "sess-1", "turn-1", now.Add(-10*time.Minute))))
	require.NoError(t, s.InsertHITL(ctx, mkHITL("recent", "sess-1", "turn-1", now)))

	cands, err := s.ListExpiredCandidates(ctx, now.Add(-time.Minute), 100)
	require.NoError(t, err)
	require.Len(t, cands, 1)
	require.Equal(t, "old-1", cands[0].HitlID)
}

func TestHITLCountPendingBySession(t *testing.T) {
	ctx := t.Context()
	s := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	require.NoError(t, s.InsertHITL(ctx, mkHITL("hitl-1", "sess-1", "turn-1", now)))
	require.NoError(t, s.InsertHITL(ctx, mkHITL("hitl-2", "sess-1", "turn-1", now)))
	require.NoError(t, s.InsertHITL(ctx, mkHITL("hitl-3", "sess-2", "turn-x", now)))

	counts, err := s.CountPendingHITLEventsBySession(ctx, []string{"sess-1", "sess-2", "sess-empty"})
	require.NoError(t, err)
	require.EqualValues(t, 2, counts["sess-1"])
	require.EqualValues(t, 1, counts["sess-2"])
	require.Zero(t, counts["sess-empty"])
}

// Sanity-check that scanHITL handles the nullable sql.* fields when a row
// has just been inserted with empty optional values.
func TestHITLScanNullables(t *testing.T) {
	ctx := t.Context()
	s := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	require.NoError(t, s.InsertHITL(ctx, mkHITL("hitl-1", "sess-1", "turn-1", now)))

	got, _, err := s.GetHITL(ctx, "hitl-1")
	require.NoError(t, err)
	require.Equal(t, sql.NullString{}, got.Decision)
	require.Equal(t, sql.NullString{}, got.ReasonCategory)
	require.Equal(t, sql.NullString{}, got.OperatorNote)
	require.Equal(t, sql.NullString{}, got.ResumeMode)
	require.Equal(t, sql.NullString{}, got.OperatorID)
	require.False(t, got.RespondedAt.Valid)
}
