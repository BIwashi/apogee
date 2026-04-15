// Package watchdog is apogee's Datadog-Watchdog-style statistical
// anomaly detector. A background worker samples metric_points every
// tick, computes a rolling 24h baseline for each monitored metric, and
// emits a watchdog_signals row whenever the window deviates by more
// than 3 standard deviations.
//
// The math lives in zscore.go; the worker loop and dedup logic live in
// watchdog.go. The DuckDB persistence layer is in
// internal/store/duckdb/watchdog_signals.go.
package watchdog

import (
	"math"

	"github.com/BIwashi/apogee/internal/store/duckdb"
)

// Threshold constants — anything above |3| is a signal, anything above
// |5| is a warning, anything above |8| is critical. The 1.5 threshold is
// used to decide when a "spell" ends: once the metric has stayed below
// it for NormalForDuration the detector is free to emit a new signal.
const (
	SignalThreshold   = 3.0
	WarningThreshold  = 5.0
	CriticalThreshold = 8.0
	NormalThreshold   = 1.5
)

// Baseline captures the rolling-window statistics the detector uses as
// the denominator of the z-score. Mean and Stddev are computed over the
// raw samples. When the sample size is too small (< 3 points) or the
// standard deviation collapses to zero, the baseline is considered
// degenerate and Degenerate is true; the caller should skip emission.
type Baseline struct {
	Mean       float64
	Stddev     float64
	Count      int
	Degenerate bool
}

// ComputeBaseline returns the rolling mean + stddev for the given
// samples. Missing points do not participate in the statistic; zero
// values do. Degenerate baselines (empty, single-sample, or
// zero-variance) are flagged so the caller can skip emission.
func ComputeBaseline(samples []duckdb.MetricSeriesPoint) Baseline {
	n := len(samples)
	if n < 3 {
		return Baseline{Count: n, Degenerate: true}
	}
	var sum float64
	for _, s := range samples {
		sum += s.Value
	}
	mean := sum / float64(n)
	var sq float64
	for _, s := range samples {
		d := s.Value - mean
		sq += d * d
	}
	// Use the population stddev — the caller feeds the detector a
	// closed historical window, not a sample of a larger distribution.
	variance := sq / float64(n)
	stddev := math.Sqrt(variance)
	if stddev <= 0 || math.IsNaN(stddev) || math.IsInf(stddev, 0) {
		return Baseline{Mean: mean, Count: n, Degenerate: true}
	}
	return Baseline{Mean: mean, Stddev: stddev, Count: n}
}

// ZScore returns (value - mean) / stddev. Safe when the baseline is
// degenerate — it returns 0 so the caller's threshold check naturally
// skips emission.
func ZScore(value float64, b Baseline) float64 {
	if b.Degenerate || b.Stddev == 0 {
		return 0
	}
	return (value - b.Mean) / b.Stddev
}

// SeverityFor maps a raw z-score to one of the three severity tiers
// used on the wire and in the UI. Returns the empty string when |z|
// is below SignalThreshold — the caller should not emit in that case.
func SeverityFor(z float64) string {
	abs := math.Abs(z)
	switch {
	case abs >= CriticalThreshold:
		return duckdb.WatchdogSeverityCritical
	case abs >= WarningThreshold:
		return duckdb.WatchdogSeverityWarning
	case abs >= SignalThreshold:
		return duckdb.WatchdogSeverityInfo
	default:
		return ""
	}
}

// WindowStats summarises a window of samples. MaxAbsZ is the most
// extreme z-score observed in the window (signed); PeakValue is the
// value that produced it. Count is the number of samples the window
// contained.
type WindowStats struct {
	MaxAbsZ   float64
	PeakValue float64
	Count     int
	AllBelow  bool // true when every |z| < NormalThreshold
}

// EvaluateWindow walks window through Baseline b and returns the
// summary stats the worker needs to decide whether to emit or close a
// spell. AllBelow is set when every window sample z-score has absolute
// value below NormalThreshold — the signal marker used to close a
// spell after the return-to-normal dwell time elapses.
func EvaluateWindow(window []duckdb.MetricSeriesPoint, b Baseline) WindowStats {
	stats := WindowStats{AllBelow: true, Count: len(window)}
	if b.Degenerate || len(window) == 0 {
		stats.AllBelow = true
		return stats
	}
	for _, pt := range window {
		z := ZScore(pt.Value, b)
		if math.Abs(z) > math.Abs(stats.MaxAbsZ) {
			stats.MaxAbsZ = z
			stats.PeakValue = pt.Value
		}
		if math.Abs(z) >= NormalThreshold {
			stats.AllBelow = false
		}
	}
	return stats
}
