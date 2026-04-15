package collector

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/BIwashi/apogee/internal/store/duckdb"
	"github.com/BIwashi/apogee/internal/summarizer"
)

func TestGetModels_DefaultsResolvedFromCatalog(t *testing.T) {
	// Seed a fresh availability cache so the stale check short-circuits
	// and the real CLIRunner never runs — the test machine has no
	// `claude` binary on PATH.
	srv, ts := newTestServer(t)
	ctx := t.Context()
	for _, m := range summarizer.KnownModels {
		if m.Status == summarizer.StatusCurrent {
			require.NoError(t, srv.store.UpsertModelAvailability(ctx, m.Alias, true, ""))
		}
	}

	resp, err := http.Get(ts.URL + "/v1/models")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var body modelsResponse
	require.NoError(t, json.Unmarshal(raw, &body))

	// The static catalog should be fully reflected in the response.
	require.Len(t, body.Models, len(summarizer.KnownModels))
	// Defaults match the static catalog order: cheapest current wins.
	require.Equal(t, "claude-haiku-4-5", body.Defaults.Recap)
	require.Equal(t, "claude-sonnet-4-6", body.Defaults.Rollup)
	require.Equal(t, "claude-sonnet-4-6", body.Defaults.Narrative)
	require.False(t, body.RefreshedAt.IsZero())
	// Every current entry in the seeded cache should carry
	// available=true and a non-nil checked_at timestamp.
	seenCurrent := 0
	for _, m := range body.Models {
		if m.Status != "current" {
			continue
		}
		seenCurrent++
		require.True(t, m.Available, "seeded current alias %q should remain available", m.Alias)
		require.NotNil(t, m.CheckedAt)
	}
	require.Greater(t, seenCurrent, 0)
}

func TestGetModels_HonorsSeededAvailabilityCache(t *testing.T) {
	srv, ts := newTestServer(t)
	ctx := t.Context()
	// Seed a fresh cache row marking Haiku as unavailable. The resolver
	// should fall back to the next-cheapest current candidate for recap
	// (Sonnet 4.6).
	require.NoError(t, srv.store.UpsertModelAvailability(ctx, "claude-haiku-4-5", false, "seeded"))
	// Also mark Sonnet/Opus explicitly available so the freshness check
	// doesn't look stale.
	require.NoError(t, srv.store.UpsertModelAvailability(ctx, "claude-sonnet-4-6", true, ""))
	require.NoError(t, srv.store.UpsertModelAvailability(ctx, "claude-opus-4-6", true, ""))

	resp, err := http.Get(ts.URL + "/v1/models")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body modelsResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, "claude-sonnet-4-6", body.Defaults.Recap, "recap should skip the unavailable Haiku")

	// The row for Haiku should carry available=false and a non-nil
	// checked_at timestamp.
	for _, m := range body.Models {
		if m.Alias == "claude-haiku-4-5" {
			require.False(t, m.Available)
			require.NotNil(t, m.CheckedAt)
		}
	}
}

func TestCacheIsStale(t *testing.T) {
	ttl := 24 * time.Hour
	require.True(t, cacheIsStale(nil, ttl))
	require.True(t, cacheIsStale(map[string]duckdb.ModelAvailability{}, ttl))

	now := time.Now().UTC()
	fresh := map[string]duckdb.ModelAvailability{
		"claude-haiku-4-5": {Alias: "claude-haiku-4-5", Available: true, CheckedAt: now},
	}
	require.False(t, cacheIsStale(fresh, ttl))

	stale := map[string]duckdb.ModelAvailability{
		"claude-haiku-4-5": {Alias: "claude-haiku-4-5", Available: true, CheckedAt: now.Add(-48 * time.Hour)},
	}
	require.True(t, cacheIsStale(stale, ttl))
}

func TestBuildModelsResponse_AvailabilityFallback(t *testing.T) {
	// Build a cache where every current model is unavailable. The
	// resolver should walk the status passes and pick the first
	// available legacy candidate.
	cache := map[string]duckdb.ModelAvailability{}
	now := time.Now().UTC()
	for _, m := range summarizer.KnownModels {
		if m.Status == summarizer.StatusCurrent {
			cache[m.Alias] = duckdb.ModelAvailability{
				Alias:     m.Alias,
				Available: false,
				CheckedAt: now,
				LastError: "probe failed",
			}
		}
	}
	resp := buildModelsResponse(cache, now)
	require.Equal(t, "claude-haiku-3-5", resp.Defaults.Recap, "should fall back to legacy haiku")
	require.Equal(t, "claude-sonnet-3-7", resp.Defaults.Rollup, "should fall back to legacy sonnet")
}
