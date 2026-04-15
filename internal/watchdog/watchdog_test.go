package watchdog

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/BIwashi/apogee/internal/sse"
	"github.com/BIwashi/apogee/internal/store/duckdb"
)

// newTestWorker spins up an in-memory store and a Worker with a single
// monitored metric (`test.metric` with empty labels). The Worker's
// clock is pinned to `now` so baseline/window bounds are deterministic.
func newTestWorker(t *testing.T, now time.Time) (*Worker, *duckdb.Store, *sse.Hub) {
	t.Helper()
	ctx := t.Context()
	store, err := duckdb.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	hub := sse.NewHub(nil)
	w := NewWorker(store, hub, nil)
	w.Tick = 10 * time.Millisecond
	w.Window = 60 * time.Second
	w.Baseline = 24 * time.Hour
	w.NormalForWait = 3 * time.Minute
	w.Clock = func() time.Time { return now }
	w.Metrics = []MonitoredMetric{{
		Name:       "test.metric",
		LabelsJSON: `{}`,
		Headline:   "test.metric spiked to %.1f (baseline %.2f ± %.2f)",
	}}
	return w, store, hub
}

// seedBaseline inserts n baseline samples into metric_points spread
// across the hour preceding `now`. Each sample has the given value.
func seedBaseline(t *testing.T, store *duckdb.Store, name string, now time.Time, value float64, n int) {
	t.Helper()
	ctx := t.Context()
	for i := 0; i < n; i++ {
		at := now.Add(-time.Duration(n-i) * time.Minute).Add(-5 * time.Minute)
		require.NoError(t, store.InsertMetricPoint(ctx, duckdb.MetricPoint{
			Timestamp: at,
			Name:      name,
			Kind:      "counter",
			Value:     value,
			Labels:    map[string]string{},
		}))
	}
}

// seedWindow inserts samples spread over the last `window` before `now`.
func seedWindow(t *testing.T, store *duckdb.Store, name string, now time.Time, values []float64) {
	t.Helper()
	ctx := t.Context()
	step := time.Duration(0)
	if len(values) > 0 {
		step = 60 * time.Second / time.Duration(len(values))
	}
	for i, v := range values {
		at := now.Add(-60 * time.Second).Add(time.Duration(i) * step).Add(time.Second)
		require.NoError(t, store.InsertMetricPoint(ctx, duckdb.MetricPoint{
			Timestamp: at,
			Name:      name,
			Kind:      "counter",
			Value:     v,
			Labels:    map[string]string{},
		}))
	}
}

func TestDetectOnceEmitsOnSpike(t *testing.T) {
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Millisecond)
	w, store, hub := newTestWorker(t, now)

	// Baseline: 30 samples of value 1 (±0 jitter is fine; add a touch so
	// stddev is non-zero).
	for i := 0; i < 30; i++ {
		at := now.Add(-time.Duration(30-i) * 5 * time.Minute)
		v := 1.0
		if i%2 == 0 {
			v = 1.1
		}
		require.NoError(t, store.InsertMetricPoint(ctx, duckdb.MetricPoint{
			Timestamp: at,
			Name:      "test.metric",
			Kind:      "counter",
			Value:     v,
			Labels:    map[string]string{},
		}))
	}

	// Window: huge spike — far beyond 3σ.
	seedWindow(t, store, "test.metric", now, []float64{1.0, 1.0, 50.0, 1.0, 1.0})

	sub := hub.Subscribe(sse.Filter{})
	defer hub.Unsubscribe(sub)

	emitted, err := w.DetectOnce(ctx)
	require.NoError(t, err)
	require.Len(t, emitted, 1)
	sig := emitted[0]
	require.Equal(t, duckdb.WatchdogSeverityCritical, sig.Severity)
	require.InDelta(t, 50.0, sig.WindowValue, 1e-9)
	require.Contains(t, sig.Headline, "spiked")

	// Broadcast landed.
	select {
	case ev := <-sub.C():
		require.Equal(t, sse.EventWatchdogSignal, ev.Type)
	case <-time.After(time.Second):
		t.Fatal("expected watchdog.signal broadcast")
	}
}

func TestDetectOnceSkipsWithInsufficientBaseline(t *testing.T) {
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Millisecond)
	w, store, _ := newTestWorker(t, now)
	// Only two baseline points → degenerate baseline → no signal.
	seedBaseline(t, store, "test.metric", now, 1.0, 2)
	seedWindow(t, store, "test.metric", now, []float64{1.0, 1.0, 50.0})
	emitted, err := w.DetectOnce(ctx)
	require.NoError(t, err)
	require.Empty(t, emitted)
}

