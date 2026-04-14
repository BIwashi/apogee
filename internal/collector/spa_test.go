package collector

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSPARootServesIndex asserts that GET / returns the embedded index.html.
// The assertion is deliberately loose: a placeholder dist and a real Next.js
// export share only the `<html>` and `</html>` tags, so we look for those.
func TestSPARootServesIndex(t *testing.T) {
	_, ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "text/html")
	body, _ := io.ReadAll(resp.Body)
	require.Contains(t, strings.ToLower(string(body)), "<html")
	require.Contains(t, strings.ToLower(string(body)), "apogee")
}

// TestSPAUnknownRouteFallsBackToIndex confirms the SPA fallback: any route
// that does not map to a file (but also does not hit /v1/*) returns the
// index.html. This is what makes client-side routing like
// `/session/?id=...` reachable from a static bundle.
func TestSPAUnknownRouteFallsBackToIndex(t *testing.T) {
	_, ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/this/does/not/exist")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	require.Contains(t, strings.ToLower(string(body)), "<html")
}

// TestSPADoesNotSwallowV1 confirms that unknown /v1/* routes still return
// the JSON 404 shape instead of the HTML fallback, so API clients never see
// a text/html response where they expected application/json.
func TestSPADoesNotSwallowV1(t *testing.T) {
	_, ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/v1/does-not-exist")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "application/json")
}

// TestSPAHealthzStillWorks pins the collector contract: the SPA handler is
// mounted after the /v1 routes and must not eat them.
func TestSPAHealthzStillWorks(t *testing.T) {
	_, ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/v1/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	require.Contains(t, string(body), "ok")
}
