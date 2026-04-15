//go:build darwin

package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestPollCollector_Healthy stubs out the three endpoints the menubar reads
// and verifies that pollCollector decodes every count correctly. This is the
// happy path — collectorOK true, activeTurns from the /v1/turns/active
// payload, and intervene/sessions from the attention-counts struct.
func TestPollCollector_Healthy(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/v1/turns/active", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"turns": []any{1, 2, 3},
		})
	})
	mux.HandleFunc("/v1/attention/counts", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]int{
			"intervene_now": 1,
			"watch":         2,
			"watchlist":     0,
			"healthy":       5,
			"total":         8,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := &http.Client{}
	snap := pollCollector(client, srv.URL)

	if !snap.collectorOK {
		t.Errorf("expected collectorOK, got error %q", snap.lastError)
	}
	if snap.activeTurns != 3 {
		t.Errorf("activeTurns = %d, want 3", snap.activeTurns)
	}
	if snap.intervene != 1 {
		t.Errorf("intervene = %d, want 1", snap.intervene)
	}
	if snap.sessions != 8 {
		t.Errorf("sessions = %d, want 8", snap.sessions)
	}
}

// TestPollCollector_Down verifies that an unreachable collector yields a
// snapshot with collectorOK=false and a non-empty lastError that the menu
// renderer can surface to the user.
func TestPollCollector_Down(t *testing.T) {
	client := &http.Client{}
	snap := pollCollector(client, "http://127.0.0.1:1") // unreachable
	if snap.collectorOK {
		t.Error("expected collectorOK=false")
	}
	if snap.lastError == "" {
		t.Error("expected lastError to be set")
	}
}

// TestGlyphTitle covers every branch of the menu-bar title renderer: the
// offline label, the intervene triangle, the running circle with count, and
// the idle circle.
func TestGlyphTitle(t *testing.T) {
	cases := []struct {
		snap menubarSnapshot
		want string
	}{
		{menubarSnapshot{CollectorOK: false}, "apogee · offline"},
		{menubarSnapshot{CollectorOK: true, Intervene: 2}, "apogee · ▲ 2"},
		{menubarSnapshot{CollectorOK: true, ActiveTurns: 3}, "apogee · ● 3"},
		{menubarSnapshot{CollectorOK: true}, "apogee · ●"},
	}
	for _, c := range cases {
		got := glyphTitle(c.snap)
		if got != c.want {
			t.Errorf("glyphTitle(%+v) = %q, want %q", c.snap, got, c.want)
		}
	}
}

// TestBuildMenu_Healthy sanity-checks the dropdown structure when the
// collector is up. We intentionally do not enumerate every item — the
// test just verifies the title is first and the item count matches the
// product-model layout.
func TestBuildMenu_Healthy(t *testing.T) {
	snap := menubarSnapshot{
		CollectorOK: true,
		Sessions:    10,
		ActiveTurns: 3,
		Intervene:   1,
	}
	items := buildMenu(snap, "http://127.0.0.1:4100")
	if len(items) < 8 {
		t.Errorf("expected at least 8 menu items, got %d", len(items))
	}
	// The first item should be the "apogee" title
	if items[0].Text != "apogee" {
		t.Errorf("first item = %q, want %q", items[0].Text, "apogee")
	}
}

// TestBuildMenu_Offline verifies the fallback path: when the collector is
// unreachable and we captured an error string, that error is surfaced in
// the dropdown so the user can diagnose the outage without reading logs.
func TestBuildMenu_Offline(t *testing.T) {
	snap := menubarSnapshot{
		CollectorOK: false,
		LastError:   "connection refused",
	}
	items := buildMenu(snap, "http://127.0.0.1:4100")
	found := false
	for _, item := range items {
		if item.Text == "  connection refused" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected the lastError text to appear in the menu")
	}
}
