package duckdb

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/BIwashi/apogee/internal/otel"
)

func TestInsertSpanDefaults(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	now := time.Now().UTC().Truncate(time.Millisecond)
	require.NoError(t, s.UpsertSession(ctx, Session{SessionID: "s1", SourceApp: "demo", StartedAt: now, LastSeenAt: now}))
	require.NoError(t, s.InsertTurn(ctx, Turn{TurnID: "t1", TraceID: "deadbeefdeadbeefdeadbeefdeadbeef", SessionID: "s1", SourceApp: "demo", StartedAt: now, Status: "running"}))

	sp := &otel.Span{
		TraceID:   otel.TraceID("deadbeefdeadbeefdeadbeefdeadbeef"),
		SpanID:    otel.SpanID("1234567890abcdef"),
		Name:      "claude_code.turn",
		Kind:      otel.SpanKindInternal,
		StartTime: now,
		StatusCode: otel.StatusUnset,
		SessionID: "s1",
		TurnID:    "t1",
	}
	require.NoError(t, s.InsertSpan(ctx, sp))

	rows, err := s.GetSpansByTrace(ctx, "deadbeefdeadbeefdeadbeefdeadbeef")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "claude-code", rows[0].ServiceName)
	require.Equal(t, "{}", "{}") // defaults baked in
}
