package attention

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/BIwashi/apogee/internal/store/duckdb"
)

// testNow is the pinned "wall clock" used by every test in this file so the
// engine's output is deterministic.
var testNow = time.Date(2026, 4, 14, 10, 0, 0, 0, time.UTC)

func runningTurn() duckdb.Turn {
	return duckdb.Turn{
		TurnID:    "turn-1",
		SessionID: "sess-1",
		SourceApp: "demo",
		StartedAt: testNow.Add(-2 * time.Minute),
		Status:    "running",
	}
}

func mkToolSpan(name string, start time.Time, dur time.Duration, status string) duckdb.SpanRow {
	end := start.Add(dur)
	return duckdb.SpanRow{
		TraceID:    "trace-1",
		SpanID:     "span-" + name + start.Format("150405.000"),
		Name:       "claude_code.tool." + name,
		Kind:       "INTERNAL",
		StartTime:  start,
		EndTime:    &end,
		StatusCode: status,
		ToolName:   name,
	}
}

func openHITL(start time.Time) duckdb.SpanRow {
	return duckdb.SpanRow{
		TraceID:    "trace-1",
		SpanID:     "span-hitl-" + start.Format("150405.000"),
		Name:       "claude_code.hitl.permission",
		Kind:       "INTERNAL",
		StartTime:  start,
		StatusCode: "UNSET",
	}
}

func TestEngineHealthyByDefault(t *testing.T) {
	e := NewEngine(nil)
	spans := []duckdb.SpanRow{
		mkToolSpan("Read", testNow.Add(-10*time.Second), 2*time.Second, "OK"),
	}
	decision := e.Score(Input{Turn: runningTurn(), Spans: spans, Now: testNow})
	require.Equal(t, StateHealthy, decision.State)
	require.Equal(t, "success", decision.Tone)
}

func TestEngineInterveneOnHITLPending(t *testing.T) {
	e := NewEngine(nil)
	spans := []duckdb.SpanRow{openHITL(testNow.Add(-45 * time.Second))}
	d := e.Score(Input{Turn: runningTurn(), Spans: spans, Now: testNow})
	require.Equal(t, StateInterveneNow, d.State)
	require.Equal(t, "critical", d.Tone)
	require.Contains(t, d.Reason, "HITL permission")
}

func TestEngineInterveneOnErrorStreak(t *testing.T) {
	e := NewEngine(nil)
	base := testNow.Add(-30 * time.Second)
	spans := []duckdb.SpanRow{
		mkToolSpan("Bash", base, time.Second, "ERROR"),
		mkToolSpan("Bash", base.Add(5*time.Second), time.Second, "ERROR"),
		mkToolSpan("Bash", base.Add(10*time.Second), time.Second, "ERROR"),
	}
	d := e.Score(Input{Turn: runningTurn(), Spans: spans, Now: testNow})
	require.Equal(t, StateInterveneNow, d.State)
	require.Contains(t, d.Reason, "consecutive tool errors")
}

func TestEngineInterveneOnIdle(t *testing.T) {
	e := NewEngine(nil)
	spans := []duckdb.SpanRow{
		mkToolSpan("Read", testNow.Add(-10*time.Minute), time.Second, "OK"),
	}
	d := e.Score(Input{Turn: runningTurn(), Spans: spans, Now: testNow})
	require.Equal(t, StateInterveneNow, d.State)
	require.Contains(t, d.Reason, "idle")
}

func TestEngineWatchOnSingleError(t *testing.T) {
	e := NewEngine(nil)
	spans := []duckdb.SpanRow{
		mkToolSpan("Read", testNow.Add(-20*time.Second), time.Second, "OK"),
		mkToolSpan("Bash", testNow.Add(-10*time.Second), time.Second, "ERROR"),
	}
	d := e.Score(Input{Turn: runningTurn(), Spans: spans, Now: testNow})
	require.Equal(t, StateWatch, d.State)
	require.Equal(t, "warning", d.Tone)
}

func TestEngineWatchOnPhaseStall(t *testing.T) {
	e := NewEngine(nil)
	start := testNow.Add(-4 * time.Minute)
	spans := []duckdb.SpanRow{
		mkToolSpan("Edit", start, time.Second, "OK"),
		mkToolSpan("Edit", start.Add(30*time.Second), time.Second, "OK"),
		mkToolSpan("Edit", start.Add(60*time.Second), time.Second, "OK"),
		mkToolSpan("Edit", start.Add(90*time.Second), time.Second, "OK"),
	}
	d := e.Score(Input{Turn: runningTurn(), Spans: spans, Now: testNow})
	require.Equal(t, StateWatch, d.State)
	require.Contains(t, d.Reason, "editing")
}

