package cli

import (
	"bytes"
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/BIwashi/apogee/internal/daemon"
)

// withFakeMenubarManager swaps menubarManagerFactory for the duration
// of the test. Mirrors withFakeManager for the daemon package.
func withFakeMenubarManager(t *testing.T, m *fakeManager) {
	t.Helper()
	prev := menubarManagerFactory
	menubarManagerFactory = func() (daemon.Manager, error) { return m, nil }
	t.Cleanup(func() { menubarManagerFactory = prev })
}

// TestMenubarSubcommandTree asserts `apogee menubar` has the three
// login-item children wired up (install / uninstall / status). This
// catches the "forgot to AddCommand" class of regression and runs on
// every platform — the tree is the same across GOOS.
func TestMenubarSubcommandTree(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cmd := NewMenubarCmd(&stdout, &stderr)
	want := map[string]bool{
		"install":   false,
		"uninstall": false,
		"status":    false,
	}
	for _, c := range cmd.Commands() {
		if _, ok := want[c.Name()]; ok {
			want[c.Name()] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("menubar is missing subcommand %q", name)
		}
	}
}

// TestMenubarInstallHappyPath drives `apogee menubar install` on
// darwin and asserts the fake manager saw exactly one Install call
// with the menubar label. On non-darwin we assert the warn-line
// short-circuit is what the user sees.
func TestMenubarInstallHappyPath(t *testing.T) {
	fm := newFakeManager()
	fm.label = daemon.MenubarLabel
	fm.unitPath = "/tmp/fake/dev.biwashi.apogee.menubar.plist"
	withFakeMenubarManager(t, fm)

	var stdout, stderr bytes.Buffer
	root := NewRootCmd(&stdout, &stderr)
	root.SetArgs([]string{"menubar", "install"})
	if err := root.Execute(); err != nil {
		t.Fatalf("install: %v", err)
	}

	out := stdout.String()
	if runtime.GOOS != "darwin" {
		if !strings.Contains(out, "macOS only") {
			t.Errorf("non-darwin should short-circuit with warn line, got: %s", out)
		}
		if fm.installCalls != 0 {
			t.Errorf("non-darwin should not call Install, got %d", fm.installCalls)
		}
		return
	}
	if fm.installCalls != 1 {
		t.Errorf("expected 1 Install call, got %d", fm.installCalls)
	}
	if !strings.Contains(out, "menubar installed") {
		t.Errorf("expected success banner, got: %s", out)
	}
}

// TestMenubarInstallConflictFriendlyError verifies that
// ErrAlreadyInstalled is translated into a human-friendly message
// pointing the user at --force. Darwin-only because the non-darwin
// path short-circuits before touching the manager.
func TestMenubarInstallConflictFriendlyError(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("menubar install is darwin-only")
	}
	fm := newFakeManager()
	fm.label = daemon.MenubarLabel
	fm.unitPath = "/tmp/fake/dev.biwashi.apogee.menubar.plist"
	fm.installErr = daemon.ErrAlreadyInstalled
	withFakeMenubarManager(t, fm)

	var stdout, stderr bytes.Buffer
	root := NewRootCmd(&stdout, &stderr)
	root.SetArgs([]string{"menubar", "install"})
	err := root.Execute()
	if err == nil {
		t.Fatalf("expected friendly conflict error")
	}
	if !strings.Contains(err.Error(), "already installed") {
		t.Errorf("expected conflict message, got: %v", err)
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("conflict message should hint at --force, got: %v", err)
	}
}

// TestMenubarInstallForceFlag verifies --force is forwarded on the
// Config passed to Manager.Install so the manager's Force path can
// overwrite an existing plist.
func TestMenubarInstallForceFlag(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("menubar install is darwin-only")
	}
	fm := newFakeManager()
	fm.label = daemon.MenubarLabel
	fm.unitPath = "/tmp/fake/dev.biwashi.apogee.menubar.plist"
	// Use a recording fake that captures Force so we can assert
	// on it. The simplest thing is to override Install via a
	// subclass, but fakeManager is a struct — we swap the factory
	// for a per-call closure instead.
	var sawForce bool
	prev := menubarManagerFactory
	menubarManagerFactory = func() (daemon.Manager, error) {
		return &recordingMenubarManager{
			fake: fm,
			onInstall: func(cfg daemon.Config) {
				sawForce = cfg.Force
			},
		}, nil
	}
	t.Cleanup(func() { menubarManagerFactory = prev })

	var stdout, stderr bytes.Buffer
	root := NewRootCmd(&stdout, &stderr)
	root.SetArgs([]string{"menubar", "install", "--force"})
	if err := root.Execute(); err != nil {
		t.Fatalf("install --force: %v", err)
	}
	if !sawForce {
		t.Errorf("expected Config.Force=true when --force is passed")
	}
}

