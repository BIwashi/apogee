package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/BIwashi/apogee/internal/summarizer"
)

func TestLoadOnboardState_PopulatesModels(t *testing.T) {
	h := newOnboardHarness(t)
	h.loadedPrefs = summarizer.Defaults()

	fillDefaults(&h.opts)
	state, err := loadOnboardState(context.Background(), h.opts)
	if err != nil {
		t.Fatalf("loadOnboardState: %v", err)
	}
	if len(state.Models) == 0 {
		t.Errorf("state.Models is empty — expected the full KnownModels slice")
	}
	if len(state.Models) != len(summarizer.KnownModels) {
		t.Errorf("state.Models has %d entries, want %d", len(state.Models), len(summarizer.KnownModels))
	}
	// With no DuckDB cache the resolver should fall through to the
	// static catalog and pick the cheapest current entries.
	if state.RecapDefault != "claude-haiku-4-5" {
		t.Errorf("state.RecapDefault = %q, want claude-haiku-4-5", state.RecapDefault)
	}
	if state.RollupDefault != "claude-sonnet-4-6" {
		t.Errorf("state.RollupDefault = %q, want claude-sonnet-4-6", state.RollupDefault)
	}
	if state.NarrativeDefault != "claude-sonnet-4-6" {
		t.Errorf("state.NarrativeDefault = %q, want claude-sonnet-4-6", state.NarrativeDefault)
	}
}

func TestModelOptions_FirstEntryIsUseDefault(t *testing.T) {
	opts := modelOptions(summarizer.KnownModels, summarizer.UseCaseRecap, "claude-haiku-4-5", nil)
	if len(opts) < 2 {
		t.Fatalf("expected at least 2 options, got %d", len(opts))
	}
	first := opts[0]
	if first.Value != "" {
		t.Errorf("first option value = %q, want empty string", first.Value)
	}
	if !strings.HasPrefix(first.Key, "Use default (") {
		t.Errorf("first option key = %q, want prefix 'Use default ('", first.Key)
	}
	if !strings.Contains(first.Key, "Haiku 4.5") {
		t.Errorf("first option key = %q, want to mention the Haiku 4.5 display name", first.Key)
	}
}

func TestModelOptions_IncludesOneEntryPerCurrentRecommended(t *testing.T) {
	opts := modelOptions(summarizer.KnownModels, summarizer.UseCaseRollup, "claude-sonnet-4-6", nil)
	// Expect: the "Use default" row + every ModelsForUseCase entry.
	want := 1 + len(summarizer.ModelsForUseCase(summarizer.UseCaseRollup))
	if len(opts) != want {
		t.Errorf("rollup option count = %d, want %d", len(opts), want)
	}
	sawSonnet := false
	sawOpus := false
	for _, o := range opts {
		if o.Value == "claude-sonnet-4-6" {
			sawSonnet = true
			if !strings.Contains(o.Key, "current") {
				t.Errorf("Sonnet 4.6 label missing 'current': %q", o.Key)
			}
		}
		if o.Value == "claude-opus-4-6" {
			sawOpus = true
		}
	}
	if !sawSonnet {
		t.Errorf("rollup options missing claude-sonnet-4-6")
	}
	if !sawOpus {
		t.Errorf("rollup options missing claude-opus-4-6")
	}
}

func TestModelOptions_FiltersUnavailable(t *testing.T) {
	avail := map[string]bool{
		"claude-sonnet-4-6": false, // mark unavailable
		"claude-opus-4-6":   true,
	}
	opts := modelOptions(summarizer.KnownModels, summarizer.UseCaseRollup, "claude-opus-4-6", avail)
	for _, o := range opts {
		if o.Value == "claude-sonnet-4-6" {
			t.Errorf("unavailable claude-sonnet-4-6 should be filtered out; got %q", o.Key)
		}
	}
}
