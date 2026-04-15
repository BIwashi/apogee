package collector

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/BIwashi/apogee/internal/store/duckdb"
	"github.com/BIwashi/apogee/internal/summarizer"
)

// modelAvailabilityTTL is how long a probed result is considered fresh
// before the /v1/models handler re-runs Probe. 24 hours mirrors the
// PruneStaleAvailability default the worker uses to drop old rows.
const modelAvailabilityTTL = 24 * time.Hour

// modelInfoResponse is the wire-encoded shape of a single catalog entry
// on /v1/models. Mirrors internal/summarizer.ModelInfo plus the probe
// fields.
type modelInfoResponse struct {
	Alias       string                    `json:"alias"`
	ShortAlias  string                    `json:"short_alias"`
	Family      string                    `json:"family"`
	Generation  string                    `json:"generation"`
	Display     string                    `json:"display"`
	Tier        int                       `json:"tier"`
	ContextK    int                       `json:"context_k"`
	Recommended []summarizer.ModelUseCase `json:"recommended"`
	Status      string                    `json:"status"`
	Available   bool                      `json:"available"`
	CheckedAt   *time.Time                `json:"checked_at"`
}

// modelsResponse is the full /v1/models response body.
type modelsResponse struct {
	Models      []modelInfoResponse        `json:"models"`
	Defaults    modelsResponseDefaults     `json:"defaults"`
	RefreshedAt time.Time                  `json:"refreshed_at"`
}

// modelsResponseDefaults is the resolver's default pick per summarizer
// tier, so the frontend can highlight the "use default" option without
// replicating ResolveDefaultModel in TypeScript.
type modelsResponseDefaults struct {
	Recap     string `json:"recap"`
	Rollup    string `json:"rollup"`
	Narrative string `json:"narrative"`
}

// modelsRefreshMu serialises concurrent probe refreshes. The probe is
// expensive (one CLI subprocess per current catalog entry) so we
// coalesce parallel /v1/models requests into a single refresh.
var modelsRefreshMu sync.Mutex

// listModels handles GET /v1/models. It:
//
//  1. Loads the model_availability cache from DuckDB
//  2. If the newest row is older than modelAvailabilityTTL (or the
//     cache is empty), runs summarizer.Probe to refresh it
//  3. Merges the cache with summarizer.KnownModels and computes the
//     resolver defaults
//
// When the claude CLI is not wired up (s.summarizer == nil or the
// runner errors on every entry) the handler still returns the full
// catalog with available=true on every row — "unknown = assume
// available" keeps the dropdown usable on a fresh install.
func (s *Server) listModels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cache, err := s.store.GetModelAvailability(ctx)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cacheIsStale(cache, modelAvailabilityTTL) {
		cache = s.refreshModelAvailability(ctx, cache)
	}
	body := buildModelsResponse(cache, time.Now().UTC())
	writeJSON(w, http.StatusOK, body)
}

// cacheIsStale reports whether the freshest row in cache is older than
// ttl (or the cache is empty).
func cacheIsStale(cache map[string]duckdb.ModelAvailability, ttl time.Duration) bool {
	if len(cache) == 0 {
		return true
	}
	var freshest time.Time
	for _, row := range cache {
		if row.CheckedAt.After(freshest) {
			freshest = row.CheckedAt
		}
	}
	return time.Since(freshest) > ttl
}

// refreshModelAvailability runs summarizer.Probe against the server's
// runner and writes the result back to the cache. Concurrent callers
// serialise on modelsRefreshMu so we never run more than one probe at
// a time. Returns the newly-loaded cache.
func (s *Server) refreshModelAvailability(ctx context.Context, prev map[string]duckdb.ModelAvailability) map[string]duckdb.ModelAvailability {
	modelsRefreshMu.Lock()
	defer modelsRefreshMu.Unlock()

	// Recheck the cache after grabbing the lock — another goroutine may
	// have refreshed while we were waiting.
	if fresh, err := s.store.GetModelAvailability(ctx); err == nil && !cacheIsStale(fresh, modelAvailabilityTTL) {
		return fresh
	}

	runner := s.modelProbeRunner()
	if runner == nil {
		// Can't probe → keep whatever's in the cache (may be empty).
		// The frontend still gets the catalog with "assume available".
		return prev
	}
	logger := s.logger
	if logger == nil {
		logger = slog.Default()
	}
	probed := summarizer.Probe(ctx, runner, logger)
	for alias, ok := range probed {
		lastErr := ""
		if !ok {
			lastErr = "probe returned error"
		}
		if err := s.store.UpsertModelAvailability(ctx, alias, ok, lastErr); err != nil {
			logger.Warn("model probe: persist failed", "alias", alias, "err", err)
		}
	}
	// Push the fresh snapshot to the workers so the next job's model
	// resolution uses the up-to-date availability.
	if s.summarizer != nil {
		s.summarizer.SetAvailability(probed)
	}
	out, err := s.store.GetModelAvailability(ctx)
	if err != nil {
		logger.Warn("model probe: reload failed", "err", err)
		return prev
	}
	return out
}

// modelProbeRunner returns the Runner used for model probes. It reuses
// the summarizer service's runner so the probe honours the same CLI
// configuration (binary path, timeout) the workers use. Returns nil
// when the summarizer is not wired up.
func (s *Server) modelProbeRunner() summarizer.Runner {
	if s == nil || s.summarizer == nil {
		return nil
	}
	return s.summarizer.Runner()
}

// buildModelsResponse merges the catalog with the availability cache
// and computes the resolver defaults. Pure function so tests can drive
// it without spinning up a Server.
func buildModelsResponse(cache map[string]duckdb.ModelAvailability, now time.Time) modelsResponse {
	avail := availabilityFromCache(cache)
	models := make([]modelInfoResponse, 0, len(summarizer.KnownModels))
	for _, m := range summarizer.KnownModels {
		row := modelInfoResponse{
			Alias:       m.Alias,
			ShortAlias:  m.ShortAlias,
			Family:      m.Family,
			Generation:  m.Generation,
			Display:     m.Display,
			Tier:        m.Tier,
			ContextK:    m.ContextK,
			Recommended: append([]summarizer.ModelUseCase{}, m.Recommended...),
			Status:      m.Status,
			Available:   true,
		}
		if cached, ok := cache[m.Alias]; ok {
			row.Available = cached.Available
			ts := cached.CheckedAt
			row.CheckedAt = &ts
		}
		models = append(models, row)
	}
	return modelsResponse{
		Models: models,
		Defaults: modelsResponseDefaults{
			Recap:     summarizer.ResolveDefaultModel(summarizer.UseCaseRecap, avail),
			Rollup:    summarizer.ResolveDefaultModel(summarizer.UseCaseRollup, avail),
			Narrative: summarizer.ResolveDefaultModel(summarizer.UseCaseNarrative, avail),
		},
		RefreshedAt: now.UTC(),
	}
}

// availabilityFromCache flattens the cache into the map shape the
// resolver expects. Missing entries in the cache are simply absent from
// the returned map — the resolver treats that as "assume available".
func availabilityFromCache(cache map[string]duckdb.ModelAvailability) map[string]bool {
	if len(cache) == 0 {
		return nil
	}
	out := make(map[string]bool, len(cache))
	for k, v := range cache {
		out[k] = v.Available
	}
	return out
}
