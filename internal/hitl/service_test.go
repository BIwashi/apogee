package hitl

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/BIwashi/apogee/internal/sse"
	"github.com/BIwashi/apogee/internal/store/duckdb"
)

func newTestStore(t *testing.T) *duckdb.Store {
	t.Helper()
	ctx := t.Context()
	s, err := duckdb.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func mkPending(id string, requestedAt time.Time) duckdb.HITLEvent {
	return duckdb.HITLEvent{
		HitlID:          id,
		SpanID:          "span-" + id,
		TraceID:         "trace-" + id,
		SessionID:       "sess-1",
		TurnID:          "turn-1",
		Kind:            "permission",
		Status:          duckdb.HITLStatusPending,
		RequestedAt:     requestedAt,
		Question:        "Allow it?",
		SuggestionsJSON: `["yes","no"]`,
		ContextJSON:     `{"tool_name":"Bash"}`,
	}
}

func TestServiceRespondBroadcastsAndUpdatesStore(t *testing.T) {
	ctx := t.Context()
	store := newTestStore(t)
	hub := sse.NewHub(slog.Default())
	now := time.Date(2026, 4, 14, 10, 0, 0, 0, time.UTC)
	svc := New(store, hub, DefaultConfig(), slog.Default())
	svc.SetClock(func() time.Time { return now })

	require.NoError(t, store.InsertHITL(ctx, mkPending("hitl-1", now.Add(-2*time.Second))))

	closed := false
	svc.CloseHITLSpan = func(ctx context.Context, ev duckdb.HITLEvent) {
		closed = true
		require.Equal(t, "hitl-1", ev.HitlID)
	}

	sub := hub.Subscribe(sse.Filter{})
	defer hub.Unsubscribe(sub)

	out, err := svc.Respond(ctx, "hitl-1", duckdb.HITLResponse{
		Decision:       "allow",
		ReasonCategory: "scope",
		OperatorNote:   "ok",
		ResumeMode:     "continue",
	})
	require.NoError(t, err)
	require.True(t, closed)
	require.Equal(t, duckdb.HITLStatusResponded, out.Status)

	select {
	case ev := <-sub.C():
		require.Equal(t, sse.EventHITLResponded, ev.Type)
		var payload sse.HITLPayload
		require.NoError(t, json.Unmarshal(ev.Data, &payload))
		require.Equal(t, "hitl-1", payload.HITL.HitlID)
		require.Equal(t, "responded", payload.HITL.Status)
		require.NotNil(t, payload.HITL.Decision)
		require.Equal(t, "allow", *payload.HITL.Decision)
	case <-time.After(time.Second):
		t.Fatal("expected hitl.responded broadcast")
	}
}

func TestServiceRespondConflictAndMissing(t *testing.T) {
	ctx := t.Context()
	store := newTestStore(t)
	svc := New(store, sse.NewHub(slog.Default()), DefaultConfig(), slog.Default())

	require.NoError(t, store.InsertHITL(ctx, mkPending("hitl-1", time.Now())))
	_, err := svc.Respond(ctx, "hitl-1", duckdb.HITLResponse{Decision: "allow"})
	require.NoError(t, err)

	_, err = svc.Respond(ctx, "hitl-1", duckdb.HITLResponse{Decision: "deny"})
	require.ErrorIs(t, err, duckdb.ErrHITLAlreadyResponded)

	_, err = svc.Respond(ctx, "missing", duckdb.HITLResponse{Decision: "allow"})
	require.ErrorIs(t, err, duckdb.ErrHITLNotFound)

	_, err = svc.Respond(ctx, "hitl-1", duckdb.HITLResponse{})
	require.Error(t, err)
}

func TestServiceExpireOnceFlipsStaleRows(t *testing.T) {
	ctx := t.Context()
	store := newTestStore(t)
	hub := sse.NewHub(slog.Default())
	cfg := Config{AutoExpireSeconds: 30, TickInterval: time.Second}
	svc := New(store, hub, cfg, slog.Default())
	now := time.Date(2026, 4, 14, 10, 0, 0, 0, time.UTC)
	svc.SetClock(func() time.Time { return now })

	require.NoError(t, store.InsertHITL(ctx, mkPending("old", now.Add(-time.Hour))))
	require.NoError(t, store.InsertHITL(ctx, mkPending("fresh", now)))

	sub := hub.Subscribe(sse.Filter{})
	defer hub.Unsubscribe(sub)

	svc.ExpireOnce(ctx)

	old, _, err := store.GetHITL(ctx, "old")
	require.NoError(t, err)
	require.Equal(t, duckdb.HITLStatusExpired, old.Status)
	fresh, _, err := store.GetHITL(ctx, "fresh")
	require.NoError(t, err)
	require.Equal(t, duckdb.HITLStatusPending, fresh.Status)

	// Drain the broadcast.
	select {
	case ev := <-sub.C():
		require.Equal(t, sse.EventHITLExpired, ev.Type)
	case <-time.After(time.Second):
		t.Fatal("expected hitl.expired broadcast")
	}
}

func TestServiceBroadcastRequested(t *testing.T) {
	ctx := t.Context()
	store := newTestStore(t)
	hub := sse.NewHub(slog.Default())
	svc := New(store, hub, DefaultConfig(), slog.Default())

	now := time.Now().UTC().Truncate(time.Millisecond)
	ev := mkPending("hitl-1", now)
	require.NoError(t, store.InsertHITL(ctx, ev))

	sub := hub.Subscribe(sse.Filter{})
	defer hub.Unsubscribe(sub)

	svc.BroadcastRequested(ev)
	select {
	case got := <-sub.C():
		require.Equal(t, sse.EventHITLRequested, got.Type)
	case <-time.After(time.Second):
		t.Fatal("expected hitl.requested broadcast")
	}
}
