package interventions

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/BIwashi/apogee/internal/sse"
	"github.com/BIwashi/apogee/internal/store/duckdb"
)

func newServiceFixture(t *testing.T) (*Service, *duckdb.Store, *sse.Subscription) {
	t.Helper()
	ctx := context.Background()
	store, err := duckdb.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	hub := sse.NewHub(nil)
	sub := hub.Subscribe(sse.Filter{})
	t.Cleanup(func() { hub.Unsubscribe(sub) })
	svc := NewService(Config{
		AutoExpireTTL:     time.Minute,
		SweepInterval:     time.Second,
		BothFallbackAfter: time.Second,
		MaxMessageChars:   4096,
	}, store, hub, nil)
	return svc, store, sub
}

func drainEvents(t *testing.T, sub *sse.Subscription) []sse.Event {
	t.Helper()
	var out []sse.Event
	deadline := time.After(200 * time.Millisecond)
	for {
		select {
		case ev := <-sub.C():
			out = append(out, ev)
		case <-deadline:
			return out
		}
	}
}

func eventTypes(events []sse.Event) []string {
	out := make([]string, 0, len(events))
	for _, ev := range events {
		out = append(out, ev.Type)
	}
	return out
}

func baseReq() duckdb.InterventionRequest {
	return duckdb.InterventionRequest{
		SessionID:    "sess-1",
		TurnID:       "turn-1",
		Message:      "stop and reconsider",
		DeliveryMode: duckdb.InterventionModeInterrupt,
		Scope:        duckdb.InterventionScopeTurn,
		Urgency:      duckdb.InterventionUrgencyNormal,
	}
}

func TestServiceSubmitBroadcasts(t *testing.T) {
	svc, _, sub := newServiceFixture(t)
	ctx := context.Background()

	iv, err := svc.Submit(ctx, baseReq())
	require.NoError(t, err)
	require.Equal(t, duckdb.InterventionStatusQueued, iv.Status)

	events := drainEvents(t, sub)
	require.Len(t, events, 1)
	require.Equal(t, sse.EventInterventionSubmitted, events[0].Type)

	// Event payload round-trips cleanly.
	var payload sse.InterventionPayload
	require.NoError(t, json.Unmarshal(events[0].Data, &payload))
	require.Equal(t, iv.InterventionID, payload.Intervention.InterventionID)
	require.Equal(t, "turn-1", payload.Intervention.TurnID)
}

func TestServiceValidateRejectsBadInputs(t *testing.T) {
	svc, _, _ := newServiceFixture(t)
	ctx := context.Background()

	_, err := svc.Submit(ctx, duckdb.InterventionRequest{})
	require.ErrorIs(t, err, ErrSessionRequired)

	_, err = svc.Submit(ctx, duckdb.InterventionRequest{SessionID: "sess-1"})
	require.ErrorIs(t, err, ErrMessageRequired)

	bad := baseReq()
	bad.DeliveryMode = "nope"
	_, err = svc.Submit(ctx, bad)
	require.ErrorIs(t, err, ErrInvalidDeliveryMode)

	bad = baseReq()
	bad.Scope = "nope"
	_, err = svc.Submit(ctx, bad)
	require.ErrorIs(t, err, ErrInvalidScope)

	bad = baseReq()
	bad.Urgency = "nope"
	_, err = svc.Submit(ctx, bad)
	require.ErrorIs(t, err, ErrInvalidUrgency)
}

func TestServiceCancelBroadcasts(t *testing.T) {
	svc, _, sub := newServiceFixture(t)
	ctx := context.Background()
	iv, err := svc.Submit(ctx, baseReq())
	require.NoError(t, err)

	_, err = svc.Cancel(ctx, iv.InterventionID)
	require.NoError(t, err)

	events := drainEvents(t, sub)
	require.Contains(t, eventTypes(events), sse.EventInterventionSubmitted)
	require.Contains(t, eventTypes(events), sse.EventInterventionCancelled)
}

func TestServiceSweepExpiresStale(t *testing.T) {
	svc, _, sub := newServiceFixture(t)
	ctx := context.Background()

	req := baseReq()
	req.TTL = time.Millisecond
	_, err := svc.Submit(ctx, req)
	require.NoError(t, err)
	time.Sleep(5 * time.Millisecond)
	_ = drainEvents(t, sub) // drain submitted broadcast

	svc.SweepOnce(ctx)

	events := drainEvents(t, sub)
	require.NotEmpty(t, events)
	require.Equal(t, sse.EventInterventionExpired, events[len(events)-1].Type)
}

func TestServiceClaimDeliverConsumedBroadcasts(t *testing.T) {
	svc, _, sub := newServiceFixture(t)
	ctx := context.Background()
	iv, err := svc.Submit(ctx, baseReq())
	require.NoError(t, err)

	claimed, ok, err := svc.Claim(ctx, iv.SessionID, iv.TurnID.String, duckdb.HookEventPreToolUse)
	require.NoError(t, err)
	require.True(t, ok)

	_, err = svc.Delivered(ctx, claimed.InterventionID, duckdb.HookEventPreToolUse)
	require.NoError(t, err)

	_, err = svc.Consumed(ctx, claimed.InterventionID, 42)
	require.NoError(t, err)

	events := drainEvents(t, sub)
	types := eventTypes(events)
	require.Contains(t, types, sse.EventInterventionSubmitted)
	require.Contains(t, types, sse.EventInterventionClaimed)
	require.Contains(t, types, sse.EventInterventionDelivered)
	require.Contains(t, types, sse.EventInterventionConsumed)

	// Ordering invariant the UI depends on: submitted → claimed → delivered → consumed.
	idx := func(kind string) int {
		for i, tt := range types {
			if tt == kind {
				return i
			}
		}
		return -1
	}
	require.Less(t, idx(sse.EventInterventionSubmitted), idx(sse.EventInterventionClaimed))
	require.Less(t, idx(sse.EventInterventionClaimed), idx(sse.EventInterventionDelivered))
	require.Less(t, idx(sse.EventInterventionDelivered), idx(sse.EventInterventionConsumed))
}

func TestServiceExpireForTurn(t *testing.T) {
	svc, _, sub := newServiceFixture(t)
	ctx := context.Background()
	_, err := svc.Submit(ctx, baseReq())
	require.NoError(t, err)
	// Turn close → expire.
	require.NoError(t, svc.ExpireForTurn(ctx, "turn-1"))
	events := drainEvents(t, sub)
	require.Contains(t, eventTypes(events), sse.EventInterventionExpired)
}

func TestServiceStartStopSafeOnShutdown(t *testing.T) {
	svc, _, _ := newServiceFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)
	// Immediate Stop must drain cleanly — no panic on a fresh sweeper.
	svc.Stop()
}
