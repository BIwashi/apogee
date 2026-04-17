package summarizer

import (
	"strings"
	"testing"
)

func TestParseAgentSummary_Valid(t *testing.T) {
	raw := `{
  "title": "Investigating CI failures",
  "role": "Subagent assigned by main to triage red CI runs in the apogee repo and propose fixes.",
  "focus": ["go", "ci", "github-actions"]
}`
	got, err := ParseAgentSummary(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Title != "Investigating CI failures" {
		t.Errorf("title: got %q", got.Title)
	}
	if !strings.Contains(got.Role, "triage red CI") {
		t.Errorf("role: got %q", got.Role)
	}
	if len(got.Focus) != 3 {
		t.Errorf("focus: got %d items, want 3", len(got.Focus))
	}
}

func TestParseAgentSummary_StripsCodeFences(t *testing.T) {
	raw := "```json\n{\"title\":\"Refactoring storage layer\",\"role\":\"Owns the duckdb package\"}\n```"
	got, err := ParseAgentSummary(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Title != "Refactoring storage layer" {
		t.Errorf("title: got %q", got.Title)
	}
}

func TestParseAgentSummary_RejectsEmptyTitle(t *testing.T) {
	raw := `{"title": "", "role": "x"}`
	if _, err := ParseAgentSummary(raw); err == nil {
		t.Fatalf("expected error for empty title")
	}
}

func TestParseAgentSummary_TruncatesLongFields(t *testing.T) {
	long := strings.Repeat("a", 300)
	raw := `{"title":"` + long + `","role":"` + long + `"}`
	got, err := ParseAgentSummary(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Title) > 100 {
		t.Errorf("title not truncated: %d chars", len(got.Title))
	}
	if len(got.Role) > 240 {
		t.Errorf("role not truncated: %d chars", len(got.Role))
	}
}
