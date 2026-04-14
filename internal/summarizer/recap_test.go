package summarizer

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseHappyPath(t *testing.T) {
	raw := `{
  "headline": "Refactored the reconstructor and added tests",
  "outcome": "success",
  "phases": [
    {"name": "explore", "start_span_index": 0, "end_span_index": 1, "summary": "looked at files"},
    {"name": "edit", "start_span_index": 2, "end_span_index": 4, "summary": "applied changes"}
  ],
  "key_steps": ["read reconstructor.go", "patched Hub field", "ran vet"],
  "failure_cause": null,
  "notable_events": []
}`
	r, err := Parse(raw, 5)
	require.NoError(t, err)
	require.Equal(t, OutcomeSuccess, r.Outcome)
	require.Len(t, r.Phases, 2)
	require.Nil(t, r.FailureCause)
	require.Len(t, r.KeySteps, 3)
}

func TestParseTolerantOfCodeFences(t *testing.T) {
	raw := "```json\n" + `{"headline":"x","outcome":"success","phases":[],"key_steps":["a","b","c"],"failure_cause":null,"notable_events":[]}` + "\n```"
	_, err := Parse(raw, 0)
	require.NoError(t, err)
}

func TestParseTolerantOfLeadingText(t *testing.T) {
	raw := `Sure, here you go:
{"headline":"x","outcome":"partial","phases":[],"key_steps":["a"],"failure_cause":"missing step","notable_events":[]}
Hope that helps.`
	r, err := Parse(raw, 0)
	require.NoError(t, err)
	require.Equal(t, OutcomePartial, r.Outcome)
	require.NotNil(t, r.FailureCause)
	require.Equal(t, "missing step", *r.FailureCause)
}

func TestParseRejectsInvalidOutcome(t *testing.T) {
	raw := `{"headline":"x","outcome":"winning","phases":[],"key_steps":[],"failure_cause":null,"notable_events":[]}`
	_, err := Parse(raw, 0)
	require.Error(t, err)
}

func TestParseRejectsOutOfRangePhase(t *testing.T) {
	raw := `{"headline":"x","outcome":"success","phases":[{"name":"edit","start_span_index":5,"end_span_index":10,"summary":"y"}],"key_steps":["a"],"failure_cause":null,"notable_events":[]}`
	_, err := Parse(raw, 3)
	require.Error(t, err)
}

func TestParseRejectsStartAfterEnd(t *testing.T) {
	raw := `{"headline":"x","outcome":"success","phases":[{"name":"edit","start_span_index":3,"end_span_index":1,"summary":"y"}],"key_steps":["a"],"failure_cause":null,"notable_events":[]}`
	_, err := Parse(raw, 5)
	require.Error(t, err)
}

func TestParseRejectsMissingHeadline(t *testing.T) {
	raw := `{"headline":"","outcome":"success","phases":[],"key_steps":["a"],"failure_cause":null,"notable_events":[]}`
	_, err := Parse(raw, 0)
	require.Error(t, err)
}

func TestParseTruncatesOverlongSlices(t *testing.T) {
	raw := `{
  "headline": "ok",
  "outcome": "success",
  "phases": [],
  "key_steps": ["1","2","3","4","5","6","7","8","9","10","11","12"],
  "failure_cause": null,
  "notable_events": ["a","b","c","d","e","f","g","h","i","j","k","l"]
}`
	r, err := Parse(raw, 0)
	require.NoError(t, err)
	require.Len(t, r.KeySteps, 10)
	require.Len(t, r.NotableEvents, 10)
}

func TestParseTruncatesLongHeadline(t *testing.T) {
	long := ""
	for i := 0; i < 300; i++ {
		long += "a"
	}
	raw := `{"headline":"` + long + `","outcome":"success","phases":[],"key_steps":["x"],"failure_cause":null,"notable_events":[]}`
	r, err := Parse(raw, 0)
	require.NoError(t, err)
	require.Len(t, r.Headline, 140)
}

func TestParseClearsFailureCauseOnSuccess(t *testing.T) {
	raw := `{"headline":"x","outcome":"success","phases":[],"key_steps":["x"],"failure_cause":"should be cleared","notable_events":[]}`
	r, err := Parse(raw, 0)
	require.NoError(t, err)
	require.Nil(t, r.FailureCause)
}
