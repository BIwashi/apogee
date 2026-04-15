package summarizer

import (
	"testing"
)

func TestFindModel(t *testing.T) {
	// Every declared entry must be resolvable by Alias.
	for _, m := range KnownModels {
		got := FindModel(m.Alias)
		if got == nil {
			t.Errorf("FindModel(%q) = nil, want hit", m.Alias)
			continue
		}
		if got.Alias != m.Alias {
			t.Errorf("FindModel(%q).Alias = %q, want %q", m.Alias, got.Alias, m.Alias)
		}
	}

	// Unknown alias → nil.
	if got := FindModel("gpt-4"); got != nil {
		t.Errorf("FindModel(gpt-4) = %+v, want nil", got)
	}
	if got := FindModel(""); got != nil {
		t.Errorf("FindModel('') = %+v, want nil", got)
	}
}

func TestModelsForUseCase_RecapReturnsHaikuFirst(t *testing.T) {
	recap := ModelsForUseCase(UseCaseRecap)
	if len(recap) < 2 {
		t.Fatalf("expected at least 2 recap candidates, got %d", len(recap))
	}
	if recap[0].Alias != "claude-haiku-4-5" {
		t.Errorf("first recap candidate = %q, want claude-haiku-4-5 (cheapest current)", recap[0].Alias)
	}
	// Legacy (Haiku 3.5) must come after the current Sonnet 4.6 because
	// current outranks legacy regardless of tier.
	for i, m := range recap {
		if m.Status == StatusLegacy {
			// Ensure every preceding element is current.
			for _, prev := range recap[:i] {
				if prev.Status != StatusCurrent {
					t.Errorf("legacy %q preceded by non-current %q", m.Alias, prev.Alias)
				}
			}
			break
		}
	}
}

func TestModelsForUseCase_RollupIncludesOpus(t *testing.T) {
	rollup := ModelsForUseCase(UseCaseRollup)
	foundOpus := false
	for _, m := range rollup {
		if m.Alias == "claude-opus-4-6" {
			foundOpus = true
		}
	}
	if !foundOpus {
		t.Errorf("rollup candidates missing claude-opus-4-6; got %+v", aliasesOf(rollup))
	}
	// First candidate must be current + cheapest: Sonnet 4.6 beats Opus
	// 4.6 on tier.
	if rollup[0].Alias != "claude-sonnet-4-6" {
		t.Errorf("first rollup candidate = %q, want claude-sonnet-4-6", rollup[0].Alias)
	}
}

func TestResolveDefaultModel_AllAvailable(t *testing.T) {
	avail := map[string]bool{
		"claude-haiku-4-5":  true,
		"claude-sonnet-4-6": true,
		"claude-opus-4-6":   true,
	}
	if got := ResolveDefaultModel(UseCaseRecap, avail); got != "claude-haiku-4-5" {
		t.Errorf("recap default = %q, want claude-haiku-4-5", got)
	}
	if got := ResolveDefaultModel(UseCaseRollup, avail); got != "claude-sonnet-4-6" {
		t.Errorf("rollup default = %q, want claude-sonnet-4-6", got)
	}
	if got := ResolveDefaultModel(UseCaseNarrative, avail); got != "claude-sonnet-4-6" {
		t.Errorf("narrative default = %q, want claude-sonnet-4-6", got)
	}
}

func TestResolveDefaultModel_CurrentUnavailable_FallsBackToLegacy(t *testing.T) {
	avail := map[string]bool{
		"claude-haiku-4-5":  false,
		"claude-sonnet-4-6": false,
		"claude-opus-4-6":   false,
		// legacy unlisted — treated as available.
	}
	got := ResolveDefaultModel(UseCaseRecap, avail)
	if got != "claude-haiku-3-5" {
		t.Errorf("recap fallback = %q, want claude-haiku-3-5", got)
	}
	got = ResolveDefaultModel(UseCaseRollup, avail)
	if got != "claude-sonnet-3-7" {
		t.Errorf("rollup fallback = %q, want claude-sonnet-3-7", got)
	}
}

func TestResolveDefaultModel_NothingAvailable_StillReturnsCatalogFirst(t *testing.T) {
	avail := map[string]bool{}
	for _, m := range KnownModels {
		avail[m.Alias] = false
	}
	got := ResolveDefaultModel(UseCaseRecap, avail)
	if got == "" {
		t.Errorf("recap resolver returned empty string despite non-empty catalog")
	}
	// With every alias explicitly unavailable the resolver walks all
	// passes and falls through to the first candidate (current+Recap).
	if got != "claude-haiku-4-5" {
		t.Errorf("catalog-first fallback = %q, want claude-haiku-4-5", got)
	}
}

func TestResolveDefaultModel_UnknownUseCase(t *testing.T) {
	got := ResolveDefaultModel(ModelUseCase("nope"), nil)
	if got != "" {
		t.Errorf("unknown use case should return empty string, got %q", got)
	}
}

func TestResolveDefaultModel_NilAvailabilityMeansAssumeAvailable(t *testing.T) {
	got := ResolveDefaultModel(UseCaseRecap, nil)
	if got != "claude-haiku-4-5" {
		t.Errorf("nil availability default = %q, want claude-haiku-4-5", got)
	}
}

func TestResolveModelForUseCase_PreferenceBeatsConfig(t *testing.T) {
	got := ResolveModelForUseCase(UseCaseRecap, " claude-sonnet-4-6 ", "claude-opus-4-6", nil)
	if got != "claude-sonnet-4-6" {
		t.Errorf("preference override = %q, want claude-sonnet-4-6 (trimmed)", got)
	}
}

func TestResolveModelForUseCase_ConfigBeatsCatalog(t *testing.T) {
	got := ResolveModelForUseCase(UseCaseRecap, "", "claude-opus-4-6", nil)
	if got != "claude-opus-4-6" {
		t.Errorf("config override = %q, want claude-opus-4-6", got)
	}
}

func TestResolveModelForUseCase_CatalogFallback(t *testing.T) {
	got := ResolveModelForUseCase(UseCaseRecap, "", "", nil)
	if got != "claude-haiku-4-5" {
		t.Errorf("catalog fallback = %q, want claude-haiku-4-5", got)
	}
}

// aliasesOf is a tiny helper so test failures print the candidate order
// in a readable way.
func aliasesOf(in []ModelInfo) []string {
	out := make([]string, 0, len(in))
	for _, m := range in {
		out = append(out, m.Alias)
	}
	return out
}
