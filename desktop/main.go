//go:build darwin

// Package main is the Apogee desktop shell — a Wails v2 entry point
// that wraps the same dashboard `apogee serve` hosts, rendered inside a
// native macOS WKWebView window.
//
// # Runtime modes
//
// The shell supports two modes, selected automatically at launch and
// overridable via the APOGEE_DESKTOP_MODE environment variable:
//
//   - proxy (default when the daemon is reachable): the Wails window is
//     a thin reverse-proxy wrapper around a running `apogee daemon` on
//     localhost:4100. No DuckDB is opened in the desktop process, no
//     collector is constructed, no worker goroutines are started. This
//     is the only mode that works correctly alongside Claude Code hooks
//     — operator interventions, summarizer recaps, HITL queue
//     responses, etc. all flow through the single daemon the hooks were
//     configured against at onboard time.
//
//   - embedded (escape hatch, APOGEE_DESKTOP_MODE=embedded): the desktop
//     process owns its own collector, DuckDB, and worker pool. Useful
//     for isolated sessions or for running without a daemon, but WILL
//     conflict with a running daemon over the DuckDB lock and will NOT
//     receive operator interventions from running Claude Code sessions
//     (those still talk to whatever URL .claude/settings.json was
//     configured with by `apogee onboard`, which is the daemon).
//
// # First-run bootstrap
//
// When auto mode detects no daemon, the shell shows a native Cocoa
// confirmation dialog (via osascript) offering to run `apogee onboard
// --yes` as a subprocess. If the user accepts, onboard installs and
// starts the daemon, the shell waits for it to become reachable, writes
// an ~/.apogee/installed-by-desktop marker so the cask uninstall path
// knows we own the daemon, and then transitions to proxy mode. If the
// user declines, the shell exits cleanly.
//
// # Non-darwin
//
// A stub entry point in main_other.go handles //go:build !darwin so
// `go build ./...` on linux/windows CI runners stays green and any
// attempt to actually run the binary there prints a clear "macOS only"
// message.
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

	mode := resolveMode()
	daemonAddr := resolveDaemonAddr()
	logger.Info("apogee desktop starting", "mode", mode, "daemon_addr", daemonAddr, "version_tag", "desktop")

	switch mode {
	case modeProxy:
		// User forced proxy mode. If the daemon is down, fail fast
		// rather than silently falling back — forcing proxy means
		// the caller has opinions about transport and we should
		// honour them.
		if !daemonReachable(daemonAddr) {
			showErrorDialog("apogee-desktop was launched with APOGEE_DESKTOP_MODE=proxy but the daemon at " + daemonAddr + " is not reachable. Start the daemon with `apogee daemon start` and try again.")
			return fmt.Errorf("proxy mode forced but daemon %s unreachable", daemonAddr)
		}
		return runProxy(logger, daemonAddr)

	case modeEmbedded:
		// User forced embedded mode. Skip the daemon probe and run
		// the in-process collector. Known to conflict with a live
		// daemon over DuckDB; documented in docs/desktop.md.
		return runEmbedded(logger)

	case modeAuto:
		// Default path: probe the daemon, proxy if it answers,
		// otherwise run the first-run bootstrap flow which ends in
		// proxy mode as well.
		if daemonReachable(daemonAddr) {
			return runProxy(logger, daemonAddr)
		}
		return runBootstrap(logger, daemonAddr)
	}

	return fmt.Errorf("unknown mode %q", mode)
}

const (
	modeAuto     = "auto"
	modeProxy    = "proxy"
	modeEmbedded = "embedded"
)

// resolveMode reads APOGEE_DESKTOP_MODE. Empty or unrecognised values
// fall back to auto. The enum is deliberately tiny — any expansion here
// should come with a docs update in docs/desktop.md.
func resolveMode() string {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("APOGEE_DESKTOP_MODE")))
	switch v {
	case modeProxy, modeEmbedded, modeAuto:
		return v
	default:
		return modeAuto
	}
}

// resolveDaemonAddr returns the host:port that proxy mode forwards to
// and that the daemon reachability probe hits. Defaults to the
// apogee-standard 127.0.0.1:4100; APOGEE_DAEMON_ADDR lets advanced
// users point at a different port (e.g. when running a second daemon
// for testing). Must NOT include a scheme — only host:port.
func resolveDaemonAddr() string {
	if v := strings.TrimSpace(os.Getenv("APOGEE_DAEMON_ADDR")); v != "" {
		return v
	}
	return "127.0.0.1:4100"
}
