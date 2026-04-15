//go:build darwin

package daemon

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"
)

// New returns the platform-specific Manager. On darwin this is the
// launchd-backed implementation. Production callers get a real
// launchctl runner; tests can use NewWithRunner to inject a fake.
func New() (Manager, error) {
	return NewWithRunner(execRunner{})
}

// NewWithRunner returns a launchd Manager backed by the supplied
// commandRunner. Tests inject a fake to avoid calling launchctl.
func NewWithRunner(r commandRunner) (Manager, error) {
	return newManagerWithLabelRunner(DefaultLabel, r)
}

// NewManagerWithLabel returns a launchd Manager pinned to the given
// label. Used by the `apogee menubar install` path to supervise a
// second launchd unit under MenubarLabel while reusing every Install
// / Uninstall / Start / Stop / Status / Restart codepath already
// exercised by the main daemon.
func NewManagerWithLabel(label string) (Manager, error) {
	return newManagerWithLabelRunner(label, execRunner{})
}

func newManagerWithLabelRunner(label string, r commandRunner) (Manager, error) {
	if label == "" {
		label = DefaultLabel
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("daemon: resolve home: %w", err)
	}
	return &launchdManager{
		runner:   r,
		homeDir:  home,
		label:    label,
		uid:      os.Getuid(),
		plistDir: filepath.Join(home, "Library", "LaunchAgents"),
	}, nil
}

// launchdManager is the darwin implementation. Every call goes
// through the injected commandRunner; the only direct OS syscalls
// are filesystem reads/writes for the plist file.
type launchdManager struct {
	runner   commandRunner
	homeDir  string
	label    string
	uid      int
	plistDir string
}

func (m *launchdManager) Label() string { return m.label }

func (m *launchdManager) UnitPath() string {
	return filepath.Join(m.plistDir, m.label+".plist")
}

func (m *launchdManager) serviceTarget() string {
	return fmt.Sprintf("gui/%d/%s", m.uid, m.label)
}

func (m *launchdManager) domain() string {
	return fmt.Sprintf("gui/%d", m.uid)
}

// Install writes the plist atomically and bootstraps it into
// launchd. Idempotent: an identical existing plist is a no-op, and
// an already-loaded unit treats bootstrap's "already exists" error
// as success.
func (m *launchdManager) Install(ctx context.Context, cfg Config) error {
	if cfg.Label != "" {
		m.label = cfg.Label
	}
	plist, err := renderPlist(m.label, cfg)
	if err != nil {
		return err
	}
	path := m.UnitPath()

	if existing, err := os.ReadFile(path); err == nil {
		if bytes.Equal(existing, plist) {
			// Same content. Re-bootstrap just in case the unit
			// was booted out since the last install.
			_ = m.bootstrap(ctx, path)
			return nil
		}
		if !cfg.Force {
			return ErrAlreadyInstalled
		}
		// Force: bootout the old one first so we can replace it.
		_, _, _ = m.runner.Run(ctx, "launchctl", "bootout", m.serviceTarget())
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("daemon: read existing plist: %w", err)
	}

	if err := os.MkdirAll(m.plistDir, 0o755); err != nil {
		return fmt.Errorf("daemon: mkdir %s: %w", m.plistDir, err)
	}
	if err := writeFileAtomic(path, plist, 0o644); err != nil {
		return fmt.Errorf("daemon: write plist: %w", err)
	}
	if cfg.LogDir != "" {
		_ = os.MkdirAll(cfg.LogDir, 0o755)
	}
	if cfg.WorkingDir != "" {
		_ = os.MkdirAll(cfg.WorkingDir, 0o755)
	}
	return m.bootstrap(ctx, path)
}

func (m *launchdManager) bootstrap(ctx context.Context, plistPath string) error {
	_, stderr, err := m.runner.Run(ctx, "launchctl", "bootstrap", m.domain(), plistPath)
	if err == nil {
		return nil
	}
	// "service already loaded" is treated as success.
	combined := strings.ToLower(stderr)
	if strings.Contains(combined, "already") || strings.Contains(combined, "exists") {
		return nil
	}
	return fmt.Errorf("daemon: launchctl bootstrap: %w: %s", err, strings.TrimSpace(stderr))
}

