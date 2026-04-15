package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/BIwashi/apogee/internal/daemon"
)

// fakeManager is a minimal Manager implementation used by CLI tests
// so we never touch real launchctl / systemctl. State is recorded on
// the struct so tests can assert.
type fakeManager struct {
	installed bool
	running   bool
	pid       int
	started   time.Time

	installCalls   int
	uninstallCalls int
	startCalls     int
	stopCalls      int
	restartCalls   int
	statusCalls    int

	unitPath string
	label    string

	installErr   error
	startErr     error
	stopErr      error
	restartErr   error
	uninstallErr error
	statusErr    error
}

func newFakeManager() *fakeManager {
	return &fakeManager{
		unitPath: "/tmp/fake/dev.biwashi.apogee.plist",
		label:    daemon.DefaultLabel,
	}
}

func (f *fakeManager) Install(_ context.Context, _ daemon.Config) error {
	f.installCalls++
	if f.installErr != nil {
		return f.installErr
	}
	f.installed = true
	return nil
}

func (f *fakeManager) Uninstall(_ context.Context) error {
	f.uninstallCalls++
	if f.uninstallErr != nil {
		return f.uninstallErr
	}
	f.installed = false
	f.running = false
	return nil
}

func (f *fakeManager) Start(_ context.Context) error {
	f.startCalls++
	if f.startErr != nil {
		return f.startErr
	}
	if !f.installed {
		return daemon.ErrNotInstalled
	}
	f.running = true
	f.pid = 4242
	f.started = time.Now()
	return nil
}

func (f *fakeManager) Stop(_ context.Context) error {
	f.stopCalls++
	if f.stopErr != nil {
		return f.stopErr
	}
	f.running = false
	f.pid = 0
	return nil
}

func (f *fakeManager) Restart(_ context.Context) error {
	f.restartCalls++
	if f.restartErr != nil {
		return f.restartErr
	}
	f.running = true
	f.pid = 4243
	f.started = time.Now()
	return nil
}

func (f *fakeManager) Status(_ context.Context) (daemon.Status, error) {
	f.statusCalls++
	return daemon.Status{
		Installed: f.installed,
		Loaded:    f.installed,
		Running:   f.running,
		PID:       f.pid,
		StartedAt: f.started,
		UnitPath:  f.unitPath,
		Label:     f.label,
	}, f.statusErr
}

func (f *fakeManager) UnitPath() string { return f.unitPath }
func (f *fakeManager) Label() string    { return f.label }

// withFakeManager swaps managerFactory for the duration of t.
func withFakeManager(t *testing.T, m *fakeManager) {
	t.Helper()
	prev := managerFactory
	managerFactory = func() (daemon.Manager, error) { return m, nil }
	t.Cleanup(func() { managerFactory = prev })
}

func TestDaemonSubcommandTree(t *testing.T) {
	var stdout, stderr bytes.Buffer
	d := NewDaemonCmd(&stdout, &stderr)
	want := map[string]bool{
		"install":   false,
		"uninstall": false,
		"start":     false,
		"stop":      false,
		"restart":   false,
		"status":    false,
	}
	for _, c := range d.Commands() {
		if _, ok := want[c.Name()]; ok {
			want[c.Name()] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("daemon is missing subcommand %q", name)
		}
	}
}

func TestDaemonInstallHappyPath(t *testing.T) {
	fm := newFakeManager()
	withFakeManager(t, fm)

	var stdout, stderr bytes.Buffer
	root := NewRootCmd(&stdout, &stderr)
	root.SetArgs([]string{"daemon", "install"})
	if err := root.Execute(); err != nil {
		t.Fatalf("install: %v", err)
	}
	if fm.installCalls != 1 {
		t.Errorf("expected 1 install call, got %d", fm.installCalls)
	}
	out := stdout.String()
	if !strings.Contains(out, "daemon installed") {
		t.Errorf("expected success banner, got: %s", out)
	}
}

func TestDaemonInstallConflictFriendlyError(t *testing.T) {
	fm := newFakeManager()
	fm.installErr = daemon.ErrAlreadyInstalled
	withFakeManager(t, fm)

	var stdout, stderr bytes.Buffer
	root := NewRootCmd(&stdout, &stderr)
	root.SetArgs([]string{"daemon", "install"})
	err := root.Execute()
	if err == nil {
		t.Fatalf("expected friendly conflict error")
	}
	if !strings.Contains(err.Error(), "already installed") {
		t.Errorf("expected conflict message, got: %v", err)
	}
}

func TestDaemonStartNotInstalledError(t *testing.T) {
	fm := newFakeManager()
	withFakeManager(t, fm)

	var stdout, stderr bytes.Buffer
	root := NewRootCmd(&stdout, &stderr)
	root.SetArgs([]string{"daemon", "start"})
	err := root.Execute()
	if err == nil {
		t.Fatalf("expected not-installed error")
	}
	if !strings.Contains(err.Error(), "not installed") {
		t.Errorf("expected friendly message, got: %v", err)
	}
}

func TestDaemonStatusPrintsNotInstalled(t *testing.T) {
	fm := newFakeManager()
	withFakeManager(t, fm)

	var stdout, stderr bytes.Buffer
	root := NewRootCmd(&stdout, &stderr)
	root.SetArgs([]string{"daemon", "status"})
	if err := root.Execute(); err != nil {
		t.Fatalf("status: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "Daemon: dev.biwashi.apogee") {
		t.Errorf("status missing daemon header: %s", out)
	}
	if !strings.Contains(out, "not installed") {
		t.Errorf("status should say not installed, got: %s", out)
	}
}

func TestDaemonStopHappyPath(t *testing.T) {
	fm := newFakeManager()
	fm.installed = true
	fm.running = true
	fm.pid = 1234
	withFakeManager(t, fm)

	var stdout, stderr bytes.Buffer
	root := NewRootCmd(&stdout, &stderr)
	root.SetArgs([]string{"daemon", "stop"})
	if err := root.Execute(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if fm.stopCalls != 1 {
		t.Errorf("expected 1 stop call, got %d", fm.stopCalls)
	}
}