func TestEngineWatchOnTokenBurn(t *testing.T) {
	e := NewEngine(nil)
	// Set defaults to something deterministic for this test
	e.Rules.TokenBurnWarningPerMin = 10
	tokens := int64(1_000_000)
	turn := runningTurn()
	// 1M tokens over 2 minutes = 500k/min, way above 10
	turn.InputTokens = &tokens
	spans := []duckdb.SpanRow{
		mkToolSpan("Read", testNow.Add(-10*time.Second), time.Second, "OK"),
	}
	d := e.Score(Input{Turn: turn, Spans: spans, Now: testNow})
	// token_burn is a warning-only signal, so we expect watch (unless other
	// rules fired first).
	require.Equal(t, StateWatch, d.State)
}

func TestEngineWatchlistFromHistory(t *testing.T) {
	hist := NewStaticHistory()
	hist.Data["Bash|Read"] = PatternStats{
		Pattern:      "Bash|Read",
		TurnCount:    10,
		FailureCount: 6,
		LastUpdated:  testNow,
	}
	e := NewEngine(hist)
	spans := []duckdb.SpanRow{
		mkToolSpan("Read", testNow.Add(-10*time.Second), time.Second, "OK"),
		mkToolSpan("Bash", testNow.Add(-5*time.Second), time.Second, "OK"),
	}
	d := e.Score(Input{Turn: runningTurn(), Spans: spans, Now: testNow})
	require.Equal(t, StateWatchlist, d.State)
	require.Equal(t, "info", d.Tone)
	require.Contains(t, d.Reason, "historical failure rate")
}

func TestEngineHistorySkippedBelowSampleSize(t *testing.T) {
	hist := NewStaticHistory()
	hist.Data["Bash|Read"] = PatternStats{
		Pattern:      "Bash|Read",
		TurnCount:    2, // below minimum
		FailureCount: 2,
	}
	e := NewEngine(hist)
	spans := []duckdb.SpanRow{
		mkToolSpan("Read", testNow.Add(-10*time.Second), time.Second, "OK"),
		mkToolSpan("Bash", testNow.Add(-5*time.Second), time.Second, "OK"),
	}
	d := e.Score(Input{Turn: runningTurn(), Spans: spans, Now: testNow})
	require.Equal(t, StateHealthy, d.State)
}

func TestOrderAndTone(t *testing.T) {
	require.Equal(t, 0, Order(StateInterveneNow))
	require.Equal(t, 3, Order(StateHealthy))
	require.Equal(t, "critical", Tone(StateInterveneNow))
	require.Equal(t, "success", Tone(StateHealthy))
	require.Equal(t, "muted", Tone(State("unknown")))
}

func TestParseFallbackToHealthy(t *testing.T) {
	require.Equal(t, StateHealthy, Parse(""))
	require.Equal(t, StateHealthy, Parse("junk"))
	require.Equal(t, StateInterveneNow, Parse("intervene_now"))
}

func TestCanonicalPattern(t *testing.T) {
	require.Equal(t, "Bash|Edit|Read", CanonicalPattern([]string{"Read", "Bash", "Edit", "Bash"}))
	require.Equal(t, "", CanonicalPattern([]string{"", "  "}))
}

func TestComputePhase(t *testing.T) {
	// Editing majority
	spans := []duckdb.SpanRow{
		mkToolSpan("Edit", testNow.Add(-30*time.Second), time.Second, "OK"),
		mkToolSpan("Edit", testNow.Add(-20*time.Second), time.Second, "OK"),
		mkToolSpan("Edit", testNow.Add(-10*time.Second), time.Second, "OK"),
		mkToolSpan("Read", testNow.Add(-5*time.Second), time.Second, "OK"),
	}
	p := ComputePhase(spans, testNow)
	require.Equal(t, PhaseEditing, p.Name)
	require.Greater(t, p.Confidence, 0.5)

	// Empty → idle
	p = ComputePhase(nil, testNow)
	require.Equal(t, PhaseIdle, p.Name)
}

func TestComputePhaseBashHeuristics(t *testing.T) {
	start := testNow.Add(-30 * time.Second)
	mk := func(cmd string, off time.Duration) duckdb.SpanRow {
		sp := mkToolSpan("Bash", start.Add(off), time.Second, "OK")
		sp.Attributes = map[string]any{"claude_code.tool.input": cmd}
		return sp
	}
	// Two "go test" + one unrelated bash should trip testing.
	spans := []duckdb.SpanRow{
		mk(`go test ./...`, 0),
		mk(`go test ./pkg/foo`, 5*time.Second),
		mk(`echo hi`, 10*time.Second),
	}
	p := ComputePhase(spans, testNow)
	require.Equal(t, PhaseTesting, p.Name)

	spans = []duckdb.SpanRow{
		mk(`git status`, 0),
		mk(`git add .`, 5*time.Second),
		mk(`git commit -m x`, 10*time.Second),
	}
	p = ComputePhase(spans, testNow)
	require.Equal(t, PhaseCommitting, p.Name)
}