func (m *launchdManager) Uninstall(ctx context.Context) error {
	// Best-effort bootout; missing unit is ignored.
	_, _, _ = m.runner.Run(ctx, "launchctl", "bootout", m.serviceTarget())
	path := m.UnitPath()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("daemon: remove plist: %w", err)
	}
	return nil
}

func (m *launchdManager) Start(ctx context.Context) error {
	if _, err := os.Stat(m.UnitPath()); os.IsNotExist(err) {
		return ErrNotInstalled
	}
	_, stderr, err := m.runner.Run(ctx, "launchctl", "kickstart", m.serviceTarget())
	if err != nil {
		return fmt.Errorf("daemon: launchctl kickstart: %w: %s", err, strings.TrimSpace(stderr))
	}
	return nil
}

func (m *launchdManager) Stop(ctx context.Context) error {
	if _, err := os.Stat(m.UnitPath()); os.IsNotExist(err) {
		return ErrNotInstalled
	}
	// `kill SIGTERM` leaves the unit loaded; KeepAlive may restart
	// it. For a user-facing "stop", we bootout instead so the
	// process actually stays down until the next start.
	_, stderr, err := m.runner.Run(ctx, "launchctl", "bootout", m.serviceTarget())
	if err != nil {
		// Already booted out is fine.
		if strings.Contains(strings.ToLower(stderr), "could not find") {
			return nil
		}
		return fmt.Errorf("daemon: launchctl bootout: %w: %s", err, strings.TrimSpace(stderr))
	}
	return nil
}

func (m *launchdManager) Restart(ctx context.Context) error {
	if _, err := os.Stat(m.UnitPath()); os.IsNotExist(err) {
		return ErrNotInstalled
	}
	// First make sure the unit is loaded. bootstrap is a no-op if
	// already present.
	_ = m.bootstrap(ctx, m.UnitPath())
	_, stderr, err := m.runner.Run(ctx, "launchctl", "kickstart", "-k", m.serviceTarget())
	if err != nil {
		return fmt.Errorf("daemon: launchctl kickstart -k: %w: %s", err, strings.TrimSpace(stderr))
	}
	return nil
}

func (m *launchdManager) Status(ctx context.Context) (Status, error) {
	s := Status{
		Label:    m.label,
		UnitPath: m.UnitPath(),
	}
	if _, err := os.Stat(s.UnitPath); err == nil {
		s.Installed = true
	} else if !os.IsNotExist(err) {
		return s, err
	}

	stdout, _, err := m.runner.Run(ctx, "launchctl", "print", m.serviceTarget())
	if err != nil {
		// Not loaded — that's fine, return what we have.
		return s, nil
	}
	parseLaunchctlPrint(stdout, &s)
	return s, nil
}

// parseLaunchctlPrint extracts pid / state / last exit from the
// loose textual output of `launchctl print gui/<uid>/<label>`. The
// format is stable enough between macOS versions to regex-match.
func parseLaunchctlPrint(output string, s *Status) {
	s.Loaded = true
	if m := rePID.FindStringSubmatch(output); len(m) == 2 {
		if pid, err := strconv.Atoi(m[1]); err == nil && pid > 0 {
			s.PID = pid
			s.Running = true
		}
	}
	if m := reState.FindStringSubmatch(output); len(m) == 2 {
		state := strings.TrimSpace(m[1])
		if state == "running" {
			s.Running = true
		}
	}
	if m := reLastExit.FindStringSubmatch(output); len(m) == 2 {
		if code, err := strconv.Atoi(m[1]); err == nil {
			s.LastExitCode = code
		}
	}
	// launchctl print doesn't include a stable start timestamp; the
	// Status.StartedAt is left zero on darwin. `apogee daemon
	// status` surfaces the fact by omitting the line rather than
	// printing a bogus value.
}

var (
	rePID      = regexp.MustCompile(`(?m)^\s*pid\s*=\s*(\d+)`)
	reState    = regexp.MustCompile(`(?m)^\s*state\s*=\s*(\w+)`)
	reLastExit = regexp.MustCompile(`(?m)^\s*last exit code\s*=\s*(-?\d+)`)
)

