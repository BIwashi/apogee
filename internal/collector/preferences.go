package collector

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/BIwashi/apogee/internal/summarizer"
)

// validateModelAlias returns nil when alias is either empty (clears the
// override) or present in the static summarizer catalog. An unknown
// alias surfaces a 400 with a pointer at the /v1/models endpoint so the
// operator can see the authoritative list.
func validateModelAlias(key, alias string) error {
	if alias == "" {
		return nil
	}
	if summarizer.FindModel(alias) == nil {
		return fmt.Errorf("%s: %q is not a known model alias; see GET /v1/models for the catalog", key, alias)
	}
	return nil
}

// summarizerPrefKeys is the canonical set of summarizer.* keys exposed by
// the /v1/preferences API. Listed in display order.
var summarizerPrefKeys = []string{
	summarizer.PrefKeyLanguage,
	summarizer.PrefKeyRecapSystemPrompt,
	summarizer.PrefKeyRollupSystemPrompt,
	summarizer.PrefKeyNarrativeSystemPrompt,
	summarizer.PrefKeyRecapModel,
	summarizer.PrefKeyRollupModel,
	summarizer.PrefKeyNarrativeModel,
}

// preferencesResponse is the wire shape returned by GET / PATCH
// /v1/preferences. preferences holds the merged view (defaults + persisted
// overrides), updated_at maps each present key to its store mtime so the UI
// can show "last saved" hints. Keys that have never been written are absent
// from updated_at but still present in preferences with their default
// value.
type preferencesResponse struct {
	Preferences map[string]any    `json:"preferences"`
	UpdatedAt   map[string]string `json:"updated_at"`
}

// preferencesPatch is the wire shape PATCH /v1/preferences accepts. Every
// field is a pointer so we can distinguish "not provided" from "set to the
// empty string" (the latter clears the override).
type preferencesPatch struct {
	Language              *string `json:"summarizer.language,omitempty"`
	RecapSystemPrompt     *string `json:"summarizer.recap_system_prompt,omitempty"`
	RollupSystemPrompt    *string `json:"summarizer.rollup_system_prompt,omitempty"`
	NarrativeSystemPrompt *string `json:"summarizer.narrative_system_prompt,omitempty"`
	RecapModel            *string `json:"summarizer.recap_model,omitempty"`
	RollupModel           *string `json:"summarizer.rollup_model,omitempty"`
	NarrativeModel        *string `json:"summarizer.narrative_model,omitempty"`
}

// listPreferences handles GET /v1/preferences. It loads every summarizer.*
// row, merges with the documented defaults, and returns the resulting
// view. Missing rows fall back to defaults silently — the response shape
// always includes every documented key.
func (s *Server) listPreferences(w http.ResponseWriter, r *http.Request) {
	body, err := s.buildPreferencesResponse(r)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, body)
}

// patchPreferences handles PATCH /v1/preferences. The body is a sparse
// merge — only the keys present in the request are updated. Validation
// errors short-circuit with a 400 and never write any key.
func (s *Server) patchPreferences(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var patch preferencesPatch
	if err := dec.Decode(&patch); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}

	updates, err := validatePreferencesPatch(patch)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(updates) == 0 {
		// PATCH with an empty body is an explicit no-op. Return the
		// current state so the client can use a single endpoint.
		body, err := s.buildPreferencesResponse(r)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, body)
		return
	}

	for _, u := range updates {
		if err := s.store.UpsertPreference(r.Context(), u.key, u.value); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	body, err := s.buildPreferencesResponse(r)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, body)
}

// deletePreferences handles DELETE /v1/preferences and removes every
// summarizer.* row. Used by the "reset to defaults" link on /settings.
func (s *Server) deletePreferences(w http.ResponseWriter, r *http.Request) {
	for _, k := range summarizerPrefKeys {
		if err := s.store.DeletePreference(r.Context(), k); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	body, err := s.buildPreferencesResponse(r)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, body)
}

