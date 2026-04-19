package summarizer

import (
	"strings"
	"testing"
)

func TestParse_TopicDecisionParsedAndClamped(t *testing.T) {
	raw := `{
  "headline": "Refactored auth middleware",
  "outcome": "success",
  "phases": [],
  "key_steps": ["edit", "test"],
  "failure_cause": null,
  "notable_events": [],
  "topic_decision": {
    "kind": "resume",
    "target_topic_ref": "recent:1",
    "confidence": 1.7,
    "goal": "  return to docs work  ",
    "reason": "user said back to docs"
  }
}`
	got, err := Parse(raw, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TopicDecision == nil {
		t.Fatalf("expected TopicDecision to be parsed")
	}
	td := got.TopicDecision
	if td.Kind != TopicKindResume {
		t.Errorf("kind: got %q", td.Kind)
	}
	if td.TargetTopicRef != "recent:1" {
		t.Errorf("target_topic_ref: got %q", td.TargetTopicRef)
	}
	if td.Confidence != 1.0 {
		t.Errorf("confidence not clamped to 1.0: got %v", td.Confidence)
	}
	if td.Goal != "return to docs work" {
		t.Errorf("goal not trimmed: %q", td.Goal)
	}
	if !strings.Contains(td.Reason, "back to docs") {
		t.Errorf("reason not preserved: %q", td.Reason)
	}
}

func TestParse_TopicDecisionInvalidKindDropped(t *testing.T) {
	raw := `{
  "headline": "Did a thing",
  "outcome": "success",
  "phases": [],
  "key_steps": ["one"],
  "failure_cause": null,
  "notable_events": [],
  "topic_decision": { "kind": "fork", "confidence": 0.9 }
}`
	got, err := Parse(raw, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TopicDecision != nil {
		t.Fatalf("expected invalid kind to drop the decision, got %+v", got.TopicDecision)
	}
}

func TestParse_NoTopicDecisionStillValid(t *testing.T) {
	raw := `{
  "headline": "Existing recap shape",
  "outcome": "success",
  "phases": [],
  "key_steps": ["one"],
  "failure_cause": null,
  "notable_events": []
}`
	got, err := Parse(raw, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TopicDecision != nil {
		t.Fatalf("did not expect a topic decision")
	}
}

func TestParseRecentRef(t *testing.T) {
	cases := []struct {
		in  string
		idx int
		ok  bool
	}{
		{"recent:0", 0, true},
		{"recent:3", 3, true},
		{"3", 3, true},
		{" recent:2 ", 2, true},
		{"", 0, false},
		{"foo", 0, false},
		{"recent:abc", 0, false},
	}
	for _, c := range cases {
		idx, ok := parseRecentRef(c.in)
		if ok != c.ok || idx != c.idx {
			t.Errorf("parseRecentRef(%q) = (%d, %v), want (%d, %v)", c.in, idx, ok, c.idx, c.ok)
		}
	}
}

func TestTopicKindIsValid(t *testing.T) {
	for _, k := range []TopicKind{TopicKindNew, TopicKindContinue, TopicKindResume, TopicKindUnknown} {
		if !k.IsValid() {
			t.Errorf("kind %q should be valid", k)
		}
	}
	if TopicKind("nonsense").IsValid() {
		t.Errorf("nonsense kind should be invalid")
	}
}
