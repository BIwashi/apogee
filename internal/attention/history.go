package attention

import (
	"sort"
	"strings"
	"time"
)

// HistoryReader exposes pattern-level stats to the engine. The default
// implementation is backed by the task_type_history DuckDB table, but tests
// may stub it with an in-memory map.
type HistoryReader interface {
	Lookup(pattern string) (PatternStats, error)
}

// HistoryWriter records the outcome of a closed turn against its tool
// signature. Separating reads from writes keeps tests simple — the engine
// only ever needs the reader.
type HistoryWriter interface {
	Upsert(pattern string, outcome Outcome) error
}

// PatternStats is the rolling success/failure stat for a given tool
// signature. Pattern is canonicalised with CanonicalPattern.
type PatternStats struct {
	Pattern      string
	TurnCount    int
	FailureCount int
	LastUpdated  time.Time
}

// FailureRate returns the fraction of turns with this pattern that ended in
// failure. Returns 0 when TurnCount is 0.
func (p PatternStats) FailureRate() float64 {
	if p.TurnCount <= 0 {
		return 0
	}
	return float64(p.FailureCount) / float64(p.TurnCount)
}

// Outcome is the single-turn record fed to HistoryWriter.Upsert.
type Outcome struct {
	Success bool
	TurnID  string
}

// CanonicalPattern returns a stable pattern signature for a list of tool
// names. Duplicates are removed, entries are sorted lexicographically, then
// joined with "|" — `"Bash|Edit|Read"`. Empty or blank entries are dropped.
// Returns the empty string when no valid tools remain.
func CanonicalPattern(tools []string) string {
	seen := make(map[string]struct{}, len(tools))
	unique := make([]string, 0, len(tools))
	for _, t := range tools {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		unique = append(unique, t)
	}
	sort.Strings(unique)
	return strings.Join(unique, "|")
}

// StaticHistory is an in-memory HistoryReader/Writer useful for tests and
// fallbacks when no store is wired. Safe for single-goroutine use.
type StaticHistory struct {
	Data map[string]PatternStats
}

// NewStaticHistory returns a ready-to-use StaticHistory.
func NewStaticHistory() *StaticHistory {
	return &StaticHistory{Data: map[string]PatternStats{}}
}

// Lookup implements HistoryReader.
func (h *StaticHistory) Lookup(pattern string) (PatternStats, error) {
	if h == nil || h.Data == nil {
		return PatternStats{Pattern: pattern}, nil
	}
	if v, ok := h.Data[pattern]; ok {
		return v, nil
	}
	return PatternStats{Pattern: pattern}, nil
}

// Upsert implements HistoryWriter.
func (h *StaticHistory) Upsert(pattern string, outcome Outcome) error {
	if h.Data == nil {
		h.Data = map[string]PatternStats{}
	}
	ps := h.Data[pattern]
	ps.Pattern = pattern
	ps.TurnCount++
	if !outcome.Success {
		ps.FailureCount++
	}
	ps.LastUpdated = time.Now().UTC()
	h.Data[pattern] = ps
	return nil
}

// nullHistory is the fallback HistoryReader used when the engine has no
// explicit history wired in. It behaves as if every pattern is brand new.
type nullHistory struct{}

func (nullHistory) Lookup(pattern string) (PatternStats, error) {
	return PatternStats{Pattern: pattern}, nil
}
