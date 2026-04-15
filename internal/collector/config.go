// Package collector wires the apogee HTTP collector together: configuration,
// router, route handlers, and graceful shutdown.
package collector

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
}
