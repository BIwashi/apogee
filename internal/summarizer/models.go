package summarizer

// ModelUseCase tags which summarizer tier a model is recommended for.
// A single model can carry multiple use cases; the resolver picks the
// cheapest entry whose Recommended list contains the requested use case.
type ModelUseCase string

// Recognised summarizer tiers. Extend as new tiers land.
const (
	UseCaseRecap      ModelUseCase = "recap"
	UseCaseRollup     ModelUseCase = "rollup"
	UseCaseNarrative  ModelUseCase = "narrative"
	UseCaseLiveStatus ModelUseCase = "live_status"
)

// Status values for a catalog entry. Ordering matters: the resolver
// prefers StatusCurrent over StatusLegacy over StatusDeprecated.
const (
	StatusCurrent    = "current"
	StatusLegacy     = "legacy"
	StatusDeprecated = "deprecated"
)

// ModelInfo is a single Claude model apogee knows about.
type ModelInfo struct {
	// Alias is the full wire name sent to the claude CLI. This is the
	// stable identifier everywhere in the codebase.
	Alias string
	// ShortAlias is the `--model <short>` shortcut the claude CLI also
	// accepts (e.g. "haiku"). Empty when the model has no shortcut.
	ShortAlias string
	// Family is "haiku" | "sonnet" | "opus" | future.
	Family string
	// Generation encodes Anthropic's version label, e.g. "4-5" or "4-6".
	Generation string
	// Display is a short human-friendly name, e.g. "Haiku 4.5".
	Display string
	// Tier orders models by cost-per-token. 0 = cheapest; higher = pricier.
	// Used by ResolveDefaultModel to walk candidates from cheapest to
	// most expensive.
	Tier int
	// ContextK is the advertised context window in thousands of tokens.
	// Informational only.
	ContextK int
	// Recommended lists the summarizer tiers this model is a good
	// default for. A model can appear in multiple use cases.
	Recommended []ModelUseCase
	// Status is StatusCurrent | StatusLegacy | StatusDeprecated.
	Status string
}

// KnownModels is the authoritative static catalog. Curated — not
// scraped. When Anthropic ships a new model, add an entry here and
// ship a new apogee release. The dynamic probe
// (see models_probe.go) is allowed to REMOVE entries from the
// active list when `claude -p --model <alias>` fails, but it never
// ADDS new ones.
//
// Ordering is significant: ResolveDefaultModel walks this slice in
// declaration order, so keep "current" families near the top in
// cheapest → most expensive order.
var KnownModels = []ModelInfo{
	{
		Alias:       "claude-haiku-4-5",
		ShortAlias:  "haiku",
		Family:      "haiku",
		Generation:  "4-5",
		Display:     "Haiku 4.5",
		Tier:        0,
		ContextK:    200,
		Recommended: []ModelUseCase{UseCaseRecap, UseCaseLiveStatus},
		Status:      StatusCurrent,
	},
	{
		Alias:       "claude-sonnet-4-6",
		ShortAlias:  "sonnet",
		Family:      "sonnet",
		Generation:  "4-6",
		Display:     "Sonnet 4.6",
		Tier:        1,
		ContextK:    200,
		Recommended: []ModelUseCase{UseCaseRecap, UseCaseRollup, UseCaseNarrative},
		Status:      StatusCurrent,
	},
	{
		Alias:       "claude-opus-4-6",
		ShortAlias:  "opus",
		Family:      "opus",
		Generation:  "4-6",
		Display:     "Opus 4.6",
		Tier:        2,
		ContextK:    200,
		Recommended: []ModelUseCase{UseCaseRollup, UseCaseNarrative},
		Status:      StatusCurrent,
	},
	// Legacy still-available fallbacks. Kept in the catalog so users
	// can explicitly pin them when a current model is offline; never
	// chosen as a default when a "current" entry is available.
	{
		Alias:       "claude-haiku-3-5",
		Family:      "haiku",
		Generation:  "3-5",
		Display:     "Haiku 3.5",
		Tier:        0,
		ContextK:    200,
		Recommended: []ModelUseCase{UseCaseRecap, UseCaseLiveStatus},
		Status:      StatusLegacy,
	},
	{
		Alias:       "claude-sonnet-3-7",
		Family:      "sonnet",
		Generation:  "3-7",
		Display:     "Sonnet 3.7",
		Tier:        1,
		ContextK:    200,
		Recommended: []ModelUseCase{UseCaseRollup, UseCaseNarrative},
		Status:      StatusLegacy,
	},
}

