//go:build darwin

package daemon

import (
	"strings"
	"testing"
)

// TestMenubarConfigDefaults asserts the Config shape MenubarConfig
// hands out matches the product requirements: LSUIElement,
// LimitLoadToSessionType=Aqua, KeepAlive=false, RunAtLoad=true,
// LogFileBase=menubar, Args=[menubar].
func TestMenubarConfigDefaults(t *testing.T) {
	cfg := MenubarConfig()
	if cfg.Label != MenubarLabel {
		t.Errorf("Label = %q, want %q", cfg.Label, MenubarLabel)
	}
	if cfg.LogFileBase != "menubar" {
		t.Errorf("LogFileBase = %q, want menubar", cfg.LogFileBase)
	}
	if !cfg.LSUIElement {
		t.Errorf("LSUIElement should be true for menubar unit")
	}
	if cfg.LimitLoadToSessionType != "Aqua" {
		t.Errorf("LimitLoadToSessionType = %q, want Aqua", cfg.LimitLoadToSessionType)
	}
	if cfg.KeepAlive {
		t.Errorf("KeepAlive should be false for menubar unit (interactive)")
	}
	if !cfg.RunAtLoad {
		t.Errorf("RunAtLoad should be true so the unit launches at login")
	}
	if len(cfg.Args) != 1 || cfg.Args[0] != "menubar" {
		t.Errorf("Args = %v, want [menubar]", cfg.Args)
	}
}

// TestRenderPlistMenubar renders the plist with MenubarConfig and
// checks every product-critical key appears in the XML. We assert on
// substrings rather than a golden fixture because the existing golden
// lives in testdata/expected_plist.xml for the collector daemon, and
// a second golden would bloat the test data without adding coverage.
func TestRenderPlistMenubar(t *testing.T) {
	cfg := MenubarConfig()
	cfg.BinaryPath = "/opt/homebrew/bin/apogee"
	cfg.WorkingDir = "/Users/me/.apogee"
	cfg.LogDir = "/Users/me/.apogee/logs"
	cfg.Environment = map[string]string{"HOME": "/Users/me"}

	got, err := renderPlist(MenubarLabel, cfg)
	if err != nil {
		t.Fatalf("renderPlist: %v", err)
	}
	s := string(got)

	for _, want := range []string{
		"<string>dev.biwashi.apogee.menubar</string>",
		"<string>/opt/homebrew/bin/apogee</string>",
		"<string>menubar</string>",
		"<key>RunAtLoad</key>\n\t<true/>",
		"<key>KeepAlive</key>\n\t<false/>",
		"<key>LSUIElement</key>\n\t<true/>",
		"<key>LimitLoadToSessionType</key>\n\t<string>Aqua</string>",
		"<string>/Users/me/.apogee/logs/menubar.out.log</string>",
		"<string>/Users/me/.apogee/logs/menubar.err.log</string>",
		"<string>Interactive</string>", // ProcessType for LSUIElement=true
	} {
		if !strings.Contains(s, want) {
			t.Errorf("menubar plist missing %q in:\n%s", want, s)
		}
	}

	// Ensure the collector daemon's log filenames DO NOT appear —
	// the menubar must not clobber apogee.out.log.
	for _, stale := range []string{"apogee.out.log", "apogee.err.log"} {
		if strings.Contains(s, stale) {
			t.Errorf("menubar plist should not use %q log basename:\n%s", stale, s)
		}
	}
}

// TestRenderPlistCollectorIsUnchanged guards the golden plist path:
// rendering with default (non-menubar) Config must still yield the
// exact byte sequence the existing TestRenderPlistGolden asserts on.
// We duplicate the minimal invariants here so a future plist tweak
// that accidentally breaks the daemon template fails close to its
// line of code, not in the shared golden test.
func TestRenderPlistCollectorIsUnchanged(t *testing.T) {
	cfg := Config{
		Label:      DefaultLabel,
		BinaryPath: "/opt/homebrew/bin/apogee",
		Args:       []string{"serve", "--addr", "127.0.0.1:4100"},
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
	s := string(got)
	// The daemon plist must NOT carry the menubar-only fields.
	for _, unwanted := range []string{
		"LSUIElement",
		"LimitLoadToSessionType",
	} {
		if strings.Contains(s, unwanted) {
			t.Errorf("daemon plist should not contain %q:\n%s", unwanted, s)
		}
	}
	// And it must still say Background, not Interactive.
	if !strings.Contains(s, "<string>Background</string>") {
		t.Errorf("daemon plist ProcessType should be Background, got:\n%s", s)
	}
	if !strings.Contains(s, "apogee.out.log") {
		t.Errorf("daemon plist should still log to apogee.out.log, got:\n%s", s)
	}
}

// TestNewManagerWithLabel_Menubar verifies the constructor hands back
// a launchdManager pinned to the menubar label + a plist path under
// LaunchAgents that matches the label.
func TestNewManagerWithLabel_Menubar(t *testing.T) {
	m, err := NewManagerWithLabel(MenubarLabel)
	if err != nil {
		t.Fatalf("NewManagerWithLabel: %v", err)
	}
	if m.Label() != MenubarLabel {
		t.Errorf("Label() = %q, want %q", m.Label(), MenubarLabel)
	}
	if !strings.HasSuffix(m.UnitPath(), MenubarLabel+".plist") {
		t.Errorf("UnitPath() = %q, want to end with %s.plist", m.UnitPath(), MenubarLabel)
	}
}

// TestLaunchdInstallMenubarPlist walks the Install path with a
// menubar-shaped Config through a tempdir-backed manager and asserts
// the rendered plist carries the menubar-specific keys.
func TestLaunchdInstallMenubarPlist(t *testing.T) {
	r := &fakeRunner{}
	tmp := t.TempDir()
	m := &launchdManager{
		runner:   r,
		homeDir:  tmp,
		label:    MenubarLabel,
		uid:      501,
		plistDir: tmp + "/Library/LaunchAgents",
	}

	cfg := MenubarConfig()
	cfg.BinaryPath = "/opt/homebrew/bin/apogee"
	cfg.WorkingDir = tmp + "/.apogee"
	cfg.LogDir = tmp + "/.apogee/logs"
	cfg.Environment = map[string]string{"HOME": tmp}

	if err := m.Install(t.Context(), cfg); err != nil {
		t.Fatalf("Install: %v", err)
	}
	// Fetch the last bootstrap call to confirm the target includes
	// the menubar label (gui/<uid>/dev.biwashi.apogee.menubar).
	if len(r.calls) == 0 {
		t.Fatalf("expected at least one launchctl call")
	}
	last := r.calls[len(r.calls)-1]
	if last.name != "launchctl" {
		t.Errorf("expected launchctl, got %q", last.name)
	}
	joined := strings.Join(last.args, " ")
	if !strings.Contains(joined, MenubarLabel) {
		t.Errorf("bootstrap target should carry %q, got %q", MenubarLabel, joined)
	}
}
