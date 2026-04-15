package watchdog

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/BIwashi/apogee/internal/sse"
	"github.com/BIwashi/apogee/internal/store/duckdb"
)

// Default worker pacing and window constants. Every field is overridable
// on the Worker struct so tests can pin the schedule.
const (
	DefaultTick          = 60 * time.Second
	DefaultWindow        = 60 * time.Second
	DefaultBaseline      = 24 * time.Hour
	DefaultNormalForWait = 3 * time.Minute
)

// MonitoredMetric is one row in the watchdog's subscription list. Each
// (Name, LabelsJSON) tuple is evaluated once per tick. Headline is a
// fmt.Sprintf template applied to (value, mean, stddev).
type MonitoredMetric struct {
	Name       string
	LabelsJSON string
	Headline   string
}

// DefaultMonitoredMetrics is the baseline subscription list used by the
// production worker. All four metrics are written by the fleet-wide
// tick in internal/metrics/collector.go.
func DefaultMonitoredMetrics() []MonitoredMetric {
	return []MonitoredMetric{
		{
			Name:       "apogee.turns.active",
			LabelsJSON: `{}`,
			Headline:   "Active turns spiked to %.0f (baseline %.1f ± %.1f)",
		},
		{
			Name:       "apogee.errors.rate",
			LabelsJSON: `{}`,
			Headline:   "Error rate surge — %.1f/s vs baseline %.2f/s (±%.2f)",
		},
		{
			Name:       "apogee.tools.rate",
			LabelsJSON: `{}`,
			Headline:   "Unusual tool activity — %.1f/s vs baseline %.2f/s (±%.2f)",
		},
		{
			Name:       "apogee.hitl.pending",
			LabelsJSON: `{}`,
			Headline:   "HITL backlog growing — %.0f pending vs baseline %.1f (±%.2f)",
		},
	}
}

// Worker is the background anomaly detector. Construct with NewWorker;
// the zero value is not usable. Start spawns a goroutine; Stop signals
// it to exit.
type Worker struct {
	store  *duckdb.Store
	hub    *sse.Hub
	logger *slog.Logger

	// Tunables — exported so tests can override the defaults.
	Tick          time.Duration
	Window        time.Duration
	Baseline      time.Duration
	NormalForWait time.Duration
	Metrics       []MonitoredMetric

	// Clock is the time source the worker uses. Defaults to time.Now in
	// UTC. Tests inject their own to pin the schedule.
	Clock func() time.Time

	// cancel is populated by Start so Stop can unblock the goroutine.
	cancel context.CancelFunc

	// normalStreaks tracks, per (metric, labels_json) tuple, when the
	// window most recently went below NormalThreshold. The detector uses
	// this to decide whether an open spell has dwelled in normality long
	// enough to close.
	mu            sync.Mutex
	normalStreaks map[string]time.Time
}

// NewWorker returns a Worker wired to the given store + hub. Logger may
// be nil. The returned worker uses the production defaults until the
// caller overrides them.
func NewWorker(store *duckdb.Store, hub *sse.Hub, logger *slog.Logger) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{
		store:         store,
		hub:           hub,
		logger:        logger,
		Tick:          DefaultTick,
		Window:        DefaultWindow,
		Baseline:      DefaultBaseline,
		NormalForWait: DefaultNormalForWait,
		Metrics:       DefaultMonitoredMetrics(),
		Clock:         func() time.Time { return time.Now().UTC() },
		normalStreaks: make(map[string]time.Time),
	}
}

// Start spawns the detection goroutine. The worker runs until ctx is
// cancelled or Stop is called.
func (w *Worker) Start(ctx context.Context) error {
	if w.store == nil {
		return fmt.Errorf("watchdog: nil store")
	}
	ctx, cancel := context.WithCancel(ctx)
	w.cancel = cancel

	go func() {
		ticker := time.NewTicker(w.Tick)
		defer ticker.Stop()
		// Initial detection pass so the first UI render is not blank.
		if _, err := w.detectOnce(ctx); err != nil && ctx.Err() == nil {
			w.logger.Debug("watchdog: initial detect", "err", err)
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := w.detectOnce(ctx); err != nil && ctx.Err() == nil {
					w.logger.Debug("watchdog: detect", "err", err)
				}
			}
		}
	}()
	return nil
}

// Stop signals the worker goroutine to exit. Safe to call on a nil or
// unstarted worker.
func (w *Worker) Stop() {
	if w == nil || w.cancel == nil {
		return
	}
	w.cancel()
	w.cancel = nil
}

// DetectOnce exposes a single detection pass to callers (and tests).
func (w *Worker) DetectOnce(ctx context.Context) ([]duckdb.WatchdogSignal, error) {
	return w.detectOnce(ctx)
}

func (w *Worker) detectOnce(ctx context.Context) ([]duckdb.WatchdogSignal, error) {
	now := w.Clock()
	baselineFrom := now.Add(-w.Baseline)
	windowFrom := now.Add(-w.Window)

	var emitted []duckdb.WatchdogSignal
	for _, m := range w.Metrics {
		sig, err := w.detectForMetric(ctx, m, now, baselineFrom, windowFrom)
		if err != nil {
			w.logger.Debug("watchdog: metric", "metric", m.Name, "err", err)
			continue
		}
		if sig != nil {
			emitted = append(emitted, *sig)
		}
	}
	return emitted, nil
}

