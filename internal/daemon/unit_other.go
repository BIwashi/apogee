//go:build !darwin && !linux

package daemon

import (
	"context"
	"runtime"
)

// New returns a stub manager that returns ErrNotSupported from every
// mutating method. Platforms without launchd or systemd-user still
// compile, so the CLI subcommands are always present; they just
// fail fast when invoked.
func New() (Manager, error) {
	return &unsupportedManager{goos: runtime.GOOS}, nil
}

// NewWithRunner exists so test code (which may build for all
// platforms via the cross-platform daemon_test.go file) can still
// call it. The runner is ignored on this stub.
func NewWithRunner(_ commandRunner) (Manager, error) {
	return New()
}

type unsupportedManager struct {
	goos string
}

func (m *unsupportedManager) Install(ctx context.Context, cfg Config) error {
	return wrapUnsupported(m.goos)
}
func (m *unsupportedManager) Uninstall(ctx context.Context) error {
	return wrapUnsupported(m.goos)
}
func (m *unsupportedManager) Start(ctx context.Context) error {
	return wrapUnsupported(m.goos)
}
func (m *unsupportedManager) Stop(ctx context.Context) error {
	return wrapUnsupported(m.goos)
}
func (m *unsupportedManager) Restart(ctx context.Context) error {
	return wrapUnsupported(m.goos)
}
func (m *unsupportedManager) Status(ctx context.Context) (Status, error) {
	return Status{Label: DefaultLabel}, wrapUnsupported(m.goos)
}
func (m *unsupportedManager) UnitPath() string { return "" }
func (m *unsupportedManager) Label() string    { return DefaultLabel }

func wrapUnsupported(goos string) error {
	return &unsupportedError{goos: goos}
}

type unsupportedError struct{ goos string }

func (e *unsupportedError) Error() string {
	return "daemon: not supported on " + e.goos
}

func (e *unsupportedError) Is(target error) bool {
	return target == ErrNotSupported
}
