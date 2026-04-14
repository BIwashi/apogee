package attention

import (
	"fmt"
	"strings"
	"time"

	"github.com/BIwashi/apogee/internal/store/duckdb"
)

// RuleSet holds the tunable thresholds the engine evaluates against.
// Zero-valued RuleSet is not useful directly — call DefaultRuleSet() for
// sensible defaults.
type RuleSet struct {
	// HITLPendingCritical fires intervene_now when an HITL permission span
	// has been open longer than this.
	HITLPendingCritical time.Duration
	// ErrorStreakCritical fires intervene_now when this many tool spans in a
	// row returned status=ERROR.
	ErrorStreakCritical int
	// ErrorStreakWarning fires watch when the error streak meets this count.
	ErrorStreakWarning int
	// IdleCritical fires intervene_now when the most recent span ended more
	// than this long ago and the turn is still running.
	IdleCritical time.Duration
	// PhaseStallWarning fires watch when the same heuristic phase has been
	// active for longer than this.
	PhaseStallWarning time.Duration
	// TokenBurnWarningPerMin fires watch when estimated input tokens in the
	// last 60 s exceed this number.
	TokenBurnWarningPerMin float64
	// HistoryMinTurns is the minimum past turn count required before a
	// pattern is considered for the watchlist bucket.
	HistoryMinTurns int
	// HistoryFailureRate is the minimum historical failure rate that will
	// trigger a watchlist classification.
	HistoryFailureRate float64
}

// DefaultRuleSet returns the attention engine defaults documented in the
// PR #4 brief.
func DefaultRuleSet() RuleSet {
	return RuleSet{
		HITLPendingCritical:    30 * time.Second,
		ErrorStreakCritical:    3,
		ErrorStreakWarning:     1,
		IdleCritical:           5 * time.Minute,
		PhaseStallWarning:      3 * time.Minute,
		TokenBurnWarningPerMin: 100_000,
		HistoryMinTurns:        5,
		HistoryFailureRate:     0.25,
	}
}

// Engine turns a per-turn snapshot into a Decision. It is stateless between
// calls; all state lives in the injected clock, history, and rules.
type Engine struct {
	Clock   func() time.Time
	History HistoryReader
	Rules   RuleSet
}

// NewEngine constructs an engine with the given history reader and the
// default rule set. Pass nil history to use a no-op reader.
func NewEngine(history HistoryReader) *Engine {
	if history == nil {
		history = nullHistory{}
	}
	return &Engine{
		Clock:   time.Now,
		History: history,
		Rules:   DefaultRuleSet(),
	}
}

// Input carries everything the engine needs to score one turn.
type Input struct {
	Turn  duckdb.Turn
	Spans []duckdb.SpanRow
	// Now is the wall-clock moment at which the decision is computed. Must
	// be set by the caller — the engine never reads time.Now directly during
	// rule evaluation so tests stay deterministic.
	Now time.Time
}

// Decision is the result of a single scoring pass.
type Decision struct {
	State   State
	Score   float64
	Reason  string
	Tone    string
	Signals []Signal
	Phase   PhaseResult
}

// Signal is a single piece of evidence the engine considered while
// classifying a turn. Callers may surface these for debugging or as tool-tips
// on the dashboard.
type Signal struct {
	Kind      string  `json:"kind"`
	Value     any     `json:"value,omitempty"`
	Threshold any     `json:"threshold,omitempty"`
	Weight    float64 `json:"weight"`
}

