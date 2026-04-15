package cli

import (
	"context"
	"fmt"

	"github.com/charmbracelet/huh"

	"github.com/BIwashi/apogee/internal/store/duckdb"
	"github.com/BIwashi/apogee/internal/summarizer"
)

// loadModelAvailability opens the DuckDB store at dbPath and returns the
// availability cache as a map[alias]bool. Any error (missing file,
// locked DB, missing table) yields nil so the wizard degrades to "every
// model assumed available" — the user can still pick an entry, and the
// worker re-probes on its own schedule.
func loadModelAvailability(ctx context.Context, dbPath string) map[string]bool {
	if dbPath == "" {
		return nil
	}
	store, err := duckdb.Open(ctx, dbPath)
	if err != nil {
		return nil
	}
	defer func() { _ = store.Close() }()
	cache, err := store.GetModelAvailability(ctx)
	if err != nil || len(cache) == 0 {
		return nil
	}
	out := make(map[string]bool, len(cache))
	for k, v := range cache {
		out[k] = v.Available
	}
	return out
}

// mergeModelCatalog returns a copy of catalog where each entry carries
// nothing beyond the static data — the function exists so the wizard
// has a stable point to swap in runtime-enriched fields later (e.g. a
// last-checked timestamp or a warning icon). For now it is a shallow
// clone so the caller can mutate the slice without poisoning the
// package-level KnownModels. availability is passed through
// unchanged — the resolver consumes it separately.
func mergeModelCatalog(catalog []summarizer.ModelInfo, _ map[string]bool) []summarizer.ModelInfo {
	out := make([]summarizer.ModelInfo, len(catalog))
	copy(out, catalog)
	return out
}

// modelOptions builds the huh.Option slice for one use-case dropdown.
// First entry is "Use default (<display of cheapest current>)" with
// value "" (empty string clears any persisted override). Subsequent
// entries are one per ModelsForUseCase result, formatted
// "{Display} — {status hint}".
//
// Unavailable models (availability[alias] == false) are filtered OUT
// entirely — huh has no per-option disable knob, so showing them as
// dead rows would only confuse the operator. Users who really want to
// pin a probed-unavailable model can still do so via the /v1/preferences
// PATCH endpoint.
func modelOptions(catalog []summarizer.ModelInfo, useCase summarizer.ModelUseCase, defaultAlias string, availability map[string]bool) []huh.Option[string] {
	defaultDisplay := defaultAlias
	for _, m := range catalog {
		if m.Alias == defaultAlias {
			defaultDisplay = m.Display
			break
		}
	}
	opts := []huh.Option[string]{
		huh.NewOption(fmt.Sprintf("Use default (%s)", defaultDisplay), ""),
	}
	for _, m := range summarizer.ModelsForUseCase(useCase) {
		if availability != nil {
			if avail, ok := availability[m.Alias]; ok && !avail {
				continue
			}
		}
		label := fmt.Sprintf("%s — %s", m.Display, statusHint(m))
		opts = append(opts, huh.NewOption(label, m.Alias))
	}
	return opts
}

// statusHint returns a short human-friendly descriptor for a model
// row: "current, cheapest" / "current" / "legacy". Used as the suffix
// on every modelOptions label.
func statusHint(m summarizer.ModelInfo) string {
	switch m.Status {
	case summarizer.StatusCurrent:
		if m.Tier == 0 {
			return "current, cheapest"
		}
		return "current"
	case summarizer.StatusLegacy:
		return "legacy"
	case summarizer.StatusDeprecated:
		return "deprecated"
	default:
		return m.Status
	}
}
