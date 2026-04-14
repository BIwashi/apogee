// Package metrics runs the apogee collector's internal sampling job. Every
// tick it computes a handful of fleet-wide gauges and counters and inserts
// them into the metric_points table so the dashboard can render sparklines
// without re-deriving state from raw spans.
package metrics

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/BIwashi/apogee/internal/store/duckdb"
)

// Default tick interval. The collector samples once at start-up then at
// Interval cadence until its context is cancelled.
const DefaultInterval = 5 * time.Second

// Collector samples fleet-wide metrics at a fixed cadence. The zero value is
// not usable; construct with New.
type Collector struct {
	DB       *duckdb.Store
	Interval time.Duration
	Clock    func() time.Time
	Logger   *slog.Logger

	// Running baselines for counter metrics, mirrored at tick boundaries.
	lastToolCount  int64
	lastErrorCount int64
	lastSampleAt   time.Time

	ticks atomic.Uint64
}

// New returns a Collector wired to the given store. Optional interval and
// logger may be zero / nil to use defaults.
func New(db *duckdb.Store, interval time.Duration, logger *slog.Logger) *Collector {
	if interval <= 0 {
		interval = DefaultInterval
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Collector{
		DB:       db,
		Interval: interval,
		Clock:    time.Now,
		Logger:   logger,
	}
}

// Run samples metrics on a ticker until ctx is cancelled. It does a tick on
// start so the first dashboard render has at least one data point.
func (c *Collector) Run(ctx context.Context) error {
	if c.DB == nil {
		return nil
	}
	c.Tick(ctx) // initial sample
	ticker := time.NewTicker(c.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			c.Tick(ctx)
		}
	}
}

// Tick performs a single sampling pass. Exposed for tests so they can pin
// the ticker's schedule.
func (c *Collector) Tick(ctx context.Context) {
	c.ticks.Add(1)
	now := c.Clock()

	// ── gauges ───────────────────────────────────────────
	active, err := c.DB.CountRunningTurns(ctx)
	if err == nil {
		_ = c.insert(ctx, "apogee.turns.active", "gauge", float64(active), nil, now)
	} else {
		c.Logger.Debug("metrics: count running turns", "err", err)
	}

	hitl, err := c.DB.CountPendingHITL(ctx)
	if err == nil {
		_ = c.insert(ctx, "apogee.hitl.pending", "gauge", float64(hitl), nil, now)
	} else {
		c.Logger.Debug("metrics: count hitl", "err", err)
	}

	// Attention counts, one point per state.
	counts, err := c.DB.CountAttention(ctx, false)
	if err == nil {
		byState := map[string]int{
			"intervene_now": counts.InterveneNow,
			"watch":         counts.Watch,
			"watchlist":     counts.Watchlist,
			"healthy":       counts.Healthy,
		}
		for state, v := range byState {
			_ = c.insert(ctx, "apogee.attention.counts", "gauge",
				float64(v),
				map[string]string{"state": state},
				now,
			)
		}
	} else {
		c.Logger.Debug("metrics: attention counts", "err", err)
	}

	// ── counters ─────────────────────────────────────────
	// Tool / error counters fire as deltas since the previous tick so the
	// series is a proper rate (per-interval count).
	var (
		toolDelta  int64
		errorDelta int64
	)
	toolTotal, err := c.DB.CountToolSpans(ctx)
	if err == nil {
		if c.lastSampleAt.IsZero() {
			toolDelta = 0
		} else {
			toolDelta = toolTotal - c.lastToolCount
			if toolDelta < 0 {
				toolDelta = 0
			}
		}
		c.lastToolCount = toolTotal
		_ = c.insert(ctx, "apogee.tools.rate", "counter", float64(toolDelta), nil, now)
	}
	errorTotal, err := c.DB.CountErrorSpans(ctx)
	if err == nil {
		if c.lastSampleAt.IsZero() {
			errorDelta = 0
		} else {
			errorDelta = errorTotal - c.lastErrorCount
			if errorDelta < 0 {
				errorDelta = 0
			}
		}
		c.lastErrorCount = errorTotal
		_ = c.insert(ctx, "apogee.errors.rate", "counter", float64(errorDelta), nil, now)
	}

	c.lastSampleAt = now
}

// Ticks returns the number of Tick calls the collector has made. Exposed for
// tests.
func (c *Collector) Ticks() uint64 { return c.ticks.Load() }

func (c *Collector) insert(ctx context.Context, name, kind string, value float64, labels map[string]string, now time.Time) error {
	return c.DB.InsertMetricPoint(ctx, duckdb.MetricPoint{
		Timestamp: now,
		Name:      name,
		Kind:      kind,
		Value:     value,
		Labels:    labels,
	})
}
