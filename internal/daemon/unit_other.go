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
	return &unsupportedManager{goos: runtime.GOOS, label: DefaultLabel}, nil
}

// NewWithRunner exists so test code (which may build for all
// platforms via the cross-platform daemon_test.go file) can still
// call it. The runner is ignored on this stub.
func NewWithRunner(_ commandRunner) (Manager, error) {
	return New()
}

// NewManagerWithLabel returns a stub manager pinned to the given
// label. Every mutating method still returns ErrNotSupported — the
// label is cosmetic so `menubar status` can render a friendly box
// even on unsupported platforms.
func NewManagerWithLabel(label string) (Manager, error) {
	if label == "" {
		label = DefaultLabel
	}
	return &unsupportedManager{goos: runtime.GOOS, label: label}, nil
}

type unsupportedManager struct {
	goos  string
	label string
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
	return Status{Label: m.label}, wrapUnsupported(m.goos)
}
func (m *unsupportedManager) UnitPath() string { return "" }
func (m *unsupportedManager) Label() string {
	if m.label == "" {
		return DefaultLabel
	}
	return m.label
}

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