// renderPlist writes the launchd plist body for the given Config.
// The XML is static apart from the ProgramArguments, environment,
// paths, and KeepAlive / RunAtLoad / LSUIElement /
// LimitLoadToSessionType flags.
func renderPlist(label string, cfg Config) ([]byte, error) {
	if label == "" {
		label = DefaultLabel
	}
	if cfg.BinaryPath == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("daemon: resolve binary path: %w", err)
		}
		cfg.BinaryPath = exe
	}
	program := append([]string{cfg.BinaryPath}, cfg.Args...)
	env := cfg.Environment
	if env == nil {
		env = map[string]string{}
	}
	if _, ok := env["HOME"]; !ok {
		if home, err := os.UserHomeDir(); err == nil {
			env["HOME"] = home
		}
	}
	base := cfg.LogFileBase
	if base == "" {
		base = "apogee"
	}
	logDir := emptyDefault(cfg.LogDir, filepath.Join(env["HOME"], ".apogee", "logs"))
	data := plistData{
		Label:                  label,
		ProgramArguments:       program,
		WorkingDir:             cfg.WorkingDir,
		EnvKeys:                sortedEnvKeys(env),
		EnvValues:              env,
		RunAtLoad:              cfg.RunAtLoad,
		KeepAlive:              cfg.KeepAlive,
		StdoutPath:             filepath.Join(logDir, base+".out.log"),
		StderrPath:             filepath.Join(logDir, base+".err.log"),
		LSUIElement:            cfg.LSUIElement,
		LimitLoadToSessionType: cfg.LimitLoadToSessionType,
	}
	var buf bytes.Buffer
	if err := plistTmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("daemon: render plist: %w", err)
	}
	return buf.Bytes(), nil
}

func emptyDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func sortedEnvKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// stable order so the golden plist matches byte-for-byte.
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

type plistData struct {
	Label                  string
	ProgramArguments       []string
	WorkingDir             string
	EnvKeys                []string
	EnvValues              map[string]string
	RunAtLoad              bool
	KeepAlive              bool
	StdoutPath             string
	StderrPath             string
	LSUIElement            bool
	LimitLoadToSessionType string
}

// plistTmpl is the launchd plist template. Indentation uses tabs
// inside the dict entries to match the existing Apple tooling style.
// The trailing newline is intentional — launchctl accepts either,
// but golden fixtures need a stable terminator.
var plistTmpl = template.Must(template.New("plist").Funcs(template.FuncMap{
	"xml": xmlEscape,
}).Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>{{xml .Label}}</string>
	<key>ProgramArguments</key>
	<array>
{{- range .ProgramArguments}}
		<string>{{xml .}}</string>
{{- end}}
	</array>
{{- if .WorkingDir}}
	<key>WorkingDirectory</key>
	<string>{{xml .WorkingDir}}</string>
{{- end}}
{{- if .EnvKeys}}
	<key>EnvironmentVariables</key>
	<dict>
{{- range .EnvKeys}}
		<key>{{xml .}}</key>
		<string>{{xml (index $.EnvValues .)}}</string>
{{- end}}
	</dict>
{{- end}}
	<key>RunAtLoad</key>
	<{{if .RunAtLoad}}true{{else}}false{{end}}/>
	<key>KeepAlive</key>
	<{{if .KeepAlive}}true{{else}}false{{end}}/>
	<key>StandardOutPath</key>
	<string>{{xml .StdoutPath}}</string>
	<key>StandardErrorPath</key>
	<string>{{xml .StderrPath}}</string>
{{- if .LSUIElement}}
	<key>LSUIElement</key>
	<true/>
{{- end}}
{{- if .LimitLoadToSessionType}}
	<key>LimitLoadToSessionType</key>
	<string>{{xml .LimitLoadToSessionType}}</string>
{{- end}}
	<key>ProcessType</key>
	<string>{{if .LSUIElement}}Interactive{{else}}Background{{end}}</string>
</dict>
</plist>
`))

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xmlEscapeInto(&b, s)
	return b.String()
}

func xmlEscapeInto(b *bytes.Buffer, s string) error {
	// Minimal escape for PLIST <string> payloads: & < >.
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	_, err := b.WriteString(replacer.Replace(s))
	return err
}

// writeFileAtomic is a tiny local copy of internal/cli/fsutil.go's
// writeFileAtomic so the daemon package does not depend on internal/cli.
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

