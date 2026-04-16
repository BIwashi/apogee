//go:build darwin

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/BIwashi/apogee/internal/version"
)

// runBootstrap is called when the daemon is not reachable at launch.
// It first checks whether the daemon service is already installed
// (launchd knows about dev.biwashi.apogee) — if so, it tries to
// start the service without asking the user to re-onboard. Only when
// the service doesn't exist at all does it offer full first-run setup.
//
// Flow (existing service):
//  1. Kick the launchd service via `launchctl kickstart`.
//  2. Poll /v1/healthz until the daemon answers.
//  3. Fall through to runProxy.
//
// Flow (first-run):
//  1. Confirm with a native Cocoa dialog (osascript).
//  2. Post a "Setting up apogee…" notification.
//  3. Spawn `apogee onboard --yes` as a subprocess.
//  4. Poll /v1/healthz until the daemon answers.
//  5. Touch ~/.apogee/installed-by-desktop marker.
//  6. Fall through to runProxy.
func runBootstrap(logger *slog.Logger, daemonAddr string) error {
	// Fast path: the daemon service is registered with launchd but
	// just isn't running right now (e.g. after a reboot before the
	// KeepAlive fires, or the user manually unloaded it). Kick it
	// back up without showing a setup dialog.
	if launchdServiceExists() {
		logger.Info("bootstrap: launchd service exists, kicking it")
		if err := kickLaunchdService(logger); err != nil {
			logger.Warn("bootstrap: kickstart failed, falling through to full onboard", "err", err)
		} else {
			if err := waitForDaemon(daemonAddr, 10*time.Second); err == nil {
				logger.Info("bootstrap: daemon came up after kickstart")
				return runProxy(logger, daemonAddr)
			}
			logger.Warn("bootstrap: daemon did not come up after kickstart, falling through to full onboard")
		}
	}

	ok := showConfirmDialog(
		"apogee is not set up on this machine yet. Set it up now?\n\n"+
			"This will:\n"+
			"  • Install the apogee daemon as a launchd user service\n"+
			"  • Register Claude Code hooks at user scope\n"+
			"  • Configure the summarizer to use your local claude CLI\n\n"+
			"You can always re-run `apogee onboard` from a terminal later.",
		"Set up",
	)
	if !ok {
		logger.Info("bootstrap: user declined first-run setup, exiting")
		return nil
	}

	// Drop a marker NOW so that even if onboard partially completes
	// and the user kills the process, the cask uninstall path has a
	// breadcrumb that this machine was touched by the desktop shell.
	// We rewrite the marker once onboard fully succeeds so it
	// carries the actual version stamp.
	if err := writeInstalledByDesktopMarker("pending"); err != nil {
		logger.Warn("bootstrap: could not write installed-by-desktop marker", "err", err)
	}

	showNotification("Apogee", "Setting up apogee… this takes about 30 seconds.")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := runOnboardSubprocess(ctx, logger); err != nil {
		showErrorDialog("apogee setup failed:\n\n" + err.Error() + "\n\nOpen Terminal.app and run `apogee onboard` manually to see the full output.")
		return fmt.Errorf("onboard subprocess: %w", err)
	}

	logger.Info("bootstrap: onboard completed, waiting for daemon to become reachable", "addr", daemonAddr)
	if err := waitForDaemon(daemonAddr, 30*time.Second); err != nil {
		showErrorDialog("apogee setup completed but the daemon at " + daemonAddr + " is still not reachable after 30 seconds.\n\nTry running `apogee daemon status` from Terminal to investigate.")
		return err
	}

	if err := writeInstalledByDesktopMarker(version.Version); err != nil {
		logger.Warn("bootstrap: could not finalise installed-by-desktop marker", "err", err)
	}

	logger.Info("bootstrap: setup complete, entering proxy mode")
	return runProxy(logger, daemonAddr)
}

// launchdServiceExists checks whether the dev.biwashi.apogee service
// is known to launchd. Uses `launchctl print` which exits 0 when the
// service exists (running or not) and non-zero otherwise.
func launchdServiceExists() bool {
	uid := fmt.Sprintf("%d", os.Getuid())
	return exec.Command("launchctl", "print", "gui/"+uid+"/dev.biwashi.apogee").Run() == nil
}

// kickLaunchdService tries to start the daemon via launchctl kickstart.
func kickLaunchdService(logger *slog.Logger) error {
	uid := fmt.Sprintf("%d", os.Getuid())
	target := "gui/" + uid + "/dev.biwashi.apogee"
	out, err := exec.Command("launchctl", "kickstart", target).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl kickstart %s: %s: %w", target, string(out), err)
	}
	logger.Info("bootstrap: kickstart succeeded", "target", target)
	return nil
}

