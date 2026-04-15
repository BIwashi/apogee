package daemon

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
)

// TestApplyDefaultEnvInjectsPATH guards the launchd / systemd PATH
// fix. Without an explicit PATH the supervisor would inherit only
// /usr/bin:/bin:/usr/sbin:/sbin and the summarizer worker's call to
// the `claude` CLI would silently fail with "executable file not
// found in $PATH".
func TestApplyDefaultEnvInjectsPATH(t *testing.T) {
	t.Setenv("PATH", "/Users/me/.local/bin:/opt/homebrew/bin:/usr/bin:/bin")
	got := applyDefaultEnv(nil)
	if got["PATH"] != "/Users/me/.local/bin:/opt/homebrew/bin:/usr/bin:/bin" {
		t.Errorf("PATH = %q, want install-time PATH", got["PATH"])
	}
	if got["HOME"] == "" {
		t.Errorf("HOME should default to user home, got empty")
	}
}

// TestApplyDefaultEnvRespectsCallerPATH guards against the helper
// stomping on an explicit PATH the caller passed in.
func TestApplyDefaultEnvRespectsCallerPATH(t *testing.T) {
	in := map[string]string{
		"HOME": "/Users/elsewhere",
		"PATH": "/explicit:/path",
	}
	got := applyDefaultEnv(in)
	if got["PATH"] != "/explicit:/path" {
		t.Errorf("PATH should be preserved, got %q", got["PATH"])
	}
	if got["HOME"] != "/Users/elsewhere" {
		t.Errorf("HOME should be preserved, got %q", got["HOME"])
	}
}

// TestApplyDefaultEnvFallback covers the unusual case where the
// install-time process runs without any PATH at all (CI sandbox,
// stripped shell). The helper falls back to a known-good list rather
// than leaving PATH unset.
func TestApplyDefaultEnvFallback(t *testing.T) {
	// t.Setenv both sets the value for the test and restores the
	// previous one on teardown.
	t.Setenv("PATH", "")
	_ = os.Unsetenv("PATH")
	got := applyDefaultEnv(nil)
	if got["PATH"] != fallbackUnitPATH {
		t.Errorf("PATH = %q, want fallback %q", got["PATH"], fallbackUnitPATH)
	}
}

// fakeRunner is the cross-platform commandRunner stub. It records
// every (name, args) pair so tests can assert the right supervisor
// subcommand was invoked, and returns canned responses keyed by the
// first non-dashed argument.
type fakeRunner struct {
	mu    sync.Mutex
	calls []fakeCall

	// responses maps a "verb" (first non-flag arg after the binary
	// name) to a canned response. When no entry matches, Run
	// returns empty strings and nil error.
	responses map[string]fakeResponse
}

type fakeCall struct {
	name string
	args []string
}

type fakeResponse struct {
	stdout string
	stderr string
	err    error
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeCall{name: name, args: append([]string(nil), args...)})
	key := firstNonFlag(args)
	if f.responses != nil {
		if r, ok := f.responses[key]; ok {
			return r.stdout, r.stderr, r.err
		}
	}
	return "", "", nil
}

func firstNonFlag(args []string) string {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		return a
	}
	return ""
}

// TestStatusFormat exercises the cross-platform FormatStatus helper.
// It does not touch launchctl/systemctl so it runs on every GOOS.
func TestStatusFormat(t *testing.T) {
	s := Status{
		Installed: true,
		Loaded:    true,
		Running:   true,
		PID:       12345,
		UnitPath:  "/tmp/fake.plist",
		Label:     "dev.biwashi.apogee",
	}
	out := FormatStatus(s)
	for _, want := range []string{
		"Daemon: dev.biwashi.apogee",
		"Installed:    yes",
		"Loaded:       yes",
		"Running:      yes (pid 12345)",
		"Unit path:    /tmp/fake.plist",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatStatus missing %q in:\n%s", want, out)
		}
	}
}

func TestStatusFormatNotRunning(t *testing.T) {
	s := Status{
		Installed: true,
		Loaded:    false,
		Running:   false,
		Label:     "dev.biwashi.apogee",
	}
	out := FormatStatus(s)
	if !strings.Contains(out, "Running:      no") {
		t.Errorf("FormatStatus should say 'Running: no', got:\n%s", out)
	}
}