func (w *Worker) detectForMetric(
	ctx context.Context,
	m MonitoredMetric,
	now, baselineFrom, windowFrom time.Time,
) (*duckdb.WatchdogSignal, error) {
	// Pull every sample in the baseline window from the store. The worker
	// deliberately does not re-bucket: the collector already writes one
	// row per sampling tick, so the raw points are a sensible baseline
	// without needing time_bucket().
	baselineSamples, err := w.store.ReadMetricWindow(ctx, m.Name, m.LabelsJSON, baselineFrom, windowFrom)
	if err != nil {
		return nil, fmt.Errorf("read baseline: %w", err)
	}
	windowSamples, err := w.store.ReadMetricWindow(ctx, m.Name, m.LabelsJSON, windowFrom, now)
	if err != nil {
		return nil, fmt.Errorf("read window: %w", err)
	}

	baseline := ComputeBaseline(baselineSamples)
	stats := EvaluateWindow(windowSamples, baseline)

	key := m.Name + "\x1f" + m.LabelsJSON

	// Lookup the currently open spell (if any). A spell represents a
	// continuous anomalous period — the detector writes one row per
	// spell and does not re-emit until the window returns to normal.
	openSig, hasOpen, err := w.store.LatestOpenWatchdogSpell(ctx, m.Name, m.LabelsJSON)
	if err != nil {
		return nil, fmt.Errorf("latest open: %w", err)
	}

	// Track the return-to-normal streak. If the latest window is
	// entirely below NormalThreshold, record the timestamp; otherwise
	// clear it.
	w.mu.Lock()
	if stats.AllBelow {
		if _, ok := w.normalStreaks[key]; !ok {
			w.normalStreaks[key] = now
		}
	} else {
		delete(w.normalStreaks, key)
	}
	startedNormalAt, inNormalStreak := w.normalStreaks[key]
	w.mu.Unlock()

	// Close an open spell once the window has been quiet for long
	// enough. This frees the detector to emit a fresh signal the next
	// time the metric deviates.
	if hasOpen && inNormalStreak && now.Sub(startedNormalAt) >= w.NormalForWait {
		if err := w.store.CloseWatchdogSpell(ctx, openSig.ID, now); err != nil {
			return nil, fmt.Errorf("close spell: %w", err)
		}
		hasOpen = false
	}

	severity := SeverityFor(stats.MaxAbsZ)
	if severity == "" {
		return nil, nil
	}
	// Dedup: while a spell is open, swallow subsequent signals so the
	// bell does not flash on every tick.
	if hasOpen {
		return nil, nil
	}

	headline := fmt.Sprintf(m.Headline, stats.PeakValue, baseline.Mean, baseline.Stddev)
	evidence := buildEvidence(windowSamples, baseline)
	evJSON, err := json.Marshal(evidence)
	if err != nil {
		return nil, fmt.Errorf("marshal evidence: %w", err)
	}

	row := duckdb.WatchdogSignal{
		DetectedAt:     now,
		MetricName:     m.Name,
		LabelsJSON:     normaliseLabels(m.LabelsJSON),
		ZScore:         stats.MaxAbsZ,
		BaselineMean:   baseline.Mean,
		BaselineStddev: baseline.Stddev,
		WindowValue:    stats.PeakValue,
		Severity:       severity,
		Headline:       headline,
		EvidenceJSON:   string(evJSON),
	}
	inserted, err := w.store.InsertWatchdogSignal(ctx, row)
	if err != nil {
		return nil, fmt.Errorf("insert signal: %w", err)
	}
	if w.hub != nil {
		w.hub.Broadcast(sse.NewWatchdogEvent(now, inserted))
	}
	return &inserted, nil
}

// evidencePayload is the typed shape that rides under evidence_json.
// Matches the wire shape defined in web/app/lib/api-types.ts.
type evidencePayload struct {
	Window   []evidencePoint `json:"window"`
	Baseline evidenceBase    `json:"baseline"`
	Z        []float64       `json:"z"`
}

type evidencePoint struct {
	At    time.Time `json:"at"`
	Value float64   `json:"value"`
}

type evidenceBase struct {
	Mean   float64 `json:"mean"`
	Stddev float64 `json:"stddev"`
}

func buildEvidence(window []duckdb.MetricSeriesPoint, b Baseline) evidencePayload {
	out := evidencePayload{
		Window:   make([]evidencePoint, 0, len(window)),
		Z:        make([]float64, 0, len(window)),
		Baseline: evidenceBase{Mean: roundFloat(b.Mean), Stddev: roundFloat(b.Stddev)},
	}
	for _, pt := range window {
		out.Window = append(out.Window, evidencePoint{At: pt.At, Value: roundFloat(pt.Value)})
		out.Z = append(out.Z, roundFloat(ZScore(pt.Value, b)))
	}
	return out
}

func roundFloat(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	// Two decimal places so the wire payload stays compact.
	return math.Round(v*100) / 100
}

// normaliseLabels guarantees the labels_json column always stores a
// canonical shape. Empty / whitespace strings fall back to "{}" so the
// detector can key consistently against LatestOpenWatchdogSpell.
func normaliseLabels(raw string) string {
	if raw == "" {
		return "{}"
	}
	return raw
}
