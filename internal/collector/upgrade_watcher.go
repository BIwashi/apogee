package collector

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// semverTokenRE matches a semver-ish version token anywhere in a
// line of text. It accepts an optional leading "v" and captures
// "MAJOR.MINOR.PATCH" plus an optional "-suffix" (e.g. 0.1.11-rc.1).
// We scan the first non-empty line of `<binary> version` output with
// this regex instead of assuming the version is a specific field
// position, because the apogee version output evolved from a short
// "apogee 0.1.7" to the richer
// "apogee v0.1.11 (commit abc, built …, go1.25.0)" format — the
// old fields[len-1] heuristic grabbed "go1.25.0)" from the new
// format and stored it as the available_version.
var semverTokenRE = regexp.MustCompile(`v?\d+\.\d+\.\d+(?:[-.][A-Za-z0-9.-]+)?`)

// upgradeWatcher notices when the apogee binary on disk has been
// replaced by `brew upgrade` (or any other out-of-band update) and
// records the new version string so the dashboard can surface a
// "restart required" banner.
//
// The mechanism is deliberately simple: on startup we stat the
// currently-running binary and remember its size + mtime. A background
// tick re-stats the same path. If either field has changed, we shell
// out to `<path> version` to read the new version line and store it.
// We never overwrite the recorded available_version with the running
// version once a change has been detected; the only way to clear it is
// for the daemon to actually restart into the new binary, at which
// point a fresh upgradeWatcher starts with a new baseline.
type upgradeWatcher struct {
	binaryPath     string
	runningVersion string
	logger         *slog.Logger

	// tick is the poll interval. Overridable in tests.
	tick time.Duration

	// versionCmd runs `<path> version` and returns the first non-empty
	// line of stdout. Overridable in tests so the unit test can
	// simulate an updated binary without linking apogee's version
	// string at test time.
	versionCmd func(ctx context.Context, path string) (string, error)

	mu                 sync.RWMutex
	baselineSize       int64
	baselineMtime      time.Time
	available          string
	availableDetected  time.Time
	lastCheckErr       string
}

// newUpgradeWatcher constructs a watcher rooted at the currently-running
// executable. A nil return means we could not resolve os.Executable —
// upgrade detection is silently disabled in that case. Running version
// is the version string the *current* process was built with.
func newUpgradeWatcher(runningVersion string, logger *slog.Logger) *upgradeWatcher {
	path, err := os.Executable()
	if err != nil {
		if logger != nil {
			logger.Warn("upgrade-watcher: os.Executable failed — upgrade detection disabled", "err", err)
		}
		return nil
	}
	// Resolve symlinks so Homebrew's /opt/homebrew/bin/apogee (a symlink
	// into the Cellar) resolves to the concrete versioned file. Without
	// this, brew upgrade replaces the symlink target but the symlink's
	// mtime appears unchanged, hiding the update from us.
	resolved, err := os.Readlink(path)
	if err == nil && resolved != "" {
		if !strings.HasPrefix(resolved, "/") {
			// Relative symlink target — let filepath.EvalSymlinks do
			// the work instead.
			resolved = ""
		}
	}
	if resolved != "" {
		path = resolved
	}
	return &upgradeWatcher{
		binaryPath:     path,
		runningVersion: runningVersion,
		logger:         logger,
		tick:           60 * time.Second,
		versionCmd:     defaultVersionCmd,
	}
}

// Start begins the background poll loop. It returns immediately; the
// loop stops when ctx is done. Start records an initial baseline so a
// restart on an already-upgraded-but-not-restarted machine still
// detects the pending upgrade on the very first tick.
func (w *upgradeWatcher) Start(ctx context.Context) {
	if w == nil {
		return
	}
	if err := w.captureBaseline(); err != nil {
		w.setCheckErr(err)
		if w.logger != nil {
			w.logger.Warn("upgrade-watcher: baseline stat failed", "path", w.binaryPath, "err", err)
		}
	}
	go w.loop(ctx)
}

