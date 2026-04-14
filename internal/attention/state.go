// Package attention implements apogee's triage engine. Every running turn is
// scored against a small rule set and classified into one of four buckets —
// intervene_now, watch, watchlist, healthy — so the dashboard can pre-rank
// work without the operator manually filtering.
//
// The engine is deterministic when given the same input: time is injected via
// a Clock function so tests can pin "now" and historical data flows through
// a HistoryReader interface so the core stays free of DuckDB dependencies.
package attention

import (
	"strings"
	"time"
)

// timeNow is injected so tests that stub time can replace it. In production
// it is always time.Now.
var timeNow = func() time.Time { return time.Now().UTC() }

// State enumerates the four attention buckets, ordered from most urgent to
// least.
type State string

const (
	StateInterveneNow State = "intervene_now"
	StateWatch        State = "watch"
	StateWatchlist    State = "watchlist"
	StateHealthy      State = "healthy"
)

// AllStates returns every valid state in priority order (most urgent first).
// Callers use this when they need to enumerate buckets for rendering or
// aggregation.
func AllStates() []State {
	return []State{StateInterveneNow, StateWatch, StateWatchlist, StateHealthy}
}

// Order returns the priority rank for a state, 0 being the most urgent. Used
// as the primary sort key on every turn list in the dashboard.
func Order(s State) int {
	switch s {
	case StateInterveneNow:
		return 0
	case StateWatch:
		return 1
	case StateWatchlist:
		return 2
	case StateHealthy:
		return 3
	default:
		// Unknown / empty state sorts after healthy so legacy rows do not
		// jump to the top of the list.
		return 4
	}
}

// Tone maps an attention state to a design-system tone name the web UI uses
// to pick a color. Tones are drawn from the semantic status palette defined
// in docs/design-tokens.md.
func Tone(s State) string {
	switch s {
	case StateInterveneNow:
		return "critical"
	case StateWatch:
		return "warning"
	case StateWatchlist:
		return "info"
	case StateHealthy:
		return "success"
	default:
		return "muted"
	}
}

// Parse normalises an arbitrary string into a State. Unknown or empty values
// default to StateHealthy so pre-engine data degrades gracefully.
func Parse(s string) State {
	switch State(strings.ToLower(strings.TrimSpace(s))) {
	case StateInterveneNow:
		return StateInterveneNow
	case StateWatch:
		return StateWatch
	case StateWatchlist:
		return StateWatchlist
	case StateHealthy:
		return StateHealthy
	}
	return StateHealthy
}

// String returns the wire form of a state, suitable for JSON serialisation
// and DB persistence.
func (s State) String() string { return string(s) }
