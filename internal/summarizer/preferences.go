package summarizer

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/BIwashi/apogee/internal/store/duckdb"
)

// Preference key constants — the canonical names persisted in the
// user_preferences DuckDB table. Kept here so the HTTP handler, the worker,
// and the web UI can refer to a single source of truth.
const (
	PrefKeyLanguage              = "summarizer.language"
	PrefKeyRecapSystemPrompt     = "summarizer.recap_system_prompt"
	PrefKeyRollupSystemPrompt    = "summarizer.rollup_system_prompt"
	PrefKeyNarrativeSystemPrompt = "summarizer.narrative_system_prompt"
	PrefKeyRecapModel            = "summarizer.recap_model"
	PrefKeyRollupModel           = "summarizer.rollup_model"
	PrefKeyNarrativeModel        = "summarizer.narrative_model"
)

// LanguageEN is the default summarizer output language.
const (
	LanguageEN = "en"
	LanguageJA = "ja"
)

// SystemPromptMaxLen caps the operator-supplied prompt slug to keep the
// instruction block from drowning out the schema rules. Mirrored in the
// PATCH validator.
const SystemPromptMaxLen = 2048

// Preferences is the typed view of every summarizer.* row in the
// user_preferences table at one point in time. Empty fields mean "fall back
// to the config / default".
type Preferences struct {
	Language               string
	RecapSystemPrompt      string
	RollupSystemPrompt     string
	NarrativeSystemPrompt  string
	RecapModelOverride     string
	RollupModelOverride    string
	NarrativeModelOverride string
}

// Defaults returns the zero-valued summarizer preferences. Language is "en";
// every other field is the empty string (= use the config fallback).
func Defaults() Preferences {
	return Preferences{Language: LanguageEN}
}

// PreferencesReader is the seam the worker depends on for hot-reloading
// operator-controlled prompt knobs at the start of every job. Tests pass a
// stub; production wires duckdbPreferencesReader.
type PreferencesReader interface {
	LoadSummarizerPreferences(ctx context.Context) (Preferences, error)
}

// staticPreferencesReader returns the same Preferences value every call.
// Useful for tests and as the fallback when the store is nil.
type staticPreferencesReader struct {
	prefs Preferences
}

// NewStaticPreferencesReader builds a reader that always returns prefs.
func NewStaticPreferencesReader(prefs Preferences) PreferencesReader {
	return &staticPreferencesReader{prefs: prefs}
}

// LoadSummarizerPreferences implements PreferencesReader.
func (r *staticPreferencesReader) LoadSummarizerPreferences(_ context.Context) (Preferences, error) {
	return r.prefs, nil
}

// duckdbPreferencesReader adapts a duckdb.Store into the PreferencesReader
// interface. It reads the canonical summarizer.* keys and tolerates missing
// rows by returning the default value for the absent field.
type duckdbPreferencesReader struct {
	store *duckdb.Store
}

// NewDuckDBPreferencesReader returns a reader that pulls from store.
func NewDuckDBPreferencesReader(store *duckdb.Store) PreferencesReader {
	return &duckdbPreferencesReader{store: store}
}

// LoadSummarizerPreferences implements PreferencesReader by querying each
// known key. Missing rows fall back to defaults; corrupt JSON logs at the
// caller and falls back too.
func (r *duckdbPreferencesReader) LoadSummarizerPreferences(ctx context.Context) (Preferences, error) {
	out := Defaults()
	if r == nil || r.store == nil {
		return out, nil
	}

	if v, ok, err := r.loadString(ctx, PrefKeyLanguage); err != nil {
		return out, err
	} else if ok {
		switch v {
		case LanguageEN, LanguageJA:
			out.Language = v
		default:
			// Unknown language — fall back to the default rather than
			// surfacing the bad value to the prompt builder.
			out.Language = LanguageEN
		}
	}
	if v, ok, err := r.loadString(ctx, PrefKeyRecapSystemPrompt); err != nil {
		return out, err
	} else if ok {
		out.RecapSystemPrompt = v
	}
	if v, ok, err := r.loadString(ctx, PrefKeyRollupSystemPrompt); err != nil {
		return out, err
	} else if ok {
		out.RollupSystemPrompt = v
	}
	if v, ok, err := r.loadString(ctx, PrefKeyNarrativeSystemPrompt); err != nil {
		return out, err
	} else if ok {
		out.NarrativeSystemPrompt = v
	}
	if v, ok, err := r.loadString(ctx, PrefKeyRecapModel); err != nil {
		return out, err
	} else if ok {
		out.RecapModelOverride = v
	}
	if v, ok, err := r.loadString(ctx, PrefKeyRollupModel); err != nil {
		return out, err
	} else if ok {
		out.RollupModelOverride = v
	}
	if v, ok, err := r.loadString(ctx, PrefKeyNarrativeModel); err != nil {
		return out, err
	} else if ok {
		out.NarrativeModelOverride = v
	}
	return out, nil
}

func (r *duckdbPreferencesReader) loadString(ctx context.Context, key string) (string, bool, error) {
	pref, ok, err := r.store.GetPreference(ctx, key)
	if err != nil {
		return "", false, fmt.Errorf("preferences: load %q: %w", key, err)
	}
	if !ok {
		return "", false, nil
	}
	var s string
	if err := json.Unmarshal(pref.Value, &s); err != nil {
		return "", false, nil
	}
	return s, true, nil
}
