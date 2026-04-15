package duckdb

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestModelAvailabilityRoundTrip(t *testing.T) {
	ctx := t.Context()
	s := newTestStore(t)

	// Empty cache returns an empty (non-nil) map.
	got, err := s.GetModelAvailability(ctx)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Empty(t, got)

	require.NoError(t, s.UpsertModelAvailability(ctx, "claude-haiku-4-5", true, ""))
	require.NoError(t, s.UpsertModelAvailability(ctx, "claude-opus-4-6", false, "model unavailable"))

	got, err = s.GetModelAvailability(ctx)
	require.NoError(t, err)
	require.Len(t, got, 2)

	haiku, ok := got["claude-haiku-4-5"]
	require.True(t, ok)
	require.True(t, haiku.Available)
	require.Empty(t, haiku.LastError)
	require.False(t, haiku.CheckedAt.IsZero())

	opus, ok := got["claude-opus-4-6"]
	require.True(t, ok)
	require.False(t, opus.Available)
	require.Equal(t, "model unavailable", opus.LastError)

	// Upsert overwrites in place.
	require.NoError(t, s.UpsertModelAvailability(ctx, "claude-opus-4-6", true, ""))
	got, err = s.GetModelAvailability(ctx)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.True(t, got["claude-opus-4-6"].Available)
	require.Empty(t, got["claude-opus-4-6"].LastError)
}

func TestModelAvailabilityPruneStale(t *testing.T) {
	ctx := t.Context()
	s := newTestStore(t)

	require.NoError(t, s.UpsertModelAvailability(ctx, "claude-haiku-4-5", true, ""))
	require.NoError(t, s.UpsertModelAvailability(ctx, "claude-sonnet-4-6", true, ""))

	// Forcibly backdate one row 48h so it trips the prune threshold.
	_, err := s.db.ExecContext(ctx, `UPDATE model_availability SET checked_at = ? WHERE alias = ?`,
		time.Now().UTC().Add(-48*time.Hour), "claude-haiku-4-5")
	require.NoError(t, err)

	n, err := s.PruneStaleAvailability(ctx, time.Now().UTC().Add(-24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	got, err := s.GetModelAvailability(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1)
	_, ok := got["claude-haiku-4-5"]
	require.False(t, ok, "stale row should have been pruned")
	_, ok = got["claude-sonnet-4-6"]
	require.True(t, ok)
}

func TestModelAvailabilityEmptyAlias(t *testing.T) {
	ctx := t.Context()
	s := newTestStore(t)
	err := s.UpsertModelAvailability(ctx, "", true, "")
	require.Error(t, err)
}