// Score runs the attention rule set against the input and returns a decision.
// The returned Decision always has a non-empty State; pre-engine / empty
// inputs degrade to healthy.
func (e *Engine) Score(in Input) Decision {
	if e == nil {
		e = NewEngine(nil)
	}
	rules := e.Rules
	if rules.HITLPendingCritical == 0 && rules.ErrorStreakCritical == 0 && rules.IdleCritical == 0 {
		rules = DefaultRuleSet()
	}
	now := in.Now
	if now.IsZero() {
		if e.Clock != nil {
			now = e.Clock()
		} else {
			now = time.Now()
		}
	}

	phase := ComputePhase(in.Spans, now)

	decision := Decision{
		State:  StateHealthy,
		Reason: "no active symptoms",
		Tone:   Tone(StateHealthy),
		Phase:  phase,
	}

	// Rules are evaluated in strict priority order. The first rule whose
	// signals fire determines the bucket. Lower-priority rules still record
	// their signals for debugging.
	var (
		critical []Signal
		warnings []Signal
	)

	// ── intervene_now rules ────────────────────────────────
	if sig, ok := checkHITLPending(in.Spans, now, rules.HITLPendingCritical); ok {
		critical = append(critical, sig)
	}
	if sig, ok := checkErrorStreak(in.Spans, rules.ErrorStreakCritical, 1.0); ok {
		sig.Kind = "error_streak_critical"
		critical = append(critical, sig)
	}
	if sig, ok := checkIdle(in.Turn, in.Spans, now, rules.IdleCritical); ok {
		critical = append(critical, sig)
	}

	// ── watch rules ────────────────────────────────────────
	if sig, ok := checkErrorStreak(in.Spans, rules.ErrorStreakWarning, 0.6); ok {
		sig.Kind = "error_streak_warning"
		warnings = append(warnings, sig)
	}
	if sig, ok := checkPhaseStall(phase, now, rules.PhaseStallWarning); ok {
		warnings = append(warnings, sig)
	}
	if sig, ok := checkTokenBurn(in.Turn, now, rules.TokenBurnWarningPerMin); ok {
		warnings = append(warnings, sig)
	}

	signals := append([]Signal{}, critical...)
	signals = append(signals, warnings...)

	if len(critical) > 0 {
		decision.State = StateInterveneNow
		decision.Reason = reasonFrom(critical)
		decision.Score = maxWeight(critical)
	} else if len(warnings) > 0 {
		decision.State = StateWatch
		decision.Reason = reasonFrom(warnings)
		decision.Score = maxWeight(warnings)
	} else if sig, ok := e.checkHistory(in.Spans, rules); ok {
		decision.State = StateWatchlist
		decision.Reason = sig.Reason
		decision.Score = sig.Signal.Weight
		signals = append(signals, sig.Signal)
	}

	decision.Signals = signals
	decision.Tone = Tone(decision.State)
	return decision
}

// maxWeight returns the highest weight across a slice of signals. Empty
// slices return 0.
func maxWeight(ss []Signal) float64 {
	m := 0.0
	for _, s := range ss {
		if s.Weight > m {
			m = s.Weight
		}
	}
	return m
}

// reasonFrom composes a short English sentence from the highest-weight
// signal in the slice. Callers only use this when the slice is non-empty.
func reasonFrom(ss []Signal) string {
	if len(ss) == 0 {
		return ""
	}
	top := ss[0]
	for _, s := range ss[1:] {
		if s.Weight > top.Weight {
			top = s
		}
	}
	switch top.Kind {
	case "hitl_pending":
		return fmt.Sprintf("HITL permission request pending for %v", top.Value)
	case "error_streak_critical":
		return fmt.Sprintf("%v consecutive tool errors", top.Value)
	case "error_streak_warning":
		return fmt.Sprintf("%v consecutive tool error(s)", top.Value)
	case "idle":
		return fmt.Sprintf("turn has been idle for %v", top.Value)
	case "phase_stall":
		return fmt.Sprintf("stuck in %v phase for %v", top.Threshold, top.Value)
	case "token_burn":
		return fmt.Sprintf("input tokens burning at %v / min", top.Value)
	case "history_failure":
		return fmt.Sprintf("historical failure rate %v for pattern %q", top.Value, top.Threshold)
	}
	return fmt.Sprintf("rule %q fired", top.Kind)
}

// ── individual rule checkers ────────────────────────────────

// checkHITLPending inspects every HITL permission span and returns a signal
// when one has been open longer than the threshold.
func checkHITLPending(spans []duckdb.SpanRow, now time.Time, threshold time.Duration) (Signal, bool) {
	var worst time.Duration
	for _, sp := range spans {
		if sp.Name != "claude_code.hitl.permission" {
			continue
		}
		if sp.EndTime != nil {
			continue
		}
		age := now.Sub(sp.StartTime)
		if age > worst {
			worst = age
		}
	}
	if worst >= threshold && threshold > 0 {
		return Signal{
			Kind:      "hitl_pending",
			Value:     worst.Round(time.Second),
			Threshold: threshold,
			Weight:    1.0,
		}, true
	}
	return Signal{}, false
}

// checkErrorStreak counts consecutive tool-span errors, looking backwards
// from the end of the span list. The streak is terminated by any
// successfully-closed tool span.
func checkErrorStreak(spans []duckdb.SpanRow, threshold int, weight float64) (Signal, bool) {
	if threshold <= 0 {
		return Signal{}, false
	}
	streak := 0
	for i := len(spans) - 1; i >= 0; i-- {
		sp := spans[i]
		if sp.ToolName == "" {
			continue
		}
		if sp.EndTime == nil {
			// Open tool span — ignore, streak continues from the previous
			// closed span.
			continue
		}
		if sp.StatusCode == "ERROR" {
			streak++
			continue
		}
		break
	}
	if streak >= threshold {
		return Signal{
			Value:     streak,
			Threshold: threshold,
			Weight:    weight,
		}, true
	}
	return Signal{}, false
}

