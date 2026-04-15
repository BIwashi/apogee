package duckdb

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/BIwashi/apogee/internal/otel"
)

func TestInsertSpanDefaults(t *testing.T) {
	ctx := t.Context()
	s := newTestStore(t)

	now := time.Now().UTC().Truncate(time.Millisecond)
	require.NoError(t, s.UpsertSession(ctx, Session{SessionID: "s1", SourceApp: "demo", StartedAt: now, LastSeenAt: now}))
	require.NoError(t, s.InsertTurn(ctx, Turn{TurnID: "t1", TraceID: "deadbeefdeadbeefdeadbeefdeadbeef", SessionID: "s1", SourceApp: "demo", StartedAt: now, Status: "running"}))

	sp := &otel.Span{
		TraceID:    otel.TraceID("deadbeefdeadbeefdeadbeefdeadbeef"),
		SpanID:     otel.SpanID("1234567890abcdef"),
		Name:       "claude_code.turn",
		Kind:       otel.SpanKindInternal,
		StartTime:  now,
		StatusCode: otel.StatusUnset,
		SessionID:  "s1",
		TurnID:     "t1",
	}
	require.NoError(t, s.InsertSpan(ctx, sp))

	rows, err := s.GetSpansByTrace(ctx, "deadbeefdeadbeefdeadbeefdeadbeef")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "claude-code", rows[0].ServiceName)
	require.Equal(t, "{}", "{}") // defaults baked in
}

func TestGetSpansByTurnOrder(t *testing.T) {
	ctx := t.Context()
	s := newTestStore(t)

	now := time.Now().UTC().Truncate(time.Millisecond)
	require.NoError(t, s.UpsertSession(ctx, Session{SessionID: "s1", SourceApp: "demo", StartedAt: now, LastSeenAt: now}))
	require.NoError(t, s.InsertTurn(ctx, Turn{TurnID: "t1", TraceID: "00000000000000000000000000000001", SessionID: "s1", SourceApp: "demo", StartedAt: now, Status: "running"}))

	tr := otel.TraceID("00000000000000000000000000000001")
	// Insert spans out of chronological order to verify GetSpansByTurn
	// returns them sorted ascending by start_time (required for swim-lane
	// and waterfall rendering).
	for i, offset := range []int{20, 5, 10, 0, 15} {
		spanID := otel.SpanID("aaaaaaaaaaaaaaaa")
		// fake unique id by mutating a byte
		idBytes := []byte(spanID)
		idBytes[0] = byte('a' + i)
		sp := &otel.Span{
			TraceID:    tr,
			SpanID:     otel.SpanID(string(idBytes)),
			Name:       "claude_code.tool.Test",
			Kind:       otel.SpanKindInternal,
			StartTime:  now.Add(time.Duration(offset) * time.Second),
			StatusCode: otel.StatusOK,
			SessionID:  "s1",
			TurnID:     "t1",
			ToolName:   "Bash",
		}
		require.NoError(t, s.InsertSpan(ctx, sp))
	}

	got, err := s.GetSpansByTurn(ctx, "t1")
	require.NoError(t, err)
	require.Len(t, got, 5)
	for i := 1; i < len(got); i++ {
		require.False(t, got[i].StartTime.Before(got[i-1].StartTime),
			"spans must be ordered ascending by start_time")
	}
}

func TestListLogsByTurnAndSession(t *testing.T) {
	ctx := t.Context()
	s := newTestStore(t)

	now := time.Now().UTC().Truncate(time.Millisecond)
	require.NoError(t, s.UpsertSession(ctx, Session{SessionID: "s1", SourceApp: "demo", StartedAt: now, LastSeenAt: now}))
	require.NoError(t, s.InsertTurn(ctx, Turn{TurnID: "t1", TraceID: "00000000000000000000000000000002", SessionID: "s1", SourceApp: "demo", StartedAt: now, Status: "running"}))

	for i := 0; i < 3; i++ {
		require.NoError(t, s.InsertLog(ctx, &otel.LogRecord{
			Timestamp:    now.Add(time.Duration(i) * time.Second),
			SessionID:    "s1",
			TurnID:       "t1",
			HookEvent:    "PreToolUse",
			SourceApp:    "demo",
			SeverityText: "INFO",
			Body:         "hi",
		}))
	}

	turnLogs, err := s.ListLogsByTurn(ctx, "t1", 100)
	require.NoError(t, err)
	require.Len(t, turnLogs, 3)
	// Ascending order.
	require.True(t, !turnLogs[1].Timestamp.Before(turnLogs[0].Timestamp))

	sessLogs, err := s.ListLogsBySession(ctx, "s1", 100)
	require.NoError(t, err)
	require.Len(t, sessLogs, 3)
	// Descending order.
	require.True(t, !sessLogs[1].Timestamp.After(sessLogs[0].Timestamp))
}
