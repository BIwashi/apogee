package duckdb

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/BIwashi/apogee/internal/otel"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	ctx := t.Context()
	s, err := Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestOpenInMemory(t *testing.T) {
	ctx := t.Context()
	s, err := Open(ctx, ":memory:")
	require.NoError(t, err)
	require.NoError(t, s.Close())
}

func TestOpenFile(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	dsn := filepath.Join(dir, "test.duckdb")
	s, err := Open(ctx, dsn)
	require.NoError(t, err)
	require.NoError(t, s.Close())

	// Reopen and verify schema is idempotent.
	s2, err := Open(ctx, dsn)
	require.NoError(t, err)
	require.NoError(t, s2.Close())
}

func TestSessionTurnSpanRoundTrip(t *testing.T) {
	ctx := t.Context()
	s := newTestStore(t)

	now := time.Now().UTC().Truncate(time.Millisecond)
	sess := Session{
		SessionID:  "sess-1",
		SourceApp:  "demo",
		StartedAt:  now,
		LastSeenAt: now,
		Model:      "claude-sonnet-4",
	}
	require.NoError(t, s.UpsertSession(ctx, sess))

	got, err := s.GetSession(ctx, "sess-1")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "demo", got.SourceApp)
	require.Equal(t, "claude-sonnet-4", got.Model)

	turn := Turn{
		TurnID:    "turn-1",
		TraceID:   "00112233445566778899aabbccddeeff",
		SessionID: "sess-1",
		SourceApp: "demo",
		StartedAt: now,
		Status:    "running",
	}
	require.NoError(t, s.InsertTurn(ctx, turn))

	tr := otel.TraceID("00112233445566778899aabbccddeeff")
	root := &otel.Span{
		TraceID:    tr,
		SpanID:     otel.SpanID("aaaaaaaaaaaaaaaa"),
		Name:       "claude_code.turn",
		Kind:       otel.SpanKindInternal,
		StartTime:  now,
		StatusCode: otel.StatusUnset,
		SessionID:  "sess-1",
		TurnID:     "turn-1",
		AgentID:    "main",
		AgentKind:  "main",
		HookEvent:  "UserPromptSubmit",
		Attributes: map[string]any{"k": "v"},
	}
	require.NoError(t, s.InsertSpan(ctx, root))

	end := now.Add(2 * time.Second)
	tool := &otel.Span{
		TraceID:      tr,
		SpanID:       otel.SpanID("bbbbbbbbbbbbbbbb"),
		ParentSpanID: root.SpanID,
		Name:         "claude_code.tool.Bash",
		Kind:         otel.SpanKindInternal,
		StartTime:    now,
		EndTime:      &end,
		StatusCode:   otel.StatusOK,
		SessionID:    "sess-1",
		TurnID:       "turn-1",
		ToolName:     "Bash",
		ToolUseID:    "tu-1",
	}
	require.NoError(t, s.InsertSpan(ctx, tool))

	spans, err := s.GetSpansByTrace(ctx, string(tr))
	require.NoError(t, err)
	require.Len(t, spans, 2)

	turnSpans, err := s.GetSpansByTurn(ctx, "turn-1")
	require.NoError(t, err)
	require.Len(t, turnSpans, 2)

	// Update the root: close it.
	rootEnd := end.Add(time.Second)
	root.EndTime = &rootEnd
	root.StatusCode = otel.StatusOK
	require.NoError(t, s.UpdateSpan(ctx, root))

	// Update the turn row.
	dur := rootEnd.Sub(now).Milliseconds()
	require.NoError(t, s.UpdateTurnStatus(ctx, "turn-1", "completed", &rootEnd, &dur, 1, 0, 0))

	gotTurn, err := s.GetTurn(ctx, "turn-1")
	require.NoError(t, err)
	require.Equal(t, "completed", gotTurn.Status)
	require.NotNil(t, gotTurn.DurationMs)
	require.Equal(t, dur, *gotTurn.DurationMs)
	require.Equal(t, 1, gotTurn.ToolCallCount)

	// Logs.
	require.NoError(t, s.InsertLog(ctx, &otel.LogRecord{
		Timestamp:    now,
		TraceID:      tr,
		SeverityText: "INFO",
		Body:         "hi",
		SessionID:    "sess-1",
		TurnID:       "turn-1",
		HookEvent:    "PreToolUse",
		SourceApp:    "demo",
		Attributes:   map[string]any{"tool": "Bash"},
	}))

	// Filter options.
	opts, err := s.GetFilterOptions(ctx)
	require.NoError(t, err)
	require.Contains(t, opts.SourceApps, "demo")
	require.Contains(t, opts.SessionIDs, "sess-1")
	require.Contains(t, opts.HookEvents, "PreToolUse")
	require.Contains(t, opts.ToolNames, "Bash")

	// Recent listings.
	listSess, err := s.ListRecentSessions(ctx, 0)
	require.NoError(t, err)
	require.Len(t, listSess, 1)

	listTurns, err := s.ListRecentTurns(ctx, 0)
	require.NoError(t, err)
	require.Len(t, listTurns, 1)

	listSessTurns, err := s.ListSessionTurns(ctx, "sess-1", 0)
	require.NoError(t, err)
	require.Len(t, listSessTurns, 1)

	listSpans, err := s.ListRecentSpans(ctx, 0)
	require.NoError(t, err)
	require.Len(t, listSpans, 2)
}

func TestUpsertSessionIdempotent(t *testing.T) {
	ctx := t.Context()
	s := newTestStore(t)

	now := time.Now().UTC().Truncate(time.Millisecond)
	require.NoError(t, s.UpsertSession(ctx, Session{
		SessionID:  "sess-1",
		SourceApp:  "demo",
		StartedAt:  now,
		LastSeenAt: now,
	}))
	require.NoError(t, s.UpsertSession(ctx, Session{
		SessionID:  "sess-1",
		SourceApp:  "demo",
		StartedAt:  now,
		LastSeenAt: now.Add(time.Minute),
		Model:      "claude-sonnet",
	}))
	got, err := s.GetSession(ctx, "sess-1")
	require.NoError(t, err)
	require.Equal(t, "claude-sonnet", got.Model)
	require.True(t, got.LastSeenAt.After(now) || got.LastSeenAt.Equal(now.Add(time.Minute)))
}