// buildPreferencesResponse loads every summarizer.* row and merges with
// defaults. The defaults block is hard-coded so the response is stable
// even when no rows exist yet.
func (s *Server) buildPreferencesResponse(r *http.Request) (preferencesResponse, error) {
	out := preferencesResponse{
		Preferences: map[string]any{
			summarizer.PrefKeyLanguage:              summarizer.LanguageEN,
			summarizer.PrefKeyRecapSystemPrompt:     "",
			summarizer.PrefKeyRollupSystemPrompt:    "",
			summarizer.PrefKeyNarrativeSystemPrompt: "",
			summarizer.PrefKeyRecapModel:            "",
			summarizer.PrefKeyRollupModel:           "",
			summarizer.PrefKeyNarrativeModel:        "",
		},
		UpdatedAt: map[string]string{},
	}
	for _, k := range summarizerPrefKeys {
		pref, ok, err := s.store.GetPreference(r.Context(), k)
		if err != nil {
			return preferencesResponse{}, err
		}
		if !ok {
			continue
		}
		var v any
		if err := json.Unmarshal(pref.Value, &v); err != nil {
			// Corrupt row — skip and keep the default in place.
			continue
		}
		out.Preferences[k] = v
		out.UpdatedAt[k] = pref.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	return out, nil
}

// preferenceUpdate is one validated K/V update, ready to feed into
// store.UpsertPreference. Validation has already happened.
type preferenceUpdate struct {
	key   string
	value any
}

func validatePreferencesPatch(p preferencesPatch) ([]preferenceUpdate, error) {
	var updates []preferenceUpdate
	if p.Language != nil {
		v := strings.TrimSpace(*p.Language)
		if v != summarizer.LanguageEN && v != summarizer.LanguageJA {
			return nil, fmt.Errorf("summarizer.language: must be %q or %q", summarizer.LanguageEN, summarizer.LanguageJA)
		}
		updates = append(updates, preferenceUpdate{key: summarizer.PrefKeyLanguage, value: v})
	}
	if p.RecapSystemPrompt != nil {
		v := *p.RecapSystemPrompt
		if len(v) > summarizer.SystemPromptMaxLen {
			return nil, fmt.Errorf("summarizer.recap_system_prompt: %d chars exceeds %d", len(v), summarizer.SystemPromptMaxLen)
		}
		updates = append(updates, preferenceUpdate{key: summarizer.PrefKeyRecapSystemPrompt, value: v})
	}
	if p.RollupSystemPrompt != nil {
		v := *p.RollupSystemPrompt
		if len(v) > summarizer.SystemPromptMaxLen {
			return nil, fmt.Errorf("summarizer.rollup_system_prompt: %d chars exceeds %d", len(v), summarizer.SystemPromptMaxLen)
		}
		updates = append(updates, preferenceUpdate{key: summarizer.PrefKeyRollupSystemPrompt, value: v})
	}
	if p.NarrativeSystemPrompt != nil {
		v := *p.NarrativeSystemPrompt
		if len(v) > summarizer.SystemPromptMaxLen {
			return nil, fmt.Errorf("summarizer.narrative_system_prompt: %d chars exceeds %d", len(v), summarizer.SystemPromptMaxLen)
		}
		updates = append(updates, preferenceUpdate{key: summarizer.PrefKeyNarrativeSystemPrompt, value: v})
	}
	if p.RecapModel != nil {
		v := strings.TrimSpace(*p.RecapModel)
		if err := validateModelAlias("summarizer.recap_model", v); err != nil {
			return nil, err
		}
		updates = append(updates, preferenceUpdate{key: summarizer.PrefKeyRecapModel, value: v})
	}
	if p.RollupModel != nil {
		v := strings.TrimSpace(*p.RollupModel)
		if err := validateModelAlias("summarizer.rollup_model", v); err != nil {
			return nil, err
		}
		updates = append(updates, preferenceUpdate{key: summarizer.PrefKeyRollupModel, value: v})
	}
	if p.NarrativeModel != nil {
		v := strings.TrimSpace(*p.NarrativeModel)
		if err := validateModelAlias("summarizer.narrative_model", v); err != nil {
			return nil, err
		}
		updates = append(updates, preferenceUpdate{key: summarizer.PrefKeyNarrativeModel, value: v})
	}
	return updates, nil
}
