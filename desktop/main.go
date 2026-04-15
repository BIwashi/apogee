//go:build darwin

// Package main is the Apogee desktop shell — a Wails v2 entry point
// that renders the apogee dashboard inside a native macOS WKWebView
// window, as a thin reverse-proxy wrapper around a running
// `apogee daemon`.
//
// # How it works
//
// On launch the shell probes the daemon at 127.0.0.1:4100 (override
// with APOGEE_DAEMON_ADDR). There are exactly two outcomes:
//
//   - Daemon reachable: the Wails AssetServer is wired to an
//     `httputil.ReverseProxy` pointing at the daemon, and the
//     window becomes a view of the daemon's collector. Nothing is
//     opened in the desktop process — no DuckDB, no collector, no
//     workers.
//
//   - Daemon unreachable: the shell runs the first-run bootstrap
//     flow (see desktop/bootstrap.go). A native Cocoa dialog asks
//     whether to set up apogee now, and on confirmation spawns
//     `apogee onboard --yes` as a subprocess. After the daemon
//     becomes reachable, the shell transitions into the exact same
//     reverse-proxy path above.
//
// The desktop shell never opens DuckDB or constructs a collector
// itself. That dual-owner topology caused v0.1.12's silent crash
// against running daemons (DuckDB is single-writer) and broke
// operator-intervention delivery (Claude Code hooks post to the
// daemon, not to the desktop's in-process collector), so the
// proxy-only model is the only model that stays consistent with the
// rest of the apogee stack.
//
// # Non-darwin
//
// A stub entry point in main_other.go handles //go:build !darwin so
// `go build ./...` on linux/windows CI runners stays green and any
// attempt to actually run the binary there prints a clear "macOS
// only" message.
package main

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "apogee desktop: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	daemonAddr := resolveDaemonAddr()
	logger.Info("apogee desktop starting", "daemon_addr", daemonAddr)

	if daemonReachable(daemonAddr) {
		return runProxy(logger, daemonAddr)
	}
	return runBootstrap(logger, daemonAddr)
}

// resolveDaemonAddr returns the host:port that the reverse proxy
// forwards to and that the daemon reachability probe hits. Defaults
// to the apogee-standard 127.0.0.1:4100; APOGEE_DAEMON_ADDR lets
// advanced users point at a different port (e.g. when running a
// second daemon for testing). Must NOT include a scheme — only
// host:port.
func resolveDaemonAddr() string {
	if v := strings.TrimSpace(os.Getenv("APOGEE_DAEMON_ADDR")); v != "" {
		return v
	}
	return "127.0.0.1:4100"
}
