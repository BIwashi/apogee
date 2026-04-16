package summarizer

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/BIwashi/apogee/internal/otel"
	"github.com/BIwashi/apogee/internal/sse"
	"github.com/BIwashi/apogee/internal/store/duckdb"
)

// seedLiveSession writes a session + running turn + one tool span so the
// live-status worker has something to describe. Optional overrides let
// individual tests reshape the fixture (e.g. closed turn, idle session).
func seedLiveSession(t *testing.T, store *duckdb.Store, sessionID, turnID string, liveState string) {
	t.Helper()
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Millisecond)

	require.NoError(t, store.UpsertSession(ctx, duckdb.Session{
		SessionID:  sessionID,
		SourceApp:  "apogee-test",
		StartedAt:  now.Add(-time.Minute),
		LastSeenAt: now,
	}))
	require.NoError(t, store.InsertTurn(ctx, duckdb.Turn{
		TurnID:     turnID,
		TraceID:    "trace-" + turnID,
		SessionID:  sessionID,
		SourceApp:  "apogee-test",
		StartedAt:  now.Add(-30 * time.Second),
		Status:     "running",
		PromptText: "refactor the widget",
	}))
	require.NoError(t, store.InsertSpan(ctx, &otel.Span{
		TraceID:     otel.TraceID("trace-" + turnID),
		SpanID:      otel.SpanID("span-1"),
		Name:        "claude_code.tool.Edit",
		Kind:        otel.SpanKindInternal,
		StartTime:   now.Add(-10 * time.Second),
		StatusCode:  otel.StatusUnset,
		ServiceName: "claude-code",
		SessionID:   sessionID,
		TurnID:      turnID,
		ToolName:    "Edit",
		HookEvent:   "PreToolUse",
	}))
	// Bubble the attention projection onto the session row so the worker
	// sees the expected live_state without needing the real reconstructor
	// wired up.
	require.NoError(t, store.UpdateSessionAttention(ctx, sessionID, "healthy", "", 0.1, turnID, "implement", liveState))
}

