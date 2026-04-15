package watchdog

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/BIwashi/apogee/internal/store/duckdb"
)

func mkPoints(vals []float64) []duckdb.MetricSeriesPoint {
	now := time.Now().UTC()
	out := make([]duckdb.MetricSeriesPoint, len(vals))
	for i, v := range vals {
		out[i] = duckdb.MetricSeriesPoint{At: now.Add(time.Duration(i) * time.Second), Value: v}
	}
	return out
}

func TestComputeBaselineConstant(t *testing.T) {
	// Constant series has stddev 0 → degenerate baseline.
	b := ComputeBaseline(mkPoints([]float64{1, 1, 1, 1, 1}))
	require.True(t, b.Degenerate)
	require.Equal(t, 1.0, b.Mean)
}

func TestComputeBaselineSimple(t *testing.T) {
	b := ComputeBaseline(mkPoints([]float64{2, 4, 4, 4, 5, 5, 7, 9}))
	require.False(t, b.Degenerate)
	require.InDelta(t, 5.0, b.Mean, 1e-9)
	require.InDelta(t, 2.0, b.Stddev, 1e-9)
}

func TestComputeBaselineSmallN(t *testing.T) {
	b := ComputeBaseline(mkPoints([]float64{1, 2}))
	require.True(t, b.Degenerate)
}

func TestZScoreMath(t *testing.T) {
	b := Baseline{Mean: 5.0, Stddev: 2.0, Count: 8}
	require.InDelta(t, 0.0, ZScore(5.0, b), 1e-9)
	require.InDelta(t, 1.5, ZScore(8.0, b), 1e-9)
	require.InDelta(t, -2.0, ZScore(1.0, b), 1e-9)
}

func TestZScoreDegenerate(t *testing.T) {
	b := Baseline{Mean: 5.0, Stddev: 0, Count: 1, Degenerate: true}
	require.Equal(t, 0.0, ZScore(42.0, b))
}

func TestSeverityThresholds(t *testing.T) {
	require.Equal(t, "", SeverityFor(2.5))
	require.Equal(t, duckdb.WatchdogSeverityInfo, SeverityFor(3.1))
	require.Equal(t, duckdb.WatchdogSeverityInfo, SeverityFor(-4.9))
	require.Equal(t, duckdb.WatchdogSeverityWarning, SeverityFor(5.1))
	require.Equal(t, duckdb.WatchdogSeverityWarning, SeverityFor(-7.9))
	require.Equal(t, duckdb.WatchdogSeverityCritical, SeverityFor(9.0))
	require.Equal(t, duckdb.WatchdogSeverityCritical, SeverityFor(-15.0))
}

func TestEvaluateWindowTracksPeak(t *testing.T) {
	b := Baseline{Mean: 10.0, Stddev: 2.0, Count: 10}
	stats := EvaluateWindow(mkPoints([]float64{10, 11, 18, 12, 10}), b)
	// 18 -> z = 4
	require.InDelta(t, 4.0, stats.MaxAbsZ, 1e-9)
	require.InDelta(t, 18.0, stats.PeakValue, 1e-9)
	require.False(t, stats.AllBelow)
}

func TestEvaluateWindowAllBelow(t *testing.T) {
	b := Baseline{Mean: 10.0, Stddev: 2.0, Count: 10}
	// |z| all < 1.5
	stats := EvaluateWindow(mkPoints([]float64{10, 11, 9, 12, 10}), b)
	require.True(t, stats.AllBelow)
	require.Less(t, math.Abs(stats.MaxAbsZ), NormalThreshold)
}

func TestEvaluateWindowDegenerateBaseline(t *testing.T) {
	b := Baseline{Mean: 10.0, Degenerate: true}
	stats := EvaluateWindow(mkPoints([]float64{10, 11, 50}), b)
	require.True(t, stats.AllBelow)
	require.Equal(t, 0.0, stats.MaxAbsZ)
}
