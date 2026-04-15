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

// TestExtractVersionToken guards the upgrade-watcher parser against
// regressions when the `apogee version` output format evolves. The
// parser must tolerate:
//
//   - the legacy "apogee 0.1.7" short form
//   - the current "apogee v0.1.11 (commit 6248d8d, built …, go1.25.0)"
//     rich form where the version token is the second field and is
//     followed by unrelated tokens that also look numeric
//   - leading/trailing whitespace and empty lines
//
// Negative cases: output with no version token at all, or output
// that contains only the "go1.25.0" runtime version (which is a
// semver-ish token that should NOT match at semverTokenRE — the
// regex anchors on a leading "v" or a standalone "x.y.z" but
// accepts "go1.25.0" too because "1.25.0" is a valid match. The
// legacy behaviour grabbed "go1.25.0)" from the last field, so the
// regex is strictly better even if it happens to match the go
// runtime version when no apogee version appears — we pick the
// first match in the line, which for real output is always the
// apogee token).
func TestExtractVersionToken(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "rich-format-with-trailing-metadata",
			input: "apogee v0.1.11 (commit 6248d8d, built 2026-04-15T12:02:27Z, go1.25.0)\n",
			want:  "0.1.11",
		},
		{
			name:  "legacy-short-format",
			input: "apogee 0.1.7\n",
			want:  "0.1.7",
		},
		{
			name:  "bare-version",
			input: "0.2.0\n",
			want:  "0.2.0",
		},
		{
			name:  "prerelease-suffix",
			input: "apogee v0.2.0-rc.1 (commit ..., go1.25.0)\n",
			want:  "0.2.0-rc.1",
		},
		{
			name:  "leading-whitespace-and-blank-lines",
			input: "\n\n   apogee v1.2.3\n",
			want:  "1.2.3",
		},
		{
			name:    "no-version-at-all",
			input:   "hello world\n",
			wantErr: true,
		},
		{
			name:    "empty",
			input:   "",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := extractVersionToken(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}

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

	if err := w.check(t.Context()); err != nil {
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
	if err := w.check(t.Context()); err != nil {
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
	if err := w.check(t.Context()); err == nil {
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

// TestAutoRestartLoopFiresAfterDelay exercises the auto-restart
// goroutine: a watcher pre-seeded with a detected upgrade and a very
// short grace window should call restartRunner once within the test
// timeout. Guards against regressions that would either fire the
// restart instantly (no delay) or never fire (broken trigger logic).
//
// Not parallel: both auto-restart tests swap the package-level
// restartRunner, so running them concurrently would let one test's
// runner leak into the other's assertion window.
func TestAutoRestartLoopFiresAfterDelay(t *testing.T) {
	prev := restartRunner
	t.Cleanup(func() { restartRunner = prev })

	called := make(chan string, 4)
	restartRunner = func(_ context.Context, label string) error {
		called <- label
		return nil
	}

	w := &upgradeWatcher{
		binaryPath:     "/tmp/apogee-test-auto-restart",
		runningVersion: "0.1.9",
		tick:           time.Hour,
		versionCmd:     nil,
	}
	// Pre-seed the snapshot so the first tick in autoRestartLoop
	// already sees an upgrade that is older than the delay window.
	w.mu.Lock()
	w.available = "0.2.0"
	w.availableDetected = time.Now().Add(-1 * time.Hour)
	w.mu.Unlock()

	srv := &Server{
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		upgradeWatcher: w,
		cfg: Config{
			AutoRestart:      true,
			AutoRestartDelay: 10 * time.Millisecond,
		},
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	// Run the loop directly with a sub-tick override is not exposed;
	// the production loop polls every 15s. To keep the test fast we
	// rely on the fact that the first select iteration will fire the
	// ticker after the tick duration — so we override tick via a
	// dedicated helper rather than waiting 15s.
	go srv.autoRestartLoopWithTick(ctx, 10*time.Millisecond)

	select {
	case got := <-called:
		if got != "dev.biwashi.apogee" {
			t.Fatalf("label=%q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("auto-restart loop never fired")
	}
}

// TestAutoRestartLoopWaitsForDelay verifies the grace window — a
// watcher whose upgrade was detected only 5 ms ago, with a 2 s
// delay, must NOT fire the restart inside a 200 ms observation
// window. Not parallel (see note on TestAutoRestartLoopFiresAfterDelay).
func TestAutoRestartLoopWaitsForDelay(t *testing.T) {
	prev := restartRunner
	t.Cleanup(func() { restartRunner = prev })

	called := make(chan string, 1)
	restartRunner = func(_ context.Context, label string) error {
		called <- label
		return nil
	}

	w := &upgradeWatcher{
		binaryPath:     "/tmp/apogee-test-auto-restart",
		runningVersion: "0.1.9",
	}
	w.mu.Lock()
	w.available = "0.2.0"
	w.availableDetected = time.Now()
	w.mu.Unlock()

	srv := &Server{
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		upgradeWatcher: w,
		cfg: Config{
			AutoRestart:      true,
			AutoRestartDelay: 2 * time.Second,
		},
	}
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	go srv.autoRestartLoopWithTick(ctx, 20*time.Millisecond)

	select {
	case <-called:
		t.Fatalf("auto-restart fired before grace window elapsed")
	case <-ctx.Done():
		// Expected — no restart within the observation window.
	}
}
