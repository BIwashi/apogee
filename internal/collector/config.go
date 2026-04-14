// Package collector wires the apogee HTTP collector together: configuration,
// router, route handlers, and graceful shutdown.
package collector

// Config holds the collector's runtime configuration. The zero value is
// sufficient for in-process tests; callers typically populate it from
// command-line flags.
type Config struct {
	// HTTPAddr is the listen address, e.g. ":4100".
	HTTPAddr string
	// DBPath is the DuckDB DSN. Use ":memory:" for ephemeral storage.
	DBPath string
}
