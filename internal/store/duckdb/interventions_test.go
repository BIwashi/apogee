package duckdb

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func baseInterventionRequest(sessionID, turnID string) InterventionRequest {
	return InterventionRequest{
		SessionID:    sessionID,
		TurnID:       turnID,
		OperatorID:   "op-1",
		Message:      "stop and reconsider",
		DeliveryMode: InterventionModeInterrupt,
		Scope:        InterventionScopeTurn,
		Urgency:      InterventionUrgencyNormal,
		TTL:          5 * time.Minute,
	}
}

func TestInterventionHappyPath(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	req := baseInterventionRequest("sess-1", "turn-1")
	iv, err := s.InsertIntervention(ctx, req)
	require.NoError(t, err)
	require.Equal(t, InterventionStatusQueued, iv.Status)
	require.NotEmpty(t, iv.InterventionID)
	require.WithinDuration(t, time.Now().UTC(), iv.CreatedAt, 5*time.Second)

	// Claim on PreToolUse picks it up.
	claimed, ok, err := s.ClaimNextIntervention(ctx, "sess-1", "turn-1", HookEventPreToolUse)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, iv.InterventionID, claimed.InterventionID)
	require.Equal(t, InterventionStatusClaimed, claimed.Status)
	require.True(t, claimed.ClaimedAt.Valid)

	// A second claim right away gets nothing (row is no longer queued).
	_, ok, err = s.ClaimNextIntervention(ctx, "sess-1", "turn-1", HookEventPreToolUse)
	require.NoError(t, err)
	require.False(t, ok)

	// Delivered.
	delivered, err := s.MarkInterventionDelivered(ctx, iv.InterventionID, HookEventPreToolUse)
	require.NoError(t, err)
	require.Equal(t, InterventionStatusDelivered, delivered.Status)
	require.True(t, delivered.DeliveredVia.Valid)
	require.Equal(t, HookEventPreToolUse, delivered.DeliveredVia.String)

	// Consumed.
	consumed, err := s.MarkInterventionConsumed(ctx, iv.InterventionID, 42)
	require.NoError(t, err)
	require.Equal(t, InterventionStatusConsumed, consumed.Status)
	require.True(t, consumed.ConsumedEventID.Valid)
	require.EqualValues(t, 42, consumed.ConsumedEventID.Int64)
}

func TestInterventionPriorityHighBeatsFIFO(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Two normal, then one high submitted last.
	req := baseInterventionRequest("sess-1", "turn-1")
	req.Message = "first normal"
	_, err := s.InsertIntervention(ctx, req)
	require.NoError(t, err)

	// Give the second normal a noticeably later created_at so FIFO ordering
	// is unambiguous.
	time.Sleep(5 * time.Millisecond)
	req.Message = "second normal"
	_, err = s.InsertIntervention(ctx, req)
	require.NoError(t, err)

	time.Sleep(5 * time.Millisecond)
	req.Message = "urgent"
	req.Urgency = InterventionUrgencyHigh
	high, err := s.InsertIntervention(ctx, req)
	require.NoError(t, err)

	claimed, ok, err := s.ClaimNextIntervention(ctx, "sess-1", "turn-1", HookEventPreToolUse)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, high.InterventionID, claimed.InterventionID, "high urgency should beat FIFO")
}

func TestInterventionScopeTurnIsolated(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	req := baseInterventionRequest("sess-1", "turn-A")
	_, err := s.InsertIntervention(ctx, req)
	require.NoError(t, err)

	// A hook firing for turn-B must not claim turn-A's intervention.
	_, ok, err := s.ClaimNextIntervention(ctx, "sess-1", "turn-B", HookEventPreToolUse)
	require.NoError(t, err)
	require.False(t, ok)

	// The same hook firing for turn-A (or for the session-scoped case) does.
	_, ok, err = s.ClaimNextIntervention(ctx, "sess-1", "turn-A", HookEventPreToolUse)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestInterventionSessionScopeClaimedByAnyTurn(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Session-scoped intervention: turn_id left blank.
	req := baseInterventionRequest("sess-1", "")
	req.Scope = InterventionScopeSession
	_, err := s.InsertIntervention(ctx, req)
	require.NoError(t, err)

	// Any turn in the session can pick it up.
	claimed, ok, err := s.ClaimNextIntervention(ctx, "sess-1", "turn-whatever", HookEventPreToolUse)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, InterventionScopeSession, claimed.Scope)
}

