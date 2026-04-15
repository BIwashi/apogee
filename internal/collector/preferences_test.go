package collector

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func patchPrefs(t *testing.T, base string, body string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPatch, base+"/v1/preferences", bytes.NewBufferString(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { resp.Body.Close() })
	out, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp, out
}

func TestPreferencesGetReturnsDefaults(t *testing.T) {
	_, ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/v1/preferences")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	prefs, ok := body["preferences"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "en", prefs["summarizer.language"])
	require.Equal(t, "", prefs["summarizer.recap_system_prompt"])
	require.Equal(t, "", prefs["summarizer.rollup_system_prompt"])
	require.Equal(t, "", prefs["summarizer.narrative_system_prompt"])
	require.Equal(t, "", prefs["summarizer.recap_model"])
	require.Equal(t, "", prefs["summarizer.rollup_model"])
	require.Equal(t, "", prefs["summarizer.narrative_model"])
	updated, ok := body["updated_at"].(map[string]any)
	require.True(t, ok)
	require.Empty(t, updated, "no rows yet → updated_at is empty")
}

func TestPreferencesPatchHappyPath(t *testing.T) {
	_, ts := newTestServer(t)

	// PATCH a single key.
	resp, raw := patchPrefs(t, ts.URL, `{"summarizer.language":"ja"}`)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(raw))
	var body map[string]any
	require.NoError(t, json.Unmarshal(raw, &body))
	prefs := body["preferences"].(map[string]any)
	require.Equal(t, "ja", prefs["summarizer.language"])
	updated := body["updated_at"].(map[string]any)
	require.Contains(t, updated, "summarizer.language")

	// Subsequent GET observes the change.
	getResp, err := http.Get(ts.URL + "/v1/preferences")
	require.NoError(t, err)
	defer getResp.Body.Close()
	var getBody map[string]any
	require.NoError(t, json.NewDecoder(getResp.Body).Decode(&getBody))
	require.Equal(t, "ja", getBody["preferences"].(map[string]any)["summarizer.language"])

	// PATCH multiple keys at once including a recap system prompt and a
	// model override.
	resp, raw = patchPrefs(t, ts.URL, `{
		"summarizer.recap_system_prompt": "Be concise.",
		"summarizer.recap_model": "claude-haiku-4-5"
	}`)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(raw))
	require.NoError(t, json.Unmarshal(raw, &body))
	prefs = body["preferences"].(map[string]any)
	require.Equal(t, "Be concise.", prefs["summarizer.recap_system_prompt"])
	require.Equal(t, "claude-haiku-4-5", prefs["summarizer.recap_model"])
	// The earlier update is still present.
	require.Equal(t, "ja", prefs["summarizer.language"])
}

func TestPreferencesPatchEmptyBodyIsNoop(t *testing.T) {
	_, ts := newTestServer(t)
	resp, raw := patchPrefs(t, ts.URL, `{}`)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(raw))
	var body map[string]any
	require.NoError(t, json.Unmarshal(raw, &body))
	require.Equal(t, "en", body["preferences"].(map[string]any)["summarizer.language"])
}

func TestPreferencesPatchValidation(t *testing.T) {
	_, ts := newTestServer(t)

	cases := []struct {
		name  string
		body  string
		wantS string
	}{
		{
			name:  "unknown language",
			body:  `{"summarizer.language":"fr"}`,
			wantS: "summarizer.language",
		},
		{
			name:  "recap prompt too long",
			body:  `{"summarizer.recap_system_prompt":"` + strings.Repeat("x", 2049) + `"}`,
			wantS: "summarizer.recap_system_prompt",
		},
		{
			name:  "rollup prompt too long",
			body:  `{"summarizer.rollup_system_prompt":"` + strings.Repeat("y", 2049) + `"}`,
			wantS: "summarizer.rollup_system_prompt",
		},
		{
			name:  "bad recap model",
			body:  `{"summarizer.recap_model":"gpt-4"}`,
			wantS: "summarizer.recap_model",
		},
		{
			name:  "bad rollup model",
			body:  `{"summarizer.rollup_model":"haiku"}`,
			wantS: "summarizer.rollup_model",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp, raw := patchPrefs(t, ts.URL, c.body)
			require.Equal(t, http.StatusBadRequest, resp.StatusCode, string(raw))
			require.Contains(t, string(raw), c.wantS)
		})
	}
}

func TestPreferencesPatchRejectsUnknownFields(t *testing.T) {
	_, ts := newTestServer(t)
	resp, raw := patchPrefs(t, ts.URL, `{"foo":"bar"}`)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode, string(raw))
}

func TestPreferencesPatchClearsModelOverride(t *testing.T) {
	_, ts := newTestServer(t)
	// Set a model override.
	resp, _ := patchPrefs(t, ts.URL, `{"summarizer.recap_model":"claude-haiku-4-5"}`)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	// Clear it with the empty string.
	resp, raw := patchPrefs(t, ts.URL, `{"summarizer.recap_model":""}`)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body map[string]any
	require.NoError(t, json.Unmarshal(raw, &body))
	require.Equal(t, "", body["preferences"].(map[string]any)["summarizer.recap_model"])
}

func TestPreferencesDeleteResetsAll(t *testing.T) {
	_, ts := newTestServer(t)
	// Set a few keys.
	patchPrefs(t, ts.URL, `{"summarizer.language":"ja","summarizer.recap_system_prompt":"hello"}`)
	// DELETE wipes them.
	req, err := http.NewRequest(http.MethodDelete, ts.URL+"/v1/preferences", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	prefs := body["preferences"].(map[string]any)
	require.Equal(t, "en", prefs["summarizer.language"])
	require.Equal(t, "", prefs["summarizer.recap_system_prompt"])
	updated := body["updated_at"].(map[string]any)
	require.Empty(t, updated)
}