// checkIdle fires when a running turn has seen no span activity for longer
// than the threshold.
func checkIdle(turn duckdb.Turn, spans []duckdb.SpanRow, now time.Time, threshold time.Duration) (Signal, bool) {
	if turn.Status != "running" || threshold <= 0 {
		return Signal{}, false
	}
	var latest time.Time
	for _, sp := range spans {
		if sp.EndTime != nil && sp.EndTime.After(latest) {
			latest = *sp.EndTime
		}
		if sp.StartTime.After(latest) {
			latest = sp.StartTime
		}
	}
	if latest.IsZero() {
		latest = turn.StartedAt
	}
	age := now.Sub(latest)
	if age >= threshold {
		return Signal{
			Kind:      "idle",
			Value:     age.Round(time.Second),
			Threshold: threshold,
			Weight:    0.9,
		}, true
	}
	return Signal{}, false
}

// checkPhaseStall fires when the current phase has been active for longer
// than the stall threshold and is one of the "work" phases (not idle).
func checkPhaseStall(phase PhaseResult, now time.Time, threshold time.Duration) (Signal, bool) {
	if threshold <= 0 || phase.Name == PhaseIdle {
		return Signal{}, false
	}
	if phase.Since.IsZero() {
		return Signal{}, false
	}
	dur := now.Sub(phase.Since)
	if dur >= threshold {
		return Signal{
			Kind:      "phase_stall",
			Value:     dur.Round(time.Second),
			Threshold: phase.Name,
			Weight:    0.5,
		}, true
	}
	return Signal{}, false
}

// checkTokenBurn uses the turn's rolling input-token total and duration to
// estimate tokens/min. It is a coarse proxy — PR #8 will wire in proper
// OTel metric samples — but it catches runaway summarisers today.
func checkTokenBurn(turn duckdb.Turn, now time.Time, rate float64) (Signal, bool) {
	if rate <= 0 || turn.InputTokens == nil {
		return Signal{}, false
	}
	if *turn.InputTokens <= 0 {
		return Signal{}, false
	}
	elapsed := now.Sub(turn.StartedAt).Minutes()
	if elapsed <= 0 {
		return Signal{}, false
	}
	perMin := float64(*turn.InputTokens) / elapsed
	if perMin >= rate {
		return Signal{
			Kind:      "token_burn",
			Value:     int(perMin),
			Threshold: rate,
			Weight:    0.4,
		}, true
	}
	return Signal{}, false
}

// historyHit bundles a triggered history signal with its composed reason so
// the engine can promote it to a Decision without re-deriving either value.
type historyHit struct {
	Signal Signal
	Reason string
}

// checkHistory consults the HistoryReader for the canonical pattern of the
// turn so far. A pattern with a sufficient sample size and a failure rate
// above the threshold promotes the turn into the watchlist bucket.
func (e *Engine) checkHistory(spans []duckdb.SpanRow, rules RuleSet) (historyHit, bool) {
	if e.History == nil {
		return historyHit{}, false
	}
	tools := toolNames(spans)
	if len(tools) == 0 {
		return historyHit{}, false
	}
	pattern := CanonicalPattern(tools)
	stats, err := e.History.Lookup(pattern)
	if err != nil || stats.TurnCount < rules.HistoryMinTurns {
		return historyHit{}, false
	}
	if stats.FailureRate() < rules.HistoryFailureRate {
		return historyHit{}, false
	}
	sig := Signal{
		Kind:      "history_failure",
		Value:     fmt.Sprintf("%.0f%%", stats.FailureRate()*100),
		Threshold: pattern,
		Weight:    0.3,
	}
	return historyHit{
		Signal: sig,
		Reason: fmt.Sprintf("historical failure rate %.0f%% for pattern %q (%d samples)",
			stats.FailureRate()*100, pattern, stats.TurnCount),
	}, true
}

// toolNames extracts the ordered list of tool names for every tool span.
func toolNames(spans []duckdb.SpanRow) []string {
	out := make([]string, 0, len(spans))
	for _, sp := range spans {
		if sp.ToolName == "" {
			continue
		}
		out = append(out, sp.ToolName)
	}
	return out
}

// ToolNamesForPattern is exported so the reconstructor can compute the
// canonical pattern for a closed turn in the same way the engine does.
func ToolNamesForPattern(spans []duckdb.SpanRow) string {
	names := toolNames(spans)
	if len(names) == 0 {
		return ""
	}
	return CanonicalPattern(names)
}

// JoinCauses is a tiny helper for composing multi-signal reason strings.
// Currently unused by the engine itself but kept for test convenience.
func JoinCauses(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			continue
		}
		out = append(out, p)
	}
	return strings.Join(out, "; ")
}
