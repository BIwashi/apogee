// Package collector wires the apogee HTTP collector together: configuration,
// router, route handlers, and graceful shutdown.
package collector

import "time"

// Config holds the collector's runtime configuration. The zero value is
// sufficient for in-process tests; callers typically populate it from
// command-line flags.
type Config struct {
	// HTTPAddr is the transport identifier surfaced back on /v1/info and
	// used by Run() as the ListenAndServe address. For `apogee serve`
	// this is a real bind address like ":4100". For embedding hosts that
	// own their own transport (the Wails desktop shell, unit tests) it
	// is a display label such as "in-process (wails webview)" — the
	// field is read for its /v1/info representation but never dialed.
	HTTPAddr string
	// DBPath is the DuckDB DSN. Use ":memory:" for ephemeral storage.
	DBPath string

	// AutoRestart tells the upgrade watcher to automatically trigger a
	// daemon restart after a new binary is detected on disk (typical
	// trigger: `brew upgrade apogee`). Defaults to true: the operator
	// asked explicitly for "brew upgrade で勝手に更新される挙動" in the
	// original scoping conversation, so the daemon self-heals into the
	// new build without requiring a dashboard click.
	//
	// The dashboard still shows the upgrade banner during the grace
	// window so an operator can either click Restart now to skip the
	// countdown or watch the auto-restart timer fire on its own.
	AutoRestart bool

	// AutoRestartDelay is how long the upgrade watcher waits between
	// detecting a new version and actually calling the restart hook.
	// A small delay is enough to let a mid-flight Claude Code turn
	// finish (or at least make the operator aware the restart is
	// coming). Defaults to 3 minutes when zero.
	AutoRestartDelay time.Duration
}

// autoRestartDelay returns the configured delay, falling back to the
// package default when the caller left the field at zero.
func (c Config) autoRestartDelay() time.Duration {
	if c.AutoRestartDelay > 0 {
		return c.AutoRestartDelay
	}
	return defaultAutoRestartDelay
}

const defaultAutoRestartDelay = 3 * time.Minute
