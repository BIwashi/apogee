package summarizer

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/BIwashi/apogee/internal/otel"
	"github.com/BIwashi/apogee/internal/sse"
	"github.com/BIwashi/apogee/internal/store/duckdb"
)

// seedTurn inserts a minimal turn with one tool span so the worker has
// something to summarise.
func seedTurn(t *testing.T, store *duckdb.Store, turnID string, durationMs int64) {
	t.Helper()
	ctx := context.Background()
	start := time.Now().Add(-10 * time.Second)
	end := start.Add(time.Duration(durationMs) * time.Millisecond)

	require.NoError(t, store.UpsertSession(ctx, duckdb.Session{
		SessionID:  "sess-" + turnID,
		SourceApp:  "apogee-test",
		StartedAt:  start,
		LastSeenAt: start,
	}))
	require.NoError(t, store.InsertTurn(ctx, duckdb.Turn{
		TurnID:     turnID,
		TraceID:    "trace-" + turnID,
		SessionID:  "sess-" + turnID,
		SourceApp:  "apogee-test",
		StartedAt:  start,
		Status:     "running",
		PromptText: "do the thing",
	}))

	spanEnd := start.Add(200 * time.Millisecond)
	sp := &otel.Span{
		TraceID:     otel.TraceID("trace-" + turnID),
		SpanID:      otel.SpanID("span-1"),
		Name:        "claude_code.tool.Edit",
		Kind:        otel.SpanKindInternal,
		StartTime:   start,
		EndTime:     &spanEnd,
		StatusCode:  otel.StatusOK,
		ServiceName: "claude-code",
		SessionID:   "sess-" + turnID,
		TurnID:      turnID,
		ToolName:    "Edit",
		HookEvent:   "PostToolUse",
	}
	require.NoError(t, store.InsertSpan(ctx, sp))

	require.NoError(t, store.UpdateTurnStatus(ctx, turnID, "completed", &end, &durationMs, 1, 0, 0))
}

func TestWorkerProcessesJob(t *testing.T) {
	ctx := context.Background()
	store, err := duckdb.Open(ctx, ":memory:")
	require.NoError(t, err)
	defer store.Close()

	seedTurn(t, store, "turn-ok", 5000)

	fake := &FakeRunner{Responder: func(model, prompt string) (string, error) {
		return `{"headline":"did work","outcome":"success","phases":[{"name":"edit","start_span_index":0,"end_span_index":0,"summary":"applied edit"}],"key_steps":["a","b","c"],"failure_cause":null,"notable_events":[]}`, nil
	}}
	hub := sse.NewHub(nil)
	cfg := Default()
	cfg.Concurrency = 1
	cfg.QueueSize = 8

	w := NewWorker(cfg, fake, store, hub, nil)
	w.Start(ctx)
	defer w.Stop()

	sub := hub.Subscribe(sse.Filter{})
	defer hub.Unsubscribe(sub)

	w.Enqueue("turn-ok", ReasonTurnClosed)

	deadline := time.Now().Add(3 * time.Second)
	var gotRecap bool
	for time.Now().Before(deadline) {
		recap, ok, err := store.GetTurnRecap(ctx, "turn-ok")
		require.NoError(t, err)
		if ok && recap.RecapJSON != "" {
			gotRecap = true
			require.Contains(t, recap.RecapJSON, "did work")
			require.Equal(t, "claude-haiku-4-5", recap.Model)
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	require.True(t, gotRecap, "recap never landed")

	// Fake runner must have been called with a prompt that includes our seed.
	require.Equal(t, 1, fake.Calls)
	require.Contains(t, fake.LastPrompt, "claude_code.tool.Edit")

	// SSE hub should have fanned a turn.updated event.
	select {
	case ev := <-sub.C():
		require.Equal(t, sse.EventTypeTurnUpdated, ev.Type)
	case <-time.After(time.Second):
		t.Fatal("no SSE broadcast")
	}
}

func TestWorkerSkipsShortTurn(t *testing.T) {
	ctx := context.Background()
	store, err := duckdb.Open(ctx, ":memory:")
	require.NoError(t, err)
	defer store.Close()

	seedTurn(t, store, "turn-short", 100) // below 1500ms floor

	fake := &FakeRunner{Responder: func(model, prompt string) (string, error) {
		return "SHOULD NOT BE CALLED", nil
	}}
	cfg := Default()
	w := NewWorker(cfg, fake, store, nil, nil)
	w.Start(ctx)
	defer w.Stop()

	w.Enqueue("turn-short", ReasonTurnClosed)
	time.Sleep(200 * time.Millisecond)

	require.Equal(t, 0, fake.Calls)
	_, ok, err := store.GetTurnRecap(ctx, "turn-short")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestWorkerDropsOnFullQueue(t *testing.T) {
	ctx := context.Background()
	store, err := duckdb.Open(ctx, ":memory:")
	require.NoError(t, err)
	defer store.Close()

	// A runner that blocks forever so the queue backs up deterministically.
	block := make(chan struct{})
	fake := &FakeRunner{Responder: func(model, prompt string) (string, error) {
		<-block
		return `{"headline":"x","outcome":"success","phases":[],"key_steps":["a"],"failure_cause":null,"notable_events":[]}`, nil
	}}
	cfg := Default()
	cfg.QueueSize = 1
	cfg.Concurrency = 1
	w := NewWorker(cfg, fake, store, nil, nil)
	w.Start(ctx)
	defer func() {
		close(block)
		w.Stop()
	}()

	// First: seed the "running job" target so it is at least loadable.
	seedTurn(t, store, "turn-1", 5000)

	w.Enqueue("turn-1", ReasonManual) // consumed immediately
	// Push a few more. The one in the channel buffer is fine, the rest
	// must be dropped without blocking.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 16; i++ {
			w.Enqueue("turn-1", ReasonManual)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Enqueue blocked on full queue")
	}
}
