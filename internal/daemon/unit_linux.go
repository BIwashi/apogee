//go:build linux

package daemon

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"
)

// serviceFileName is the systemd unit basename. We intentionally
// keep it short and human-friendly instead of using the full Label
// so CLI users can invoke `systemctl --user status apogee.service`
// without typing the reverse-dns prefix.
const serviceFileName = "apogee.service"

// New returns the systemd user manager.
func New() (Manager, error) {
	return NewWithRunner(execRunner{})
}

// NewManagerWithLabel returns a Manager pinned to the given label.
// On Linux the menubar is not supported, so callers that pass
// MenubarLabel receive a stub manager whose Install/Uninstall/Start/
// Stop/Restart/Status all return ErrNotSupported. Other labels fall
// back to the systemd-user manager the same way New() does.
func NewManagerWithLabel(label string) (Manager, error) {
	if label == MenubarLabel {
		return &unsupportedMenubarManager{}, nil
	}
	m, err := NewWithRunner(execRunner{})
	if err != nil {
		return nil, err
	}
	if sm, ok := m.(*systemdManager); ok && label != "" {
		sm.label = label
	}
	return m, nil
}

// unsupportedMenubarManager is the stub used on Linux for
// MenubarLabel. Every mutating method returns ErrNotSupported;
// Status returns a zero Status with the label filled in so the CLI
// `menubar status` subcommand can still render a friendly "not
// installed" box.
type unsupportedMenubarManager struct{}

func (unsupportedMenubarManager) Install(context.Context, Config) error {
	return ErrNotSupported
}
func (unsupportedMenubarManager) Uninstall(context.Context) error { return ErrNotSupported }
func (unsupportedMenubarManager) Start(context.Context) error     { return ErrNotSupported }
func (unsupportedMenubarManager) Stop(context.Context) error      { return ErrNotSupported }
func (unsupportedMenubarManager) Restart(context.Context) error   { return ErrNotSupported }
func (unsupportedMenubarManager) Status(context.Context) (Status, error) {
	return Status{Label: MenubarLabel}, nil
}
func (unsupportedMenubarManager) UnitPath() string { return "" }
func (unsupportedMenubarManager) Label() string    { return MenubarLabel }

// NewWithRunner returns a Linux Manager backed by the supplied
// commandRunner. Tests inject a fake.
func NewWithRunner(r commandRunner) (Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("daemon: resolve home: %w", err)
	}
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		dir = filepath.Join(home, ".config")
	}
	unitDir := filepath.Join(dir, "systemd", "user")
	return &systemdManager{
		runner:  r,
		homeDir: home,
		label:   DefaultLabel,
		unitDir: unitDir,
	}, nil
}

type systemdManager struct {
	runner  commandRunner
	homeDir string
	label   string
	unitDir string
}

func (m *systemdManager) Label() string { return m.label }

func (m *systemdManager) UnitPath() string {
	return filepath.Join(m.unitDir, serviceFileName)
}

