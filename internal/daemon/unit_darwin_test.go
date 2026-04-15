//go:build darwin

package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderPlistGolden(t *testing.T) {
	cfg := Config{
		Label:      DefaultLabel,
		BinaryPath: "/opt/homebrew/bin/apogee",
		Args: []string{
			"serve",
			"--addr", "127.0.0.1:4100",
			"--db", "/Users/me/.apogee/apogee.duckdb",
		},
		WorkingDir: "/Users/me/.apogee",
		LogDir:     "/Users/me/.apogee/logs",
		Environment: map[string]string{
			"HOME": "/Users/me",
		},
		KeepAlive: true,
		RunAtLoad: true,
	}
	got, err := renderPlist(DefaultLabel, cfg)
	if err != nil {
		t.Fatalf("renderPlist: %v", err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "expected_plist.xml"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("plist content mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestParseLaunchctlPrint(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "launchctl_print.txt"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var s Status
	parseLaunchctlPrint(string(raw), &s)
	if !s.Loaded {
		t.Errorf("expected Loaded=true")
	}
	if !s.Running {
		t.Errorf("expected Running=true")
	}
	if s.PID != 67890 {
		t.Errorf("expected PID=67890, got %d", s.PID)
	}
	if s.LastExitCode != 0 {
		t.Errorf("expected LastExitCode=0, got %d", s.LastExitCode)
	}
}

// launchdTestManager builds a manager with an injected runner and a
// tempdir plist directory so Install/Uninstall/Start/Stop can run
// without touching the real $HOME/Library/LaunchAgents.
func launchdTestManager(t *testing.T, r commandRunner) (*launchdManager, string) {
	t.Helper()
	tmp := t.TempDir()
	m := &launchdManager{
		runner:   r,
		homeDir:  tmp,
		label:    DefaultLabel,
		uid:      501,
		plistDir: filepath.Join(tmp, "Library", "LaunchAgents"),
	}
	return m, tmp
}

func TestLaunchdInstallWritesPlist(t *testing.T) {
	r := &fakeRunner{}
	m, _ := launchdTestManager(t, r)

	cfg := Config{
		Label:      DefaultLabel,
		BinaryPath: "/opt/homebrew/bin/apogee",
		Args:       []string{"serve", "--addr", "127.0.0.1:4100", "--db", "/tmp/x.duckdb"},
		WorkingDir: "/tmp/wd",
		LogDir:     "/tmp/logs",
		Environment: map[string]string{
			"HOME": "/Users/me",
		},
		KeepAlive: true,
		RunAtLoad: true,
	}
	if err := m.Install(context.Background(), cfg); err != nil {
		t.Fatalf("Install: %v", err)
	}
	// Verify the plist file exists and contains expected fragments.
	data, err := os.ReadFile(m.UnitPath())
	if err != nil {
		t.Fatalf("read plist: %v", err)
	}
	if !strings.Contains(string(data), "<string>/opt/homebrew/bin/apogee</string>") {
		t.Errorf("plist missing binary path: %s", data)
	}
	if !strings.Contains(string(data), "<string>dev.biwashi.apogee</string>") {
		t.Errorf("plist missing label: %s", data)
	}
	// Verify bootstrap was called.
	if len(r.calls) == 0 {
		t.Fatalf("expected at least one launchctl call")
	}
	lc := r.calls[len(r.calls)-1]
	if lc.name != "launchctl" || lc.args[0] != "bootstrap" {
		t.Errorf("expected launchctl bootstrap, got %+v", lc)
	}
}

func TestLaunchdInstallIdempotent(t *testing.T) {
	r := &fakeRunner{}
	m, _ := launchdTestManager(t, r)
	cfg := Config{BinaryPath: "/opt/homebrew/bin/apogee", Args: []string{"serve"}}
	if err := m.Install(context.Background(), cfg); err != nil {
		t.Fatalf("first install: %v", err)
	}
	before := len(r.calls)
	if err := m.Install(context.Background(), cfg); err != nil {
		t.Fatalf("second install should be idempotent, got %v", err)
	}
	// Second install should re-bootstrap (1 call) but not replace the file.
	if len(r.calls) <= before {
		t.Errorf("expected re-bootstrap call after idempotent install")
	}
}

func TestLaunchdInstallConflictWithoutForce(t *testing.T) {
	r := &fakeRunner{}
	m, _ := launchdTestManager(t, r)
	if err := m.Install(context.Background(), Config{BinaryPath: "/bin/a", Args: []string{"serve"}}); err != nil {
		t.Fatalf("first install: %v", err)
	}
	err := m.Install(context.Background(), Config{BinaryPath: "/bin/b", Args: []string{"serve"}})
	if err != ErrAlreadyInstalled {
		t.Errorf("expected ErrAlreadyInstalled, got %v", err)
	}
}

func TestLaunchdInstallForceOverwrites(t *testing.T) {
	r := &fakeRunner{}
	m, _ := launchdTestManager(t, r)
	if err := m.Install(context.Background(), Config{BinaryPath: "/bin/a", Args: []string{"serve"}}); err != nil {
		t.Fatalf("first install: %v", err)
	}
	if err := m.Install(context.Background(), Config{BinaryPath: "/bin/b", Args: []string{"serve"}, Force: true}); err != nil {
		t.Fatalf("forced install: %v", err)
	}
	data, _ := os.ReadFile(m.UnitPath())
	if !strings.Contains(string(data), "/bin/b") {
		t.Errorf("force install did not overwrite binary path: %s", data)
	}
}

func TestLaunchdUninstallRemovesPlist(t *testing.T) {
	r := &fakeRunner{}
	m, _ := launchdTestManager(t, r)
	if err := m.Install(context.Background(), Config{BinaryPath: "/bin/a", Args: []string{"serve"}}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := m.Uninstall(context.Background()); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if _, err := os.Stat(m.UnitPath()); !os.IsNotExist(err) {
		t.Errorf("expected plist removed, stat: %v", err)
	}
}

func TestLaunchdStartNotInstalled(t *testing.T) {
	r := &fakeRunner{}
	m, _ := launchdTestManager(t, r)
	err := m.Start(context.Background())
	if err != ErrNotInstalled {
		t.Errorf("expected ErrNotInstalled, got %v", err)
	}
}

func TestLaunchdStatusNotLoaded(t *testing.T) {
	r := &fakeRunner{
		responses: map[string]fakeResponse{
			"print": {err: errPrintUnloaded{}},
		},
	}
	m, _ := launchdTestManager(t, r)
	s, err := m.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if s.Loaded || s.Running {
		t.Errorf("expected not loaded, got %+v", s)
	}
	if s.Installed {
		t.Errorf("should not be installed without a plist")
	}
}

// errPrintUnloaded is a stand-in error for the "service not found"
// condition returned by launchctl when print is called on a missing
// target.
type errPrintUnloaded struct{}

func (errPrintUnloaded) Error() string { return "Could not find service" }