// FindModel returns the catalog entry for alias, or nil when unknown.
// Lookup is O(n) by design — the catalog is small and curated.
func FindModel(alias string) *ModelInfo {
	if alias == "" {
		return nil
	}
	for i := range KnownModels {
		if KnownModels[i].Alias == alias {
			return &KnownModels[i]
		}
	}
	return nil
}

// containsUseCase reports whether needle appears in haystack.
func containsUseCase(haystack []ModelUseCase, needle ModelUseCase) bool {
	for _, u := range haystack {
		if u == needle {
			return true
		}
	}
	return false
}

// statusRank returns the resolver priority for a status string.
// Lower is better. Unknown statuses sort after deprecated so a typo
// never beats a real entry.
func statusRank(s string) int {
	switch s {
	case StatusCurrent:
		return 0
	case StatusLegacy:
		return 1
	case StatusDeprecated:
		return 2
	default:
		return 3
	}
}

// ModelsForUseCase returns every KnownModel whose Recommended list
// contains useCase, ordered by (statusRank asc, tier asc, declaration
// order). Declaration order is the tiebreaker so the catalog author
// has final say.
func ModelsForUseCase(useCase ModelUseCase) []ModelInfo {
	out := make([]ModelInfo, 0, len(KnownModels))
	for _, m := range KnownModels {
		if containsUseCase(m.Recommended, useCase) {
			out = append(out, m)
		}
	}
	// Simple insertion sort — the catalog is tiny so the code stays
	// obvious instead of pulling in sort.SliceStable.
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 {
			a := out[j-1]
			b := out[j]
			ra := statusRank(a.Status)
			rb := statusRank(b.Status)
			if ra < rb {
				break
			}
			if ra == rb && a.Tier <= b.Tier {
				break
			}
			out[j-1], out[j] = out[j], out[j-1]
			j--
		}
	}
	return out
}

// ResolveDefaultModel returns the best default alias for useCase given
// the current availability map. The availability map is keyed by Alias;
// a missing key means "not probed yet, treat as available". An explicit
// false means "probe failed, skip".
//
// Walks ModelsForUseCase(useCase), picking the first entry that:
//  1. Is StatusCurrent (fallback to legacy if no current matches)
//  2. Is available in the availability map (or unknown)
//
// Never returns an empty string when the catalog is non-empty — it
// falls back to the first ModelsForUseCase entry if every check fails.
func ResolveDefaultModel(useCase ModelUseCase, availability map[string]bool) string {
	candidates := ModelsForUseCase(useCase)
	if len(candidates) == 0 {
		return ""
	}
	isAvailable := func(m ModelInfo) bool {
		if availability == nil {
			return true
		}
		v, ok := availability[m.Alias]
		if !ok {
			// Unknown = assume available. The probe has not (yet)
			// reported a result, so we give the entry the benefit of
			// the doubt.
			return true
		}
		return v
	}
	// Pass 1: current + available.
	for _, m := range candidates {
		if m.Status == StatusCurrent && isAvailable(m) {
			return m.Alias
		}
	}
	// Pass 2: legacy + available.
	for _, m := range candidates {
		if m.Status == StatusLegacy && isAvailable(m) {
			return m.Alias
		}
	}
	// Pass 3: any status + available (covers deprecated or unknown).
	for _, m := range candidates {
		if isAvailable(m) {
			return m.Alias
		}
	}
	// Pass 4: nothing available — fall back to the first candidate so
	// callers always get a non-empty alias when the catalog is non-empty.
	return candidates[0].Alias
}

// ResolveModelForUseCase wraps the preference + config + resolver chain
// used by every summarizer worker. Order:
//
//  1. preferenceOverride — operator-controlled, trimmed
//  2. configOverride     — TOML / env-controlled, trimmed
//  3. ResolveDefaultModel(useCase, availability) — catalog default
//
// Never returns an empty string when the catalog is non-empty.
func ResolveModelForUseCase(useCase ModelUseCase, preferenceOverride, configOverride string, availability map[string]bool) string {
	if v := trimSpace(preferenceOverride); v != "" {
		return v
	}
	if v := trimSpace(configOverride); v != "" {
		return v
	}
	return ResolveDefaultModel(useCase, availability)
}

// trimSpace is a tiny wrapper so we don't need to import strings from
// every caller of ResolveModelForUseCase. Package models.go stays
// import-light on purpose.
func trimSpace(s string) string {
	// Manual trim — avoids an import cycle with strings elsewhere if
	// the catalog is ever referenced from a lower-level package.
	start := 0
	end := len(s)
	for start < end && isSpace(s[start]) {
		start++
	}
	for end > start && isSpace(s[end-1]) {
		end--
	}
	return s[start:end]
}

func isSpace(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return false
}
