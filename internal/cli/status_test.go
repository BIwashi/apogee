package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStatusCmdWithFakeCollector(t *testing.T) {
	// Spin up a fake collector so `apogee status` can probe
	// /v1/healthz without requiring the real daemon.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/healthz":
			w.WriteHeader(200)
			_, _ = w.Write([]byte("ok"))
		case "/v1/attention/counts":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"intervene_now":1}`))
		case "/v1/turns/active":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`[{"id":"a"},{"id":"b"},{"id":"c"}]`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	fm := newFakeManager()
	fm.installed = true
	fm.running = true
	fm.pid = 555
	withFakeManager(t, fm)

	var stdout, stderr bytes.Buffer
	root := NewRootCmd(&stdout, &stderr)
	// strip "http://" prefix from httptest URL
	addr := strings.TrimPrefix(srv.URL, "http://")
	root.SetArgs([]string{"status", "--addr", addr})
	if err := root.Execute(); err != nil {
		t.Fatalf("status: %v", err)
	}
	out := stdout.String()
	for _, frag := range []string{
		"APOGEE STATUS",
		"Daemon:    running (pid 555",
		"Collector: http://" + addr,
		"3 active turns",
		"1 intervene_now",
	} {
		if !strings.Contains(out, frag) {
			t.Errorf("status missing %q in:\n%s", frag, out)
		}
	}
}

func TestStatusCmdCollectorDown(t *testing.T) {
	fm := newFakeManager()
	withFakeManager(t, fm)

	var stdout, stderr bytes.Buffer
	root := NewRootCmd(&stdout, &stderr)
	// Point at an unlikely port so the probe fails fast.
	root.SetArgs([]string{"status", "--addr", "127.0.0.1:1"})
	if err := root.Execute(); err != nil {
		t.Fatalf("status: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "Daemon:    not installed") {
		t.Errorf("expected not installed marker, got: %s", out)
	}
	if !strings.Contains(out, "unreachable") {
		t.Errorf("expected unreachable marker, got: %s", out)
	}
}
