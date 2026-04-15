package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/BIwashi/apogee/internal/daemon"
)

// isMenubarPlatform reports whether the current GOOS ships a
// working `apogee menubar` binary. Linux / windows do not, so the
// onboard wizard hides the menubar group and the install subcommand
// short-circuits with a warn line.
func isMenubarPlatform() bool {
	return runtime.GOOS == "darwin"
}

// menubarManagerFactory is indirected so menubar_install_test.go can
// inject a fake Manager without touching launchctl. Matches the
// shape of the daemon package's own NewManagerWithLabel so production
// callers never have to think about the seam.
var menubarManagerFactory = func() (daemon.Manager, error) {
	return daemon.NewManagerWithLabel(daemon.MenubarLabel)
}

// newMenubarInstallCmd builds `apogee menubar install`. On macOS it
// writes a second launchd plist under `dev.biwashi.apogee.menubar`
// whose ProgramArguments point at the currently-running apogee
// binary with the `menubar` subcommand. The unit is marked
// LSUIElement=true, LimitLoadToSessionType=Aqua, KeepAlive=false,
// RunAtLoad=true — see daemon.MenubarConfig for the rationale.
//
// Idempotent: a second `menubar install` without `--force` is a
// no-op (same plist content re-bootstraps the unit). A conflicting
// plist without `--force` returns a friendly "already installed
// (pass --force to overwrite)" error.
//
// On Linux / other platforms the command prints a styled warn line
// explaining macOS-only and exits 0 so the subcommand tree is
// discoverable via `apogee menubar --help` everywhere.
func newMenubarInstallCmd(stdout, stderr io.Writer) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Register `apogee menubar` as a macOS login item",
		Long: `Write a second launchd plist (dev.biwashi.apogee.menubar) under
~/Library/LaunchAgents so the menu bar companion starts automatically
at every login. The unit is independent from the collector daemon
unit — installing/uninstalling the menubar does not touch
dev.biwashi.apogee.

macOS only. On Linux the command prints a warn line and exits 0.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if runtime.GOOS != "darwin" {
				fmt.Fprintln(stdout, formatStatusLine("warn", "macOS only — apogee menubar is not supported on "+runtime.GOOS))
				return nil
			}
			m, err := menubarManagerFactory()
			if err != nil {
				return err
			}
			cfg, err := resolveMenubarConfig()
			if err != nil {
				return err
			}
			cfg.Force = force
			if err := m.Install(cmd.Context(), cfg); err != nil {
				if errors.Is(err, daemon.ErrAlreadyInstalled) {
					return fmt.Errorf("menubar already installed at %s (pass --force to overwrite)", m.UnitPath())
				}
				return err
			}
			printMenubarInstallSummary(stdout, m, cfg)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite an existing menubar plist with different content")
	return cmd
}

// newMenubarUninstallCmd builds `apogee menubar uninstall`. Removes
// the plist and boots it out of launchd. Idempotent: missing unit
// returns nil.
func newMenubarUninstallCmd(stdout, stderr io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Stop and remove the `apogee menubar` launchd unit",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if runtime.GOOS != "darwin" {
				fmt.Fprintln(stdout, formatStatusLine("warn", "macOS only — apogee menubar is not supported on "+runtime.GOOS))
				return nil
			}
			m, err := menubarManagerFactory()
			if err != nil {
				return err
			}
			if err := m.Uninstall(cmd.Context()); err != nil {
				return err
			}
			body := keyValueLines([][2]string{
				{"Label", m.Label()},
			})
			inner := styleHeading.Render("menubar uninstalled") + "\n\n" + body
			fmt.Fprintln(stdout, boxInfo.Render(inner))
			return nil
		},
	}
}

// newMenubarStatusCmd builds `apogee menubar status`. Mirrors
// `apogee daemon status` but for the secondary label. Does not
// probe the collector (the menubar is not an HTTP service).
func newMenubarStatusCmd(stdout, stderr io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the menubar launchd unit's runtime status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if runtime.GOOS != "darwin" {
				fmt.Fprintln(stdout, formatStatusLine("warn", "macOS only — apogee menubar is not supported on "+runtime.GOOS))
				return nil
			}
			m, err := menubarManagerFactory()
			if err != nil {
				return err
			}
			s, serr := m.Status(cmd.Context())
			if serr != nil && !errors.Is(serr, daemon.ErrNotSupported) {
				return serr
			}
			renderMenubarStatusBox(stdout, m, s)
			return nil
		},
	}
}

// renderMenubarStatusBox writes a single-section menubar-status box
// (no "Collector:" section — the menubar is a client, not a server).
func renderMenubarStatusBox(out io.Writer, m daemon.Manager, s daemon.Status) {
	fmt.Fprintln(out, renderHeading(fmt.Sprintf("Menubar: %s", labelOf(s, m))))
	fmt.Fprintln(out, daemonBox(m, s))
	if !s.Installed {
		fmt.Fprintln(out, styleMuted.Render("not installed — run `apogee menubar install` to register the login item."))
	}
}

// resolveMenubarConfig fills a daemon.Config with the menubar-
// specific defaults (LSUIElement, LimitLoadToSessionType=Aqua,
// KeepAlive=false) plus every absolute path the plist template
// needs: BinaryPath via os.Executable, WorkingDir/LogDir under
// ~/.apogee, and HOME environment for launchd.
func resolveMenubarConfig() (daemon.Config, error) {
	cfg := daemon.MenubarConfig()

	exe, err := os.Executable()
	if err != nil {
		return daemon.Config{}, fmt.Errorf("resolve binary path: %w", err)
	}
	if abs, err := filepath.Abs(exe); err == nil && abs != "" {
		cfg.BinaryPath = abs
	} else {
		cfg.BinaryPath = exe
	}

	logDir, err := expandHome("~/.apogee/logs")
	if err != nil {
		return daemon.Config{}, err
	}
	workDir, err := expandHome("~/.apogee")
	if err != nil {
		return daemon.Config{}, err
	}
	cfg.LogDir = logDir
	cfg.WorkingDir = workDir

	env := map[string]string{}
	if home, err := os.UserHomeDir(); err == nil {
		env["HOME"] = home
	}
	cfg.Environment = env

	return cfg, nil
}

// printMenubarInstallSummary writes the styled success block for
// `menubar install`. Matches the shape of printInstallSummary in
// daemon.go so the two units feel cohesive when installed back to
// back.
func printMenubarInstallSummary(out io.Writer, m daemon.Manager, cfg daemon.Config) {
	body := keyValueLines([][2]string{
		{"Label", m.Label()},
		{"Unit path", m.UnitPath()},
		{"Binary", cfg.BinaryPath},
		{"Args", "menubar"},
		{"Logs", cfg.LogDir + "/menubar.{out,err}.log"},
	})
	hint := "The menubar will launch automatically on next login. To start it now:\n  apogee menubar"
	inner := styleSuccess.Render(glyphCheck+" menubar installed") + "\n\n" + body + "\n\n" + styleMuted.Render(hint)
	fmt.Fprintln(out, boxSuccess.Render(inner))
}