func TestDetectOnceSkipsBelowThreshold(t *testing.T) {
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Millisecond)
	w, store, _ := newTestWorker(t, now)

	// Baseline noisy around 10 with stddev ≈ 2.
	vals := []float64{8, 12, 10, 11, 9, 10, 12, 8, 11, 9, 10, 12, 8, 11, 9}
	for i, v := range vals {
		at := now.Add(-time.Duration(len(vals)-i) * 5 * time.Minute)
		require.NoError(t, store.InsertMetricPoint(ctx, duckdb.MetricPoint{
			Timestamp: at,
			Name:      "test.metric",
			Kind:      "counter",
			Value:     v,
			Labels:    map[string]string{},
		}))
	}
	// Window values near the mean → no signal.
	seedWindow(t, store, "test.metric", now, []float64{10, 11, 9, 10})

	emitted, err := w.DetectOnce(ctx)
	require.NoError(t, err)
	require.Empty(t, emitted)
}

func TestDetectOnceDedupesSpellsWhileAnomalous(t *testing.T) {
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Millisecond)
	w, store, _ := newTestWorker(t, now)

	// Stable baseline.
	for i := 0; i < 40; i++ {
		at := now.Add(-time.Duration(40-i) * 5 * time.Minute)
		v := 1.0
		if i%3 == 0 {
			v = 1.2
		}
		require.NoError(t, store.InsertMetricPoint(ctx, duckdb.MetricPoint{
			Timestamp: at,
			Name:      "test.metric",
			Kind:      "counter",
			Value:     v,
			Labels:    map[string]string{},
		}))
	}
	// Spike in the window.
	seedWindow(t, store, "test.metric", now, []float64{1.0, 1.0, 20.0})

	// First tick emits.
	first, err := w.DetectOnce(ctx)
	require.NoError(t, err)
	require.Len(t, first, 1)

	// Second tick: still anomalous (same data), must not re-emit.
	second, err := w.DetectOnce(ctx)
	require.NoError(t, err)
	require.Empty(t, second, "dedup while spell is open")

	// Exactly one stored row.
	list, err := store.ListWatchdogSignals(ctx, duckdb.WatchdogListFilter{}, 10)
	require.NoError(t, err)
	require.Len(t, list, 1)
}

func TestDetectOnceClosesSpellAfterNormalWait(t *testing.T) {
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Millisecond)

	// Seed store with a baseline + anomalous window at t0.
	w, store, _ := newTestWorker(t, now)
	w.NormalForWait = time.Second // tight dwell for the test

	for i := 0; i < 40; i++ {
		at := now.Add(-time.Duration(40-i) * 5 * time.Minute)
		v := 1.0
		if i%3 == 0 {
			v = 1.2
		}
		require.NoError(t, store.InsertMetricPoint(ctx, duckdb.MetricPoint{
			Timestamp: at,
			Name:      "test.metric",
			Kind:      "counter",
			Value:     v,
			Labels:    map[string]string{},
		}))
	}
	seedWindow(t, store, "test.metric", now, []float64{1.0, 1.0, 25.0})

	// First tick opens a spell.
	first, err := w.DetectOnce(ctx)
	require.NoError(t, err)
	require.Len(t, first, 1)

	// Advance clock by 2 seconds so the window no longer includes the
	// spike (seedWindow placed it within the 60s window at the *old*
	// now; a later clock shifts the window forward enough that the
	// spike is still visible since seedWindow samples lie at old-now -
	// 60s..old-now, which for a +2s shift is now-62s..now-2s. To
	// simulate the return to normal, insert three new within-window
	// samples at quiet values and advance the clock so the spike drops
	// out.
	later := now.Add(3 * time.Minute)
	quietValues := []float64{1.0, 1.1, 0.9, 1.0, 1.1, 1.0}
	for i, v := range quietValues {
		at := later.Add(-60 * time.Second).Add(time.Duration(i*10) * time.Second)
		require.NoError(t, store.InsertMetricPoint(ctx, duckdb.MetricPoint{
			Timestamp: at,
			Name:      "test.metric",
			Kind:      "counter",
			Value:     v,
			Labels:    map[string]string{},
		}))
	}
	w.Clock = func() time.Time { return later }

	// First quiet tick: records the start-of-normal timestamp but does
	// not close the spell yet (dwell not exceeded on same call).
	_, err = w.DetectOnce(ctx)
	require.NoError(t, err)

	// Advance a little further and the spell should close.
	evenLater := later.Add(5 * time.Second)
	w.Clock = func() time.Time { return evenLater }
	_, err = w.DetectOnce(ctx)
	require.NoError(t, err)

	// No open spell left.
	_, ok, err := store.LatestOpenWatchdogSpell(ctx, "test.metric", `{}`)
	require.NoError(t, err)
	require.False(t, ok, "spell should be closed")
}
