//go:build linux

package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderServiceFileGolden(t *testing.T) {
	cfg := Config{
		Label:      DefaultLabel,
		BinaryPath: "/usr/local/bin/apogee",
		Args: []string{
			"serve",
			"--addr", "127.0.0.1:4100",
			"--db", "/home/me/.apogee/apogee.duckdb",
		},
		WorkingDir: "/home/me/.apogee",
		LogDir:     "/home/me/.apogee/logs",
		KeepAlive:  true,
		RunAtLoad:  true,
	}
	got, err := renderServiceFile(cfg)
	if err != nil {
		t.Fatalf("renderServiceFile: %v", err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "expected_service.unit"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("service file mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestParseSystemctlShow(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "systemctl_show.txt"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var s Status
	parseSystemctlShow(string(raw), &s)
	if !s.Loaded {
		t.Errorf("expected Loaded=true")
	}
	if !s.Running {
		t.Errorf("expected Running=true")
	}
	if s.PID != 67890 {
		t.Errorf("expected PID=67890, got %d", s.PID)
	}
	if s.StartedAt.IsZero() {
		t.Errorf("expected StartedAt to be parsed")
	}
}

func systemdTestManager(t *testing.T, r commandRunner) *systemdManager {
	t.Helper()
	tmp := t.TempDir()
	return &systemdManager{
		runner:  r,
		homeDir: tmp,
		label:   DefaultLabel,
		unitDir: filepath.Join(tmp, ".config", "systemd", "user"),
	}
}

func TestSystemdInstallWritesUnit(t *testing.T) {
	r := &fakeRunner{}
	m := systemdTestManager(t, r)

	cfg := Config{
		BinaryPath: "/usr/local/bin/apogee",
		Args:       []string{"serve", "--addr", "127.0.0.1:4100"},
		WorkingDir: "/tmp/wd",
		LogDir:     "/tmp/logs",
		KeepAlive:  true,
	}
	if err := m.Install(context.Background(), cfg); err != nil {
		t.Fatalf("Install: %v", err)
	}
	data, err := os.ReadFile(m.UnitPath())
	if err != nil {
		t.Fatalf("read unit: %v", err)
	}
	if !strings.Contains(string(data), "ExecStart=/usr/local/bin/apogee serve --addr 127.0.0.1:4100") {
		t.Errorf("unit missing ExecStart: %s", data)
	}
	// Expect daemon-reload and enable to have been called.
	saw := map[string]bool{}
	for _, c := range r.calls {
		if c.name != "systemctl" {
			continue
		}
		for _, a := range c.args {
			saw[a] = true
		}
	}
	if !saw["daemon-reload"] {
		t.Errorf("expected daemon-reload call, got %+v", r.calls)
	}
	if !saw["enable"] {
		t.Errorf("expected enable call, got %+v", r.calls)
	}
}

func TestSystemdInstallIdempotent(t *testing.T) {
	r := &fakeRunner{}
	m := systemdTestManager(t, r)
	cfg := Config{BinaryPath: "/a", Args: []string{"serve"}}
	if err := m.Install(context.Background(), cfg); err != nil {
		t.Fatalf("install 1: %v", err)
	}
	if err := m.Install(context.Background(), cfg); err != nil {
		t.Fatalf("install 2 (idempotent): %v", err)
	}
}

func TestSystemdInstallConflictWithoutForce(t *testing.T) {
	r := &fakeRunner{}
	m := systemdTestManager(t, r)
	if err := m.Install(context.Background(), Config{BinaryPath: "/a", Args: []string{"serve"}}); err != nil {
		t.Fatalf("install 1: %v", err)
	}
	err := m.Install(context.Background(), Config{BinaryPath: "/b", Args: []string{"serve"}})
	if err != ErrAlreadyInstalled {
		t.Errorf("expected ErrAlreadyInstalled, got %v", err)
	}
}

func TestSystemdInstallForceOverwrites(t *testing.T) {
	r := &fakeRunner{}
	m := systemdTestManager(t, r)
	if err := m.Install(context.Background(), Config{BinaryPath: "/a", Args: []string{"serve"}}); err != nil {
		t.Fatalf("install 1: %v", err)
	}
	if err := m.Install(context.Background(), Config{BinaryPath: "/b", Args: []string{"serve"}, Force: true}); err != nil {
		t.Fatalf("force install: %v", err)
	}
	data, _ := os.ReadFile(m.UnitPath())
	if !strings.Contains(string(data), "/b serve") {
		t.Errorf("force install did not overwrite, got: %s", data)
	}
}

func TestSystemdStartNotInstalled(t *testing.T) {
	r := &fakeRunner{}
	m := systemdTestManager(t, r)
	if err := m.Start(context.Background()); err != ErrNotInstalled {
		t.Errorf("expected ErrNotInstalled, got %v", err)
	}
}

func TestSystemdUninstall(t *testing.T) {
	r := &fakeRunner{}
	m := systemdTestManager(t, r)
	if err := m.Install(context.Background(), Config{BinaryPath: "/a", Args: []string{"serve"}}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := m.Uninstall(context.Background()); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if _, err := os.Stat(m.UnitPath()); !os.IsNotExist(err) {
		t.Errorf("unit should be removed, stat err: %v", err)
	}
}