// runOnboardSubprocess execs `apogee onboard --yes` and waits for it
// to finish. Stdout and stderr are wired to the desktop process's
// own stdout/stderr so the output is visible in Console.app (or in a
// terminal if the user launched the .app from `open -a`).
func runOnboardSubprocess(ctx context.Context, logger *slog.Logger) error {
	bin, err := findApogeeBinary()
	if err != nil {
		return fmt.Errorf("apogee CLI not found (is BIwashi/tap/apogee installed?): %w", err)
	}
	logger.Info("bootstrap: running onboard", "bin", bin)

	cmd := exec.CommandContext(ctx, bin, "onboard", "--yes")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// Belt and braces: even though --yes is set, the env var path
	// is also supported by onboard and makes the non-interactive
	// intent obvious in process lists.
	cmd.Env = append(os.Environ(), "APOGEE_ONBOARD_NONINTERACTIVE=1")
	return cmd.Run()
}

// daemonReachable issues a short-timeout GET to /v1/healthz. Returns
// true on 200, false on anything else (dial error, non-200 status,
// timeout). Kept intentionally narrow — no retries, no body parsing —
// because the caller handles the retry loop (waitForDaemon).
func daemonReachable(addr string) bool {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get("http://" + addr + "/v1/healthz")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// waitForDaemon polls daemonReachable every 500ms until it returns
// true or the deadline passes. Used right after the onboard
// subprocess finishes, because launchd-registered units take a beat
// to actually bind their listen socket.
func waitForDaemon(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if daemonReachable(addr) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("daemon %s did not become reachable within %s", addr, timeout)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// writeInstalledByDesktopMarker touches ~/.apogee/installed-by-desktop
// with the given tag (version or "pending"). The Homebrew cask's
// uninstall_preflight hook checks for this file to decide whether the
// daemon was installed BY the desktop shell (and therefore the cask
// is responsible for tearing it down) or by the CLI (in which case
// the cask leaves it alone).
func writeInstalledByDesktopMarker(tag string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".apogee")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, "installed-by-desktop")
	return os.WriteFile(path, []byte(tag+"\n"), 0o600)
}

// showConfirmDialog pops a modal native confirmation dialog via
// osascript. Returns true if the user clicked the primary button,
// false on Cancel or any dialog error. osascript exits with code 1
// when the user clicks Cancel, which `exec.Command.Run` reports as a
// non-nil error — that is a legitimate "user said no", not a failure.
func showConfirmDialog(message, okLabel string) bool {
	script := fmt.Sprintf(
		`display dialog %s buttons {"Cancel", %s} default button %s cancel button "Cancel" with title "Apogee" with icon note`,
		applescriptString(message),
		applescriptString(okLabel),
		applescriptString(okLabel),
	)
	out, err := exec.Command("osascript", "-e", script).CombinedOutput()
	if err != nil {
		// Either the user clicked Cancel (exit code 1 with
		// "User canceled" on stderr) or osascript is unavailable
		// for some reason. Either way, treat it as "no".
		return false
	}
	return strings.Contains(string(out), "button returned:"+okLabel)
}

// showErrorDialog pops a modal error dialog. Best-effort: if
// osascript fails (e.g. sandboxed environment) the message is logged
// to stderr instead.
func showErrorDialog(message string) {
	script := fmt.Sprintf(
		`display dialog %s buttons {"OK"} default button "OK" with title "Apogee" with icon stop`,
		applescriptString(message),
	)
	if err := exec.Command("osascript", "-e", script).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "apogee desktop error:", message)
	}
}

// showNotification posts a Notification Center toast. Non-blocking.
// Notifications require the parent app bundle to have a
// CFBundleIdentifier — Apogee.app does, so this works from the
// installed cask. Standalone `go run ./desktop` may see the
// notification swallowed if the binary is not wrapped in a bundle.
func showNotification(title, message string) {
	script := fmt.Sprintf(
		`display notification %s with title %s`,
		applescriptString(message),
		applescriptString(title),
	)
	_ = exec.Command("osascript", "-e", script).Run()
}

// findApogeeBinary locates the apogee CLI binary. macOS GUI apps
// launched from Finder or Dock inherit a minimal PATH
// (/usr/bin:/bin:/usr/sbin:/sbin) that excludes Homebrew prefixes.
// We check PATH first, then probe well-known Homebrew install
// locations so the bootstrap flow works even when the shell PATH
// isn't inherited.
func findApogeeBinary() (string, error) {
	if bin, err := exec.LookPath("apogee"); err == nil {
		return bin, nil
	}
	// Well-known Homebrew paths on Apple Silicon and Intel Macs.
	for _, candidate := range []string{
		"/opt/homebrew/bin/apogee",
		"/usr/local/bin/apogee",
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("apogee binary not found in PATH or Homebrew prefixes (/opt/homebrew/bin, /usr/local/bin)")
}

// applescriptString escapes a Go string into an AppleScript string
// literal. AppleScript uses double quotes and backslash escaping for
// \" and \\ — mirroring JSON quoting is close enough for our use
// cases (plain-text dialog messages with no control characters).
func applescriptString(s string) string {
	// Escape backslashes first, then quotes.
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
