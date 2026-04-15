// Package daemon abstracts the OS-level supervisor (launchd on macOS,
// systemd user units on Linux) so apogee can be installed as a
// background service that starts on login and is controllable via a
// handful of subcommands.
//
// The Manager interface is platform-agnostic. New() returns the
// concrete implementation for the current GOOS, selected via build
// tags on the unit_{darwin,linux,other}.go files.
//
// The manager never talks to the OS directly. Every call goes through
// a commandRunner so tests can inject fakes and avoid touching real
// launchctl / systemctl processes.
package daemon

import (
	"context"
	"errors"
	"time"
)

// DefaultLabel is the stable identifier used for the launchd plist and
// the systemd unit. Callers can override it via Config.Label.
const DefaultLabel = "dev.biwashi.apogee"

// DefaultAddr is the listen address baked into the unit file unless
// the caller overrides it.
const DefaultAddr = "127.0.0.1:4100"

// Manager is the platform-specific supervisor abstraction. Every
// method is safe to call even when the unit is not installed; the
// implementation reports the situation via ErrNotInstalled or a
// zero Status rather than panicking.
type Manager interface {
	// Install writes the unit file and loads it. Idempotent: if the
	// unit already exists with the same content, no-op. If it exists
	// with different content, returns ErrAlreadyInstalled unless
	// cfg.Force is set.
	Install(ctx context.Context, cfg Config) error

	// Uninstall stops the daemon if running and removes the unit
	// file. Idempotent: missing unit returns nil.
	Uninstall(ctx context.Context) error

	// Start asks the OS supervisor to launch the unit.
	Start(ctx context.Context) error

	// Stop asks the OS supervisor to terminate the unit.
	Stop(ctx context.Context) error

	// Restart is Stop + Start with a small grace period.
	Restart(ctx context.Context) error

	// Status reports what the OS supervisor knows about the unit.
	Status(ctx context.Context) (Status, error)

	// UnitPath returns the absolute path to the unit file (plist or
	// systemd service file). Useful for `apogee daemon status`.
	UnitPath() string

	// Label returns the unit's stable identifier (e.g.
	// "dev.biwashi.apogee").
	Label() string
}

// Config is the full configuration surface for installing the
// daemon. Fields mirror what a launchd plist or a systemd unit needs
// to describe a long-running user-scope service.
type Config struct {
	// Label is the stable identifier. Defaults to DefaultLabel when
	// empty. On launchd this is the <Label> key; on systemd this
	// becomes the service file's basename (<Label>.service) — well,
	// the linux implementation always uses "apogee.service" for a
	// human-friendly CLI, but the Label is still stored on the
	// Status result for display.
	Label string

	// BinaryPath is the absolute path to the apogee binary the unit
	// should exec. Defaults to the currently-running binary when
	// empty.
	BinaryPath string

	// Args are the arguments passed to the binary, not including the
	// binary itself. The typical set is
	// ["serve", "--addr", "127.0.0.1:4100", "--db", "~/.apogee/apogee.duckdb"].
	Args []string

	// WorkingDir is the directory the daemon is launched from. Most
	// installs set this to ~/.apogee.
	WorkingDir string

	// LogDir is where the unit's stdout/stderr are redirected. Two
	// files are written: apogee.out.log and apogee.err.log.
	LogDir string

	// Environment is passed through to the supervisor as
	// EnvironmentVariables (launchd) or Environment= (systemd). HOME
	// is always set to the user's home directory.
	Environment map[string]string

	// KeepAlive tells the supervisor to restart the daemon on exit.
	KeepAlive bool

	// RunAtLoad makes the supervisor launch the daemon the moment
	// the unit is loaded (launchd) or enabled (systemd with a
	// subsequent start).
	RunAtLoad bool

	// Force overwrites an existing unit file even when the content
	// differs. Without Force, a mismatched existing unit causes
	// Install to return ErrAlreadyInstalled.
	Force bool
}

// Status is the runtime report from the OS supervisor.
type Status struct {
	// Installed is true when the unit file exists on disk.
	Installed bool

	// Loaded is true when the supervisor has the unit in its
	// in-memory state: on launchd, when the label appears in
	// `launchctl list`; on systemd, when the unit is either enabled
	// or currently active.
	Loaded bool

	// Running is true when a process is actually up and serving.
	Running bool

	// PID is the current daemon PID, or 0 when not running.
	PID int

	// LastExitCode is the last observed exit code, or 0 when the
	// supervisor has not yet recorded one.
	LastExitCode int

	// StartedAt is when the current run started. Zero when unknown
	// or not running.
	StartedAt time.Time

	// UnitPath is the absolute path to the unit file.
	UnitPath string

	// Label mirrors the manager's Label for display.
	Label string
}

// Uptime returns the duration since StartedAt. Zero when the unit is
// not running or the start time is unknown.
func (s Status) Uptime() time.Duration {
	if s.StartedAt.IsZero() {
		return 0
	}
	return time.Since(s.StartedAt)
}

// ErrNotInstalled is returned when the unit file does not exist and
// the operation requires it (e.g. Start, Stop, Restart). Install is
// exempt and callers can check for this sentinel to render a
// friendly error.
var ErrNotInstalled = errors.New("daemon: not installed")

// ErrAlreadyInstalled is returned when a different unit already
// lives at the destination. Pass Config.Force = true to overwrite.
var ErrAlreadyInstalled = errors.New("daemon: already installed at destination")

// ErrNotSupported is returned by the stub manager on platforms that
// have neither launchd nor systemd. The CLI subcommands compile
// everywhere but return this error at runtime on Windows / bsd.
var ErrNotSupported = errors.New("daemon: not supported on this platform")

// commandRunner is the narrow subprocess interface used by the
// platform-specific implementations. Tests inject fakes to avoid
// touching the real launchctl / systemctl binaries.
type commandRunner interface {
	// Run executes name + args and returns (stdout, stderr, err).
	// The implementation is free to join stdout+stderr into one
	// stream — tests only care about the error and the combined
	// textual output.
	Run(ctx context.Context, name string, args ...string) (stdout string, stderr string, err error)
}

// execRunner is the production commandRunner that shells out via
// os/exec.
type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	return runExec(ctx, name, args...)
}
