package duckdb

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func mkSignal(metric string, detected time.Time, z float64, severity string) WatchdogSignal {
	return WatchdogSignal{
		DetectedAt:     detected,
		MetricName:     metric,
		LabelsJSON:     `{}`,
		ZScore:         z,
		BaselineMean:   1.0,
		BaselineStddev: 0.5,
		WindowValue:    10.0,
		Severity:       severity,
		Headline:       metric + " spiked",
		EvidenceJSON:   `{"window":[],"baseline":{"mean":1.0,"stddev":0.5}}`,
	}
}

func TestWatchdogInsertAndGet(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	in := mkSignal("apogee.tools.rate", now, 4.2, WatchdogSeverityInfo)
	out, err := s.InsertWatchdogSignal(ctx, in)
	require.NoError(t, err)
	require.Greater(t, out.ID, int64(0))
	require.Equal(t, "apogee.tools.rate", out.MetricName)
	require.Equal(t, 4.2, out.ZScore)
	require.Equal(t, WatchdogSeverityInfo, out.Severity)
	require.False(t, out.Acknowledged)

	fetched, err := s.GetWatchdogSignal(ctx, out.ID)
	require.NoError(t, err)
	require.Equal(t, out.ID, fetched.ID)
	require.Equal(t, out.Headline, fetched.Headline)
}

func TestWatchdogListOrderAndFilter(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	_, err := s.InsertWatchdogSignal(ctx, mkSignal("apogee.errors.rate", now.Add(-2*time.Minute), 3.5, WatchdogSeverityInfo))
	require.NoError(t, err)
	second, err := s.InsertWatchdogSignal(ctx, mkSignal("apogee.tools.rate", now.Add(-time.Minute), 6.2, WatchdogSeverityWarning))
	require.NoError(t, err)
	third, err := s.InsertWatchdogSignal(ctx, mkSignal("apogee.hitl.pending", now, 9.5, WatchdogSeverityCritical))
	require.NoError(t, err)

	all, err := s.ListWatchdogSignals(ctx, WatchdogListFilter{}, 50)
	require.NoError(t, err)
	require.Len(t, all, 3)
	require.Equal(t, third.ID, all[0].ID, "newest first")
	require.Equal(t, second.ID, all[1].ID)

	crit, err := s.ListWatchdogSignals(ctx, WatchdogListFilter{Severity: WatchdogSeverityCritical}, 50)
	require.NoError(t, err)
	require.Len(t, crit, 1)
	require.Equal(t, third.ID, crit[0].ID)
}

func TestWatchdogAckRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	in, err := s.InsertWatchdogSignal(ctx, mkSignal("apogee.turns.active", now, 3.2, WatchdogSeverityInfo))
	require.NoError(t, err)

	unacked, err := s.ListWatchdogSignals(ctx, WatchdogListFilter{OnlyUnacked: true}, 50)
	require.NoError(t, err)
	require.Len(t, unacked, 1)

	acked, err := s.AckWatchdogSignal(ctx, in.ID, now.Add(5*time.Second))
	require.NoError(t, err)
	require.True(t, acked.Acknowledged)
	require.True(t, acked.AcknowledgedAt.Valid)

	unacked2, err := s.ListWatchdogSignals(ctx, WatchdogListFilter{OnlyUnacked: true}, 50)
	require.NoError(t, err)
	require.Len(t, unacked2, 0)

	// Idempotent second ack — ErrWatchdogSignalNotFound is only for missing rows.
	second, err := s.AckWatchdogSignal(ctx, in.ID, now.Add(10*time.Second))
	require.NoError(t, err)
	require.True(t, second.Acknowledged)
	// The original acked_at should stick (we use COALESCE).
	require.Equal(t, acked.AcknowledgedAt.Time, second.AcknowledgedAt.Time)

	_, err = s.AckWatchdogSignal(ctx, 999_999, now)
	require.ErrorIs(t, err, ErrWatchdogSignalNotFound)
}

func TestWatchdogLatestOpenSpell(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	in, err := s.InsertWatchdogSignal(ctx, mkSignal("apogee.errors.rate", now, 4.0, WatchdogSeverityInfo))
	require.NoError(t, err)

	got, ok, err := s.LatestOpenWatchdogSpell(ctx, "apogee.errors.rate", `{}`)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, in.ID, got.ID)

	// Close the spell and the query should come back empty.
	require.NoError(t, s.CloseWatchdogSpell(ctx, in.ID, now.Add(5*time.Minute)))
	_, ok, err = s.LatestOpenWatchdogSpell(ctx, "apogee.errors.rate", `{}`)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestWatchdogReadMetricWindow(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	for i := 0; i < 5; i++ {
		require.NoError(t, s.InsertMetricPoint(ctx, MetricPoint{
			Timestamp: now.Add(time.Duration(i) * time.Second),
			Name:      "apogee.tools.rate",
			Kind:      "counter",
			Value:     float64(i),
			Labels:    map[string]string{},
		}))
	}

	pts, err := s.ReadMetricWindow(ctx, "apogee.tools.rate", `{}`, now, now.Add(10*time.Second))
	require.NoError(t, err)
	require.Len(t, pts, 5)
	require.Equal(t, 0.0, pts[0].Value)
	require.Equal(t, 4.0, pts[4].Value)
}