func TestInterventionModeFilterPreToolUse(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// context-only mode should NOT be claimable by PreToolUse.
	req := baseInterventionRequest("sess-1", "turn-1")
	req.DeliveryMode = InterventionModeContext
	_, err := s.InsertIntervention(ctx, req)
	require.NoError(t, err)

	_, ok, err := s.ClaimNextIntervention(ctx, "sess-1", "turn-1", HookEventPreToolUse)
	require.NoError(t, err)
	require.False(t, ok)

	// UserPromptSubmit picks it up.
	_, ok, err = s.ClaimNextIntervention(ctx, "sess-1", "turn-1", HookEventUserPromptSubmit)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestInterventionModeBothClaimedByEither(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	req := baseInterventionRequest("sess-1", "turn-1")
	req.DeliveryMode = InterventionModeBoth
	_, err := s.InsertIntervention(ctx, req)
	require.NoError(t, err)

	_, ok, err := s.ClaimNextIntervention(ctx, "sess-1", "turn-1", HookEventPreToolUse)
	require.NoError(t, err)
	require.True(t, ok)

	// Second row: delivered via UserPromptSubmit instead.
	_, err = s.InsertIntervention(ctx, req)
	require.NoError(t, err)
	_, ok, err = s.ClaimNextIntervention(ctx, "sess-1", "turn-1", HookEventUserPromptSubmit)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestInterventionConcurrentClaims(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	req := baseInterventionRequest("sess-1", "turn-1")
	_, err := s.InsertIntervention(ctx, req)
	require.NoError(t, err)

	const workers = 10
	var winners int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, ok, err := s.ClaimNextIntervention(ctx, "sess-1", "turn-1", HookEventPreToolUse)
			if err != nil {
				t.Errorf("claim: %v", err)
				return
			}
			if ok {
				atomic.AddInt64(&winners, 1)
			}
		}()
	}
	close(start)
	wg.Wait()
	require.EqualValues(t, 1, atomic.LoadInt64(&winners), "exactly one claimant should win")
}

func TestInterventionCancelTransitions(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	req := baseInterventionRequest("sess-1", "turn-1")
	iv, err := s.InsertIntervention(ctx, req)
	require.NoError(t, err)

	// Cancel a queued row.
	cancelled, err := s.CancelIntervention(ctx, iv.InterventionID)
	require.NoError(t, err)
	require.Equal(t, InterventionStatusCancelled, cancelled.Status)

	// Cancelling again is an error.
	_, err = s.CancelIntervention(ctx, iv.InterventionID)
	require.ErrorIs(t, err, ErrInterventionImmutable)

	// Insert another, claim, then cancel — still allowed.
	iv2, err := s.InsertIntervention(ctx, req)
	require.NoError(t, err)
	_, ok, err := s.ClaimNextIntervention(ctx, "sess-1", "turn-1", HookEventPreToolUse)
	require.NoError(t, err)
	require.True(t, ok)
	cancelled2, err := s.CancelIntervention(ctx, iv2.InterventionID)
	require.NoError(t, err)
	require.Equal(t, InterventionStatusCancelled, cancelled2.Status)

	// Insert a third, run through to delivered, and confirm cancel fails.
	iv3, err := s.InsertIntervention(ctx, req)
	require.NoError(t, err)
	_, ok, err = s.ClaimNextIntervention(ctx, "sess-1", "turn-1", HookEventPreToolUse)
	require.NoError(t, err)
	require.True(t, ok)
	_, err = s.MarkInterventionDelivered(ctx, iv3.InterventionID, HookEventPreToolUse)
	require.NoError(t, err)
	_, err = s.CancelIntervention(ctx, iv3.InterventionID)
	require.ErrorIs(t, err, ErrInterventionImmutable)
}

func TestInterventionAutoExpireScan(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Stale row with a 1 ms TTL.
	stale := baseInterventionRequest("sess-1", "turn-1")
	stale.TTL = time.Millisecond
	ivStale, err := s.InsertIntervention(ctx, stale)
	require.NoError(t, err)

	// Fresh row that shouldn't be swept.
	fresh := baseInterventionRequest("sess-1", "turn-1")
	fresh.TTL = time.Hour
	_, err = s.InsertIntervention(ctx, fresh)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)

	now := time.Now().UTC()
	candidates, err := s.ListInterventionsToAutoExpire(ctx, now, 100)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, ivStale.InterventionID, candidates[0].InterventionID)

	// Expire it and confirm the list drains.
	_, err = s.ExpireIntervention(ctx, ivStale.InterventionID)
	require.NoError(t, err)
	candidates, err = s.ListInterventionsToAutoExpire(ctx, now, 100)
	require.NoError(t, err)
	require.Len(t, candidates, 0)
}

func TestInterventionListingsAndPending(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	req := baseInterventionRequest("sess-1", "turn-1")
	ivA, err := s.InsertIntervention(ctx, req)
	require.NoError(t, err)
	time.Sleep(5 * time.Millisecond)
	ivB, err := s.InsertIntervention(ctx, req)
	require.NoError(t, err)

	// Cancel ivA so the pending list only returns ivB.
	_, err = s.CancelIntervention(ctx, ivA.InterventionID)
	require.NoError(t, err)

	pending, err := s.ListPendingInterventionsBySession(ctx, "sess-1")
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, ivB.InterventionID, pending[0].InterventionID)

	all, err := s.ListInterventionsBySession(ctx, "sess-1", 10)
	require.NoError(t, err)
	require.Len(t, all, 2)

	byTurn, err := s.ListPendingInterventionsByTurn(ctx, "turn-1")
	require.NoError(t, err)
	require.Len(t, byTurn, 1)
}