func TestLiveStatusWorkerHappyPath(t *testing.T) {
	ctx := t.Context()
	store, err := duckdb.Open(ctx, ":memory:")
	require.NoError(t, err)
	defer store.Close()

	seedLiveSession(t, store, "sess-1", "turn-1", "live")

	fake := &FakeRunner{Responder: func(model, prompt string) (string, error) {
		return `"Editing the widget component."`, nil
	}}
	hub := sse.NewHub(nil)
	cfg := Default()
	cfg.QueueSize = 8
	cfg.LiveStatusDebounce = 5 * time.Millisecond

	w := NewLiveStatusWorker(cfg, fake, store, hub, nil)
	w.Start(ctx)
	defer w.Stop()

	sub := hub.Subscribe(sse.Filter{})
	defer hub.Unsubscribe(sub)

	w.Enqueue("sess-1", LiveStatusReasonSpanInserted)

	deadline := time.Now().Add(2 * time.Second)
	var got *duckdb.Session
	for time.Now().Before(deadline) {
		sess, err := store.GetSession(ctx, "sess-1")
		require.NoError(t, err)
		if sess != nil && sess.LiveStatusText != "" {
			got = sess
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NotNil(t, got, "live status never landed")
	require.Equal(t, "Editing the widget component.", got.LiveStatusText, "quotes should be stripped")
	require.Equal(t, "claude-haiku-4-5", got.LiveStatusModel)
	require.NotNil(t, got.LiveStatusAt)

	// SSE session.updated should have fanned out before we look at the
	// fake runner — the channel receive is the happens-before edge that
	// synchronises the worker's writes with this goroutine.
	select {
	case ev := <-sub.C():
		require.Equal(t, sse.EventTypeSessionUpdated, ev.Type)
	case <-time.After(time.Second):
		t.Fatal("no SSE broadcast for live status")
	}

	require.Equal(t, 1, fake.Calls)
	require.Contains(t, fake.LastPrompt, "- Edit [")
	require.Contains(t, fake.LastPrompt, "refactor the widget")
	require.Contains(t, fake.LastPrompt, "source_app: apogee-test")
}

func TestLiveStatusWorkerSkipsIdleSession(t *testing.T) {
	ctx := t.Context()
	store, err := duckdb.Open(ctx, ":memory:")
	require.NoError(t, err)
	defer store.Close()

	seedLiveSession(t, store, "sess-1", "turn-1", "idle")

	fake := &FakeRunner{Responder: func(model, prompt string) (string, error) {
		t.Fatal("runner should not be invoked for idle session")
		return "", nil
	}}
	cfg := Default()
	cfg.QueueSize = 8

	w := NewLiveStatusWorker(cfg, fake, store, sse.NewHub(nil), nil)
	w.Start(ctx)
	defer w.Stop()

	w.Enqueue("sess-1", LiveStatusReasonSpanInserted)
	// Drain: after Stop the worker finished processing the job so any
	// call-through would have fired the runner (which t.Fatal's).
	time.Sleep(150 * time.Millisecond)
	require.Equal(t, 0, fake.Calls)
}

func TestLiveStatusWorkerSkipsWhenTurnNotRunning(t *testing.T) {
	ctx := t.Context()
	store, err := duckdb.Open(ctx, ":memory:")
	require.NoError(t, err)
	defer store.Close()

	seedLiveSession(t, store, "sess-1", "turn-1", "live")
	// Flip the representative turn to completed so the worker's
	// race-guard triggers.
	endedAt := time.Now()
	var duration int64 = 5000
	require.NoError(t, store.UpdateTurnStatus(ctx, "turn-1", "completed", &endedAt, &duration, 1, 0, 0))

	fake := &FakeRunner{Responder: func(model, prompt string) (string, error) {
		t.Fatal("runner should not be invoked when the representative turn is closed")
		return "", nil
	}}
	cfg := Default()
	cfg.QueueSize = 8

	w := NewLiveStatusWorker(cfg, fake, store, sse.NewHub(nil), nil)
	w.Start(ctx)
	defer w.Stop()

	w.Enqueue("sess-1", LiveStatusReasonSpanInserted)
	time.Sleep(150 * time.Millisecond)
	require.Equal(t, 0, fake.Calls)
}

func TestLiveStatusWorkerDebounce(t *testing.T) {
	ctx := t.Context()
	store, err := duckdb.Open(ctx, ":memory:")
	require.NoError(t, err)
	defer store.Close()

	seedLiveSession(t, store, "sess-1", "turn-1", "live")

	// Use an atomic counter inside the responder so the test goroutine
	// and the worker goroutine share state without a data race. The
	// responder emits a different string per call so we can observe
	// which pass wrote to the DB.
	var calls atomic.Int32
	fake := &FakeRunner{Responder: func(model, prompt string) (string, error) {
		n := calls.Add(1)
		return "call " + string(rune('0'+n)), nil
	}}
	hub := sse.NewHub(nil)
	sub := hub.Subscribe(sse.Filter{})
	defer hub.Unsubscribe(sub)

	cfg := Default()
	cfg.QueueSize = 8
	cfg.LiveStatusDebounce = time.Hour // effectively block all re-runs

	w := NewLiveStatusWorker(cfg, fake, store, hub, nil)
	w.Start(ctx)
	defer w.Stop()

	// First job lands — wait for the SSE broadcast to give us a
	// happens-before edge before we touch the atomic.
	w.Enqueue("sess-1", LiveStatusReasonSpanInserted)
	select {
	case <-sub.C():
	case <-time.After(2 * time.Second):
		t.Fatal("first live-status broadcast never arrived")
	}
	require.EqualValues(t, 1, calls.Load())

	// Second job within the debounce window is skipped. No SSE, no
	// runner call. We give the worker a conservative 200ms to reach the
	// debounce guard and return.
	w.Enqueue("sess-1", LiveStatusReasonSpanInserted)
	select {
	case ev := <-sub.C():
		t.Fatalf("unexpected broadcast inside debounce window: %v", ev.Type)
	case <-time.After(200 * time.Millisecond):
	}
	require.EqualValues(t, 1, calls.Load(), "second enqueue inside debounce window should not run")

	// Manual reason bypasses the debounce.
	w.Enqueue("sess-1", LiveStatusReasonManual)
	select {
	case <-sub.C():
	case <-time.After(2 * time.Second):
		t.Fatal("manual live-status broadcast never arrived")
	}
	require.EqualValues(t, 2, calls.Load(), "manual reason must bypass debounce")
}

func TestCleanLiveStatusOutput(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`  Editing main.go.  `, "Editing main.go."},
		{`"Running tests."`, "Running tests."},
		{"`Searching`", "Searching"},
		{"Line one\nline two", "Line one line two"},
		{strings.Repeat("a", 200), strings.Repeat("a", liveStatusMaxOutputChars) + "…"},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, cleanLiveStatusOutput(tc.in), "input=%q", tc.in)
	}
}

func TestBuildLiveStatusPromptShape(t *testing.T) {
	now := time.Now()
	sess := duckdb.Session{
		SessionID:    "sess-1",
		SourceApp:    "apogee",
		CurrentPhase: "debug",
	}
	turn := duckdb.Turn{
		TurnID:        "turn-1",
		PromptText:    "fix the flake",
		Headline:      "debug flaky test",
		ToolCallCount: 3,
		ErrorCount:    1,
		StartedAt:     now,
	}
	spans := []duckdb.SpanRow{
		{Name: "claude_code.tool.Bash", ToolName: "Bash", StartTime: now, StatusCode: "OK"},
		{Name: "claude_code.tool.Edit", ToolName: "Edit", StartTime: now.Add(time.Second), StatusCode: "UNSET"},
	}
	prompt := buildLiveStatusPrompt(sess, turn, spans)
	require.Contains(t, prompt, "source_app: apogee")
	require.Contains(t, prompt, "phase: debug")
	require.Contains(t, prompt, "user_prompt: fix the flake")
	require.Contains(t, prompt, "headline: debug flaky test")
	require.Contains(t, prompt, "tool_call_count: 3")
	require.Contains(t, prompt, "error_count: 1")
	require.Contains(t, prompt, "- Bash [ok]")
	require.Contains(t, prompt, "- Edit [running]")
}
