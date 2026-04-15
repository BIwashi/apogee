package duckdb

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPreferencesRoundTrip(t *testing.T) {
	ctx := t.Context()
	s := newTestStore(t)

	// Missing key returns (_, false, nil).
	_, ok, err := s.GetPreference(ctx, "summarizer.language")
	require.NoError(t, err)
	require.False(t, ok)

	// Upsert a string value.
	require.NoError(t, s.UpsertPreference(ctx, "summarizer.language", "ja"))

	got, ok, err := s.GetPreference(ctx, "summarizer.language")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "summarizer.language", got.Key)
	var lang string
	require.NoError(t, json.Unmarshal(got.Value, &lang))
	require.Equal(t, "ja", lang)
	require.False(t, got.UpdatedAt.IsZero())

	// Overwrite the same key — value should change, key count remains 1.
	require.NoError(t, s.UpsertPreference(ctx, "summarizer.language", "en"))
	got, ok, err = s.GetPreference(ctx, "summarizer.language")
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, json.Unmarshal(got.Value, &lang))
	require.Equal(t, "en", lang)

	// Add a second key.
	require.NoError(t, s.UpsertPreference(ctx, "summarizer.recap_system_prompt", "Be concise."))

	rows, err := s.ListPreferences(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	// Ordered ascending by key.
	require.Equal(t, "summarizer.language", rows[0].Key)
	require.Equal(t, "summarizer.recap_system_prompt", rows[1].Key)

	// Delete one key.
	require.NoError(t, s.DeletePreference(ctx, "summarizer.language"))
	_, ok, err = s.GetPreference(ctx, "summarizer.language")
	require.NoError(t, err)
	require.False(t, ok)

	rows, err = s.ListPreferences(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)

	// Deleting a missing key is a no-op.
	require.NoError(t, s.DeletePreference(ctx, "summarizer.language"))
}

func TestPreferencesAcceptsRawJSON(t *testing.T) {
	ctx := t.Context()
	s := newTestStore(t)

	raw := json.RawMessage(`{"primary":"haiku","fallback":"sonnet"}`)
	require.NoError(t, s.UpsertPreference(ctx, "summarizer.providers", raw))

	got, ok, err := s.GetPreference(ctx, "summarizer.providers")
	require.NoError(t, err)
	require.True(t, ok)

	var decoded map[string]string
	require.NoError(t, json.Unmarshal(got.Value, &decoded))
	require.Equal(t, "haiku", decoded["primary"])
	require.Equal(t, "sonnet", decoded["fallback"])
}

func TestPreferencesEmptyKeyRejected(t *testing.T) {
	ctx := t.Context()
	s := newTestStore(t)

	require.Error(t, s.UpsertPreference(ctx, "", "ja"))
	_, _, err := s.GetPreference(ctx, "")
	require.Error(t, err)
	require.Error(t, s.DeletePreference(ctx, ""))
}