func (m *systemdManager) Install(ctx context.Context, cfg Config) error {
	if cfg.Label != "" {
		m.label = cfg.Label
	}
	unit, err := renderServiceFile(cfg)
	if err != nil {
		return err
	}
	path := m.UnitPath()

	if existing, err := os.ReadFile(path); err == nil {
		if bytes.Equal(existing, unit) {
			_, _, _ = m.runner.Run(ctx, "systemctl", "--user", "daemon-reload")
			_, _, _ = m.runner.Run(ctx, "systemctl", "--user", "enable", serviceFileName)
			return nil
		}
		if !cfg.Force {
			return ErrAlreadyInstalled
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("daemon: read existing service: %w", err)
	}

	if err := os.MkdirAll(m.unitDir, 0o755); err != nil {
		return fmt.Errorf("daemon: mkdir %s: %w", m.unitDir, err)
	}
	if err := writeFileAtomic(path, unit, 0o644); err != nil {
		return fmt.Errorf("daemon: write service file: %w", err)
	}
	if cfg.LogDir != "" {
		_ = os.MkdirAll(cfg.LogDir, 0o755)
	}
	if cfg.WorkingDir != "" {
		_ = os.MkdirAll(cfg.WorkingDir, 0o755)
	}
	if _, stderr, err := m.runner.Run(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("daemon: systemctl daemon-reload: %w: %s", err, strings.TrimSpace(stderr))
	}
	if _, stderr, err := m.runner.Run(ctx, "systemctl", "--user", "enable", serviceFileName); err != nil {
		return fmt.Errorf("daemon: systemctl enable: %w: %s", err, strings.TrimSpace(stderr))
	}
	return nil
}

func (m *systemdManager) Uninstall(ctx context.Context) error {
	_, _, _ = m.runner.Run(ctx, "systemctl", "--user", "disable", "--now", serviceFileName)
	if err := os.Remove(m.UnitPath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("daemon: remove service: %w", err)
	}
	_, _, _ = m.runner.Run(ctx, "systemctl", "--user", "daemon-reload")
	return nil
}

func (m *systemdManager) Start(ctx context.Context) error {
	if _, err := os.Stat(m.UnitPath()); os.IsNotExist(err) {
		return ErrNotInstalled
	}
	if _, stderr, err := m.runner.Run(ctx, "systemctl", "--user", "start", serviceFileName); err != nil {
		return fmt.Errorf("daemon: systemctl start: %w: %s", err, strings.TrimSpace(stderr))
	}
	return nil
}

func (m *systemdManager) Stop(ctx context.Context) error {
	if _, err := os.Stat(m.UnitPath()); os.IsNotExist(err) {
		return ErrNotInstalled
	}
	if _, stderr, err := m.runner.Run(ctx, "systemctl", "--user", "stop", serviceFileName); err != nil {
		return fmt.Errorf("daemon: systemctl stop: %w: %s", err, strings.TrimSpace(stderr))
	}
	return nil
}

func (m *systemdManager) Restart(ctx context.Context) error {
	if _, err := os.Stat(m.UnitPath()); os.IsNotExist(err) {
		return ErrNotInstalled
	}
	if _, stderr, err := m.runner.Run(ctx, "systemctl", "--user", "restart", serviceFileName); err != nil {
		return fmt.Errorf("daemon: systemctl restart: %w: %s", err, strings.TrimSpace(stderr))
	}
	return nil
}

func (m *systemdManager) Status(ctx context.Context) (Status, error) {
	s := Status{
		Label:    m.label,
		UnitPath: m.UnitPath(),
	}
	if _, err := os.Stat(s.UnitPath); err == nil {
		s.Installed = true
	} else if !os.IsNotExist(err) {
		return s, err
	}

	stdout, _, err := m.runner.Run(ctx, "systemctl", "--user", "show", serviceFileName,
		"--property=ActiveState",
		"--property=SubState",
		"--property=MainPID",
		"--property=ExecMainStartTimestamp",
		"--property=ExecMainCode",
		"--property=ExecMainStatus",
		"--property=LoadState",
	)
	if err != nil {
		return s, nil
	}
	parseSystemctlShow(stdout, &s)
	return s, nil
}

// parseSystemctlShow reads Key=Value lines from `systemctl --user
// show` output and fills the Status struct. Unknown / missing
// properties leave the field at its zero value.
func parseSystemctlShow(output string, s *Status) {
	kv := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimRight(line, "\r")
		if idx := strings.Index(line, "="); idx > 0 {
			kv[line[:idx]] = line[idx+1:]
		}
	}
	if v := kv["LoadState"]; v == "loaded" {
		s.Loaded = true
	}
	if v := kv["ActiveState"]; v == "active" || v == "activating" {
		s.Loaded = true
	}
	if v := kv["SubState"]; v == "running" {
		s.Running = true
	}
	if v := kv["MainPID"]; v != "" && v != "0" {
		if pid, err := strconv.Atoi(v); err == nil && pid > 0 {
			s.PID = pid
		}
	}
	if v := kv["ExecMainStatus"]; v != "" {
		if code, err := strconv.Atoi(v); err == nil {
			s.LastExitCode = code
		}
	}
	if v := kv["ExecMainStartTimestamp"]; v != "" && v != "0" {
		// systemd format: "Wed 2026-04-15 01:42:13 UTC"
		// Try a couple of layouts, leave zero on failure.
		layouts := []string{
			"Mon 2006-01-02 15:04:05 MST",
			"Mon 2006-01-02 15:04:05 -0700",
		}
		for _, l := range layouts {
			if t, err := time.Parse(l, v); err == nil {
				s.StartedAt = t
				break
			}
		}
	}
	if s.PID == 0 {
		s.Running = false
	}
}

func renderServiceFile(cfg Config) ([]byte, error) {
	if cfg.BinaryPath == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("daemon: resolve binary path: %w", err)
		}
		cfg.BinaryPath = exe
	}
	workingDir := cfg.WorkingDir
	if workingDir == "" {
		workingDir = "%h/.apogee"
	}
	logDir := cfg.LogDir
	if logDir == "" {
		logDir = "%h/.apogee/logs"
	}
	env := applyDefaultEnv(cfg.Environment)
	envKeys := sortedKeys(env)

	data := serviceData{
		Description: "apogee collector for Claude Code sessions",
		ExecStart:   strings.TrimSpace(cfg.BinaryPath + " " + strings.Join(cfg.Args, " ")),
		WorkingDir:  workingDir,
		Restart:     cfg.KeepAlive,
		StdoutPath:  filepath.Join(logDir, "apogee.out.log"),
		StderrPath:  filepath.Join(logDir, "apogee.err.log"),
		EnvKeys:     envKeys,
		EnvValues:   env,
	}
	var buf bytes.Buffer
	if err := serviceTmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("daemon: render service file: %w", err)
	}
	return buf.Bytes(), nil
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

type serviceData struct {
	Description string
	ExecStart   string
	WorkingDir  string
	Restart     bool
	StdoutPath  string
	StderrPath  string
	EnvKeys     []string
	EnvValues   map[string]string
}

var serviceTmpl = template.Must(template.New("service").Parse(`[Unit]
Description={{.Description}}
After=network.target

[Service]
Type=simple
ExecStart={{.ExecStart}}
WorkingDirectory={{.WorkingDir}}
{{- range .EnvKeys}}
Environment={{.}}={{index $.EnvValues .}}
{{- end}}
{{- if .Restart}}
Restart=on-failure
RestartSec=3
{{- else}}
Restart=no
{{- end}}
StandardOutput=append:{{.StdoutPath}}
StandardError=append:{{.StderrPath}}

[Install]
WantedBy=default.target
`))

// writeFileAtomic is duplicated here to keep internal/daemon free of
// cross-package deps.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".apogee-daemon-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