func (w *upgradeWatcher) loop(ctx context.Context) {
	t := time.NewTicker(w.tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := w.check(ctx); err != nil {
				w.setCheckErr(err)
				if w.logger != nil {
					w.logger.Debug("upgrade-watcher: tick failed", "err", err)
				}
			}
		}
	}
}

func (w *upgradeWatcher) captureBaseline() error {
	fi, err := os.Stat(w.binaryPath)
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.baselineSize = fi.Size()
	w.baselineMtime = fi.ModTime()
	w.mu.Unlock()
	return nil
}

// check stats the binary and, if it looks different from the recorded
// baseline, runs `<path> version` and records the new version string.
// Returns nil when nothing needs doing.
func (w *upgradeWatcher) check(ctx context.Context) error {
	fi, err := os.Stat(w.binaryPath)
	if err != nil {
		return err
	}
	w.mu.RLock()
	same := fi.Size() == w.baselineSize && fi.ModTime().Equal(w.baselineMtime)
	w.mu.RUnlock()
	if same {
		return nil
	}
	newVersion, err := w.versionCmd(ctx, w.binaryPath)
	if err != nil {
		return err
	}
	newVersion = strings.TrimSpace(newVersion)
	if newVersion == "" || newVersion == w.runningVersion {
		// Binary changed but reports the same version string —
		// probably a rebuild with identical VERSION (dev flow). Do
		// not raise a banner, but refresh the baseline so we don't
		// re-shell every tick.
		w.mu.Lock()
		w.baselineSize = fi.Size()
		w.baselineMtime = fi.ModTime()
		w.lastCheckErr = ""
		w.mu.Unlock()
		return nil
	}
	w.mu.Lock()
	w.available = newVersion
	w.availableDetected = time.Now().UTC()
	w.baselineSize = fi.Size()
	w.baselineMtime = fi.ModTime()
	w.lastCheckErr = ""
	w.mu.Unlock()
	if w.logger != nil {
		w.logger.Info("upgrade-watcher: new version available",
			"path", w.binaryPath,
			"running", w.runningVersion,
			"available", newVersion,
		)
	}
	return nil
}

func (w *upgradeWatcher) setCheckErr(err error) {
	if err == nil {
		return
	}
	w.mu.Lock()
	w.lastCheckErr = err.Error()
	w.mu.Unlock()
}

// Snapshot returns the currently-recorded upgrade state for
// /v1/info. When no upgrade has been detected, available is "".
func (w *upgradeWatcher) Snapshot() (available string, detectedAt time.Time) {
	if w == nil {
		return "", time.Time{}
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.available, w.availableDetected
}

// defaultVersionCmd shells out to `<path> version` and returns the
// first non-empty line of stdout and extracts the version token via
// semverTokenRE. The apogee version subcommand's output evolved from
//
//	apogee 0.1.7
//
// to
//
//	apogee v0.1.11 (commit 6248d8d, built 2026-04-15T12:02:27Z, go1.25.0)
//
// so a positional heuristic (first/last field) does not work across
// versions. The regex match grabs "v0.1.11" regardless of its field
// index and we strip the leading "v" so /v1/info reports the bare
// version string the dashboard banner compares against.
func defaultVersionCmd(ctx context.Context, path string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, path, "version")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", errors.Join(err, errors.New(strings.TrimSpace(out.String())))
	}
	return extractVersionToken(out.String())
}

// extractVersionToken scans the first non-empty line of the given
// output for the first semver-ish token. Exported for tests only
// (lowercase). Returns a friendly error when no token is found so
// the watcher can log a meaningful warning instead of silently
// storing garbage.
func extractVersionToken(raw string) (string, error) {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		match := semverTokenRE.FindString(line)
		if match == "" {
			continue
		}
		return strings.TrimPrefix(match, "v"), nil
	}
	return "", errors.New("upgrade-watcher: empty or unrecognised version output")
}