// TestMenubarUninstallHappyPath runs the uninstall path and checks
// the fake manager saw one Uninstall call plus the styled info box
// showing the label.
func TestMenubarUninstallHappyPath(t *testing.T) {
	fm := newFakeManager()
	fm.label = daemon.MenubarLabel
	fm.installed = true
	fm.unitPath = "/tmp/fake/dev.biwashi.apogee.menubar.plist"
	withFakeMenubarManager(t, fm)

	var stdout, stderr bytes.Buffer
	root := NewRootCmd(&stdout, &stderr)
	root.SetArgs([]string{"menubar", "uninstall"})
	if err := root.Execute(); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if runtime.GOOS != "darwin" {
		if fm.uninstallCalls != 0 {
			t.Errorf("non-darwin should not call Uninstall")
		}
		return
	}
	if fm.uninstallCalls != 1 {
		t.Errorf("expected 1 Uninstall call, got %d", fm.uninstallCalls)
	}
	out := stdout.String()
	if !strings.Contains(out, "menubar uninstalled") {
		t.Errorf("expected uninstall banner, got: %s", out)
	}
}

// TestMenubarStatusNotInstalled exercises the status command against
// a fresh (not-installed) fake. The output should include the
// Menubar: heading and the "not installed" hint.
func TestMenubarStatusNotInstalled(t *testing.T) {
	fm := newFakeManager()
	fm.label = daemon.MenubarLabel
	fm.unitPath = "/tmp/fake/dev.biwashi.apogee.menubar.plist"
	withFakeMenubarManager(t, fm)

	var stdout, stderr bytes.Buffer
	root := NewRootCmd(&stdout, &stderr)
	root.SetArgs([]string{"menubar", "status"})
	if err := root.Execute(); err != nil {
		t.Fatalf("status: %v", err)
	}
	out := stdout.String()
	if runtime.GOOS != "darwin" {
		if !strings.Contains(out, "macOS only") {
			t.Errorf("non-darwin should show warn line, got: %s", out)
		}
		return
	}
	if !strings.Contains(out, "Menubar: "+daemon.MenubarLabel) {
		t.Errorf("status missing menubar header, got: %s", out)
	}
	if !strings.Contains(out, "not installed") {
		t.Errorf("status should say not installed, got: %s", out)
	}
}

// TestMenubarStatusInstalled exercises the status command against a
// manager whose fake reports Installed=true. The output should no
// longer carry the "not installed" tail.
func TestMenubarStatusInstalled(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("menubar status full output is darwin-only")
	}
	fm := newFakeManager()
	fm.label = daemon.MenubarLabel
	fm.installed = true
	fm.running = true
	fm.pid = 5151
	fm.unitPath = "/tmp/fake/dev.biwashi.apogee.menubar.plist"
	withFakeMenubarManager(t, fm)

	var stdout, stderr bytes.Buffer
	root := NewRootCmd(&stdout, &stderr)
	root.SetArgs([]string{"menubar", "status"})
	if err := root.Execute(); err != nil {
		t.Fatalf("status: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "Menubar: "+daemon.MenubarLabel) {
		t.Errorf("status missing menubar header, got: %s", out)
	}
	if strings.Contains(out, "not installed") {
		t.Errorf("installed status should not render the 'not installed' hint, got: %s", out)
	}
}

// recordingMenubarManager wraps a fakeManager and invokes a callback
// on every Install call. We use this instead of mutating fakeManager
// so the other daemon_test.go users of fakeManager are unaffected.
type recordingMenubarManager struct {
	fake      *fakeManager
	onInstall func(daemon.Config)
}

func (r *recordingMenubarManager) Install(ctx context.Context, cfg daemon.Config) error {
	if r.onInstall != nil {
		r.onInstall(cfg)
	}
	return r.fake.Install(ctx, cfg)
}

func (r *recordingMenubarManager) Uninstall(ctx context.Context) error {
	return r.fake.Uninstall(ctx)
}
func (r *recordingMenubarManager) Start(ctx context.Context) error { return r.fake.Start(ctx) }
func (r *recordingMenubarManager) Stop(ctx context.Context) error  { return r.fake.Stop(ctx) }
func (r *recordingMenubarManager) Restart(ctx context.Context) error {
	return r.fake.Restart(ctx)
}

func (r *recordingMenubarManager) Status(ctx context.Context) (daemon.Status, error) {
	return r.fake.Status(ctx)
}
func (r *recordingMenubarManager) UnitPath() string { return r.fake.UnitPath() }
func (r *recordingMenubarManager) Label() string    { return r.fake.Label() }
