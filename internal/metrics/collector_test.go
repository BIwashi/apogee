package metrics

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/BIwashi/apogee/internal/otel"
	"github.com/BIwashi/apogee/internal/store/duckdb"
)

func TestCollectorTickWritesMetricPoints(t *testing.T) {
	ctx := context.Background()
	db, err := duckdb.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// Seed a running turn so the gauges are non-zero.
	now := time.Now().UTC().Truncate(time.Millisecond)
	require.NoError(t, db.UpsertSession(ctx, duckdb.Session{
		SessionID:  "sess-1",
		SourceApp:  "demo",
		StartedAt:  now,
		LastSeenAt: now,
	}))
	require.NoError(t, db.InsertTurn(ctx, duckdb.Turn{
		TurnID:    "turn-1",
		TraceID:   "trace-1",
		SessionID: "sess-1",
		SourceApp: "demo",
		StartedAt: now,
		Status:    "running",
	}))
	end := now.Add(500 * time.Millisecond)
	require.NoError(t, db.InsertSpan(ctx, &otel.Span{
		TraceID:    otel.TraceID("trace-1"),
		SpanID:     otel.SpanID("span-1"),
		Name:       "claude_code.tool.Bash",
		Kind:       otel.SpanKindInternal,
		StartTime:  now,
		EndTime:    &end,
		StatusCode: otel.StatusError,
		ToolName:   "Bash",
	}))

	fakeNow := now
	c := New(db, 10*time.Second, nil)
	c.Clock = func() time.Time { return fakeNow }

	c.Tick(ctx)
	require.Equal(t, uint64(1), c.Ticks())

	// Active-turns gauge.
	pts, err := db.GetMetricSeries(ctx, duckdb.MetricSeriesOptions{
		Name:   "apogee.turns.active",
		Window: time.Minute,
		Step:   10 * time.Second,
		Kind:   "gauge",
		Now:    fakeNow.Add(5 * time.Second),
	})
	require.NoError(t, err)
	require.NotEmpty(t, pts)
	var sawActive bool
	for _, p := range pts {
		if p.Value >= 1 {
			sawActive = true
			break
		}
	}
	require.True(t, sawActive, "expected active-turn gauge to reflect the running turn")

	// Attention counts gauge.
	pts, err = db.GetMetricSeries(ctx, duckdb.MetricSeriesOptions{
		Name:   "apogee.attention.counts",
		Window: time.Minute,
		Step:   10 * time.Second,
		Kind:   "gauge",
		Now:    fakeNow.Add(5 * time.Second),
	})
	require.NoError(t, err)
	require.NotEmpty(t, pts)
}
