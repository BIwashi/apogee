package summarizer

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// RecapOutcome is the enum of terminal outcomes the summariser will
// assign. Keep aligned with web/app/lib/api-types.ts.
type RecapOutcome string

const (
	OutcomeSuccess RecapOutcome = "success"
	OutcomePartial RecapOutcome = "partial"
	OutcomeFailure RecapOutcome = "failure"
	OutcomeAborted RecapOutcome = "aborted"
)

// IsValid reports whether the outcome is one of the four enum values.
func (o RecapOutcome) IsValid() bool {
	switch o {
	case OutcomeSuccess, OutcomePartial, OutcomeFailure, OutcomeAborted:
		return true
	}
	return false
}

// RecapPhase is one refined phase segment produced by the LLM. Indices are
// inclusive and refer to the span table's printing order.
type RecapPhase struct {
	Name           string `json:"name"`
	StartSpanIndex int    `json:"start_span_index"`
	EndSpanIndex   int    `json:"end_span_index"`
	Summary        string `json:"summary"`
}

// Recap is the structured summary of a turn. It is persisted as JSON on
// the turns row and consumed by the web dashboard.
type Recap struct {
	Headline      string       `json:"headline"`
	Outcome       RecapOutcome `json:"outcome"`
	Phases        []RecapPhase `json:"phases"`
	KeySteps      []string     `json:"key_steps"`
	FailureCause  *string      `json:"failure_cause"`
	NotableEvents []string     `json:"notable_events"`
	GeneratedAt   time.Time    `json:"generated_at"`
	Model         string       `json:"model"`
	PromptTokens  int          `json:"prompt_tokens,omitempty"`
	OutputTokens  int          `json:"output_tokens,omitempty"`
}

// Parse tolerates common LLM output quirks (leading/trailing prose,
// triple-backtick fences) before unmarshalling. spanCount is used for the
// phase-index bounds check; pass 0 to skip that rule entirely.
func Parse(raw string, spanCount int) (Recap, error) {
	cleaned := strings.TrimSpace(raw)
	cleaned = stripCodeFences(cleaned)
	cleaned = extractJSONObject(cleaned)
	if cleaned == "" {
		return Recap{}, fmt.Errorf("recap: empty or unparseable input")
	}

	var r Recap
	if err := json.Unmarshal([]byte(cleaned), &r); err != nil {
		return Recap{}, fmt.Errorf("recap: unmarshal: %w", err)
	}
	return validate(r, spanCount)
}

// validate enforces the hard rules described in the prompt. Soft rules
// (headline length, array caps) silently truncate.
func validate(r Recap, spanCount int) (Recap, error) {
	r.Headline = strings.TrimSpace(r.Headline)
	if r.Headline == "" {
		return Recap{}, fmt.Errorf("recap: headline is required")
	}
	if len(r.Headline) > 140 {
		r.Headline = r.Headline[:140]
	}

	if !r.Outcome.IsValid() {
		return Recap{}, fmt.Errorf("recap: invalid outcome %q", r.Outcome)
	}

	// Phase validation: indices within bounds, start<=end.
	phases := make([]RecapPhase, 0, len(r.Phases))
	for i, p := range r.Phases {
		if p.StartSpanIndex > p.EndSpanIndex {
			return Recap{}, fmt.Errorf("recap: phase %d has start > end (%d > %d)", i, p.StartSpanIndex, p.EndSpanIndex)
		}
		if spanCount > 0 {
			if p.StartSpanIndex < 0 || p.EndSpanIndex >= spanCount {
				return Recap{}, fmt.Errorf("recap: phase %d index out of range [0,%d): [%d,%d]", i, spanCount, p.StartSpanIndex, p.EndSpanIndex)
			}
		} else if p.StartSpanIndex < 0 {
			return Recap{}, fmt.Errorf("recap: phase %d has negative start index", i)
		}
		if len(p.Summary) > 80 {
			p.Summary = p.Summary[:80]
		}
		phases = append(phases, p)
	}
	r.Phases = phases

	r.KeySteps = truncateStringSlice(r.KeySteps, 10, 80)
	r.NotableEvents = truncateStringSlice(r.NotableEvents, 10, 80)

	// FailureCause reconciliation.
	if r.Outcome == OutcomeSuccess {
		r.FailureCause = nil
	} else if r.FailureCause != nil {
		trimmed := strings.TrimSpace(*r.FailureCause)
		if trimmed == "" {
			r.FailureCause = nil
		} else {
			r.FailureCause = &trimmed
		}
	}

	return r, nil
}

func truncateStringSlice(items []string, maxItems, maxLen int) []string {
	if len(items) > maxItems {
		items = items[:maxItems]
	}
	out := make([]string, 0, len(items))
	for _, s := range items {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if len(s) > maxLen {
			s = s[:maxLen]
		}
		out = append(out, s)
	}
	return out
}

func stripCodeFences(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	// Drop the opening fence line (e.g. ```json).
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		s = s[idx+1:]
	} else {
		s = strings.TrimPrefix(s, "```")
	}
	// Drop the trailing fence.
	if i := strings.LastIndex(s, "```"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func extractJSONObject(s string) string {
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end < 0 || end <= start {
		return ""
	}
	return s[start : end+1]
}
