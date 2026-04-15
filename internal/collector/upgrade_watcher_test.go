package collector

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestUpgradeWatcherDetectsBumpedBinary writes a fake binary to a
// temporary path, primes the watcher's baseline, then mutates the file
// to simulate a `brew upgrade` rewriting the bytes. After a single
// check pass the watcher should expose the new version via Snapshot().
func TestUpgradeWatcherDetectsBumpedBinary(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "apogee-test")
	if err := os.WriteFile(bin, []byte("v1-bytes"), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}

	w := &upgradeWatcher{
		binaryPath:     bin,
		runningVersion: "0.1.7",
		tick:           time.Second,
		versionCmd: func(_ context.Context, _ string) (string, error) {
			return "0.1.8", nil
		},
	}
	if err := w.captureBaseline(); err != nil {
		t.Fatalf("baseline: %v", err)
	}

	// Sleep a hair to guarantee a different mtime, then rewrite.
	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(bin, []byte("v2-bytes-longer"), 0o755); err != nil {
		t.Fatalf("rewrite binary: %v", err)
	}

	if err := w.check(context.Background()); err != nil {
		t.Fatalf("check: %v", err)
	}
	avail, detected := w.Snapshot()
	if avail != "0.1.8" {
		t.Fatalf("available=%q want 0.1.8", avail)
	}
	if detected.IsZero() {
		t.Fatalf("expected detected timestamp")
	}
}

// TestUpgradeWatcherIgnoresSameVersion guards against the dev-rebuild
// scenario: the file changes but the version subcommand reports the
// same string. We refresh the baseline so we don't reshell every tick
// but do not raise a banner.
func TestUpgradeWatcherIgnoresSameVersion(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "apogee-test")
	if err := os.WriteFile(bin, []byte("v1"), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}
	w := &upgradeWatcher{
		binaryPath:     bin,
		runningVersion: "0.1.7",
		tick:           time.Second,
		versionCmd: func(_ context.Context, _ string) (string, error) {
			return "0.1.7", nil
		},
	}
	if err := w.captureBaseline(); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(bin, []byte("v1-rebuild"), 0o755); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if err := w.check(context.Background()); err != nil {
		t.Fatalf("check: %v", err)
	}
	if avail, _ := w.Snapshot(); avail != "" {
		t.Fatalf("snapshot=%q want empty", avail)
	}
}

// TestUpgradeWatcherCheckErrorPropagates makes sure a stat failure on
// the binary (e.g. the user moved the file) bubbles back through
// check() and is recorded as the last check error.
func TestUpgradeWatcherCheckErrorPropagates(t *testing.T) {
	t.Parallel()
	w := &upgradeWatcher{
		binaryPath:     "/definitely/not/a/real/path",
		runningVersion: "0.1.7",
		tick:           time.Second,
		versionCmd: func(_ context.Context, _ string) (string, error) {
			return "", errors.New("should not be called")
		},
	}
	if err := w.check(context.Background()); err == nil {
		t.Fatalf("expected error from missing path")
	}
}

// TestPostDaemonRestartCallsRunner exercises the HTTP handler with a
// fake restart runner. The handler must respond 202 immediately and
// then invoke the runner once, with the default daemon label.
func TestPostDaemonRestartCallsRunner(t *testing.T) {
	t.Parallel()

	prev := restartRunner
	prevDelay := daemonRestartDelay
	t.Cleanup(func() {
		restartRunner = prev
		daemonRestartDelay = prevDelay
	})
	daemonRestartDelay = 5 * time.Millisecond

	called := make(chan string, 1)
	restartRunner = func(_ context.Context, label string) error {
		called <- label
		return nil
	}

	srv := &Server{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/daemon/restart", nil)
	srv.postDaemonRestart(rec, req)
	if rec.Code != 202 {
		t.Fatalf("status=%d want 202", rec.Code)
	}
	select {
	case got := <-called:
		if got != "dev.biwashi.apogee" {
			t.Fatalf("label=%q", got)
		}
	case <-time.After(time.Second):
		t.Fatalf("restart runner never called")
	}
}
