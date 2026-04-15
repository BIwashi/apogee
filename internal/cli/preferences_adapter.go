package cli

import (
	"context"
	"fmt"

	"github.com/BIwashi/apogee/internal/store/duckdb"
	"github.com/BIwashi/apogee/internal/summarizer"
)

// preferencesWriter is the narrow interface the onboard wizard uses to
// persist summarizer preferences. Production code wires it to a real
// duckdb.Store; tests can inject a fake that captures calls without
// touching a DuckDB file.
//
// The interface intentionally mirrors the shape of
// duckdb.Store.UpsertPreference so the adapter can be a one-line
// forward in production.
type preferencesWriter interface {
	UpsertPreference(ctx context.Context, key string, value any) error
}

// loadSummarizerPreferencesFromDB returns the current summarizer
// preferences from the DuckDB file at dbPath. A missing file is not
// an error — the function returns summarizer.Defaults() so the
// onboard wizard can still pre-fill sensible defaults.
//
// The store is opened read-only-ish (DuckDB has no real read-only
// mode for attached databases but we Close immediately after loading
// so the sidecar lock does not block a concurrent daemon).
func loadSummarizerPreferencesFromDB(ctx context.Context, dbPath string) (summarizer.Preferences, error) {
	if dbPath == "" {
		return summarizer.Defaults(), nil
	}
	store, err := duckdb.Open(ctx, dbPath)
	if err != nil {
		// A locked DB (daemon already running) or a missing file are
		// both non-fatal for the onboard flow — fall back to defaults
		// rather than crashing the wizard.
		return summarizer.Defaults(), nil
	}
	defer func() { _ = store.Close() }()
	reader := summarizer.NewDuckDBPreferencesReader(store)
	prefs, err := reader.LoadSummarizerPreferences(ctx)
	if err != nil {
		return summarizer.Defaults(), fmt.Errorf("onboard: read summarizer prefs: %w", err)
	}
	return prefs, nil
}

// writeSummarizerPreferencesToDB opens the DuckDB store at dbPath and
// writes every non-skipped summarizer preference. Empty strings on
// optional fields are skipped so the onboard flow never overwrites a
// user-edited prompt with an empty default.
func writeSummarizerPreferencesToDB(ctx context.Context, dbPath string, prefs summarizer.Preferences) error {
	if dbPath == "" {
		return nil
	}
	store, err := duckdb.Open(ctx, dbPath)
	if err != nil {
		return fmt.Errorf("onboard: open duckdb at %s: %w", dbPath, err)
	}
	defer func() { _ = store.Close() }()
	return writeSummarizerPreferences(ctx, store, prefs)
}

// writeSummarizerPreferences writes prefs via the supplied writer.
// Extracted so tests can inject a fake writer and assert on the
// exact set of keys+values emitted.
func writeSummarizerPreferences(ctx context.Context, w preferencesWriter, prefs summarizer.Preferences) error {
	// Language is always written when non-empty — it has a hard
	// default (en) so the user always gets a deterministic row.
	if prefs.Language != "" {
		if err := w.UpsertPreference(ctx, summarizer.PrefKeyLanguage, prefs.Language); err != nil {
			return err
		}
	}
	if prefs.RecapSystemPrompt != "" {
		if err := w.UpsertPreference(ctx, summarizer.PrefKeyRecapSystemPrompt, prefs.RecapSystemPrompt); err != nil {
			return err
		}
	}
	if prefs.RollupSystemPrompt != "" {
		if err := w.UpsertPreference(ctx, summarizer.PrefKeyRollupSystemPrompt, prefs.RollupSystemPrompt); err != nil {
			return err
		}
	}
	if prefs.NarrativeSystemPrompt != "" {
		if err := w.UpsertPreference(ctx, summarizer.PrefKeyNarrativeSystemPrompt, prefs.NarrativeSystemPrompt); err != nil {
			return err
		}
	}
	if prefs.RecapModelOverride != "" {
		if err := w.UpsertPreference(ctx, summarizer.PrefKeyRecapModel, prefs.RecapModelOverride); err != nil {
			return err
		}
	}
	if prefs.RollupModelOverride != "" {
		if err := w.UpsertPreference(ctx, summarizer.PrefKeyRollupModel, prefs.RollupModelOverride); err != nil {
			return err
		}
	}
	if prefs.NarrativeModelOverride != "" {
		if err := w.UpsertPreference(ctx, summarizer.PrefKeyNarrativeModel, prefs.NarrativeModelOverride); err != nil {
			return err
		}
	}
	return nil
}
