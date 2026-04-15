package cli

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/BIwashi/apogee/internal/daemon"
)

// managerFactory is indirected so daemon_test.go can install a fake
// Manager without touching the real launchctl / systemctl binaries.
var managerFactory = daemon.New

// NewDaemonCmd builds the `apogee daemon` subcommand tree:
// install, uninstall, start, stop, restart, status.
func NewDaemonCmd(stdout, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the apogee background service",
		Long: `Install, start, stop, and inspect the apogee background service.

On macOS this wraps launchd via launchctl; on Linux it wraps
systemctl --user. Every subcommand is safe to run even when the
unit is not installed — it returns a friendly error instead of a
crash.`,
	}
	out := styledWriter(stdout)
	cmd.AddCommand(newDaemonInstallCmd(out, stderr))
	cmd.AddCommand(newDaemonUninstallCmd(out, stderr))
	cmd.AddCommand(newDaemonStartCmd(out, stderr))
	cmd.AddCommand(newDaemonStopCmd(out, stderr))
	cmd.AddCommand(newDaemonRestartCmd(out, stderr))
	cmd.AddCommand(newDaemonStatusCmd(out, stderr))
	return cmd
}

func newDaemonInstallCmd(stdout, stderr io.Writer) *cobra.Command {
	var (
		addr  string
		db    string
		label string
		force bool
	)
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Register the apogee background service",
		Long: `Write the platform unit file (launchd plist on macOS,
systemd user unit on Linux) and load it. Idempotent: an identical
existing unit is a no-op. Pass --force to overwrite a conflicting
unit.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			m, err := managerFactory()
			if err != nil {
				return err
			}
			cfg, err := resolveDaemonConfig(label, addr, db)
			if err != nil {
				return err
			}
			cfg.Force = force
			if err := m.Install(cmd.Context(), cfg); err != nil {
				if errors.Is(err, daemon.ErrAlreadyInstalled) {
					return fmt.Errorf("daemon already installed at %s (pass --force to overwrite)", m.UnitPath())
				}
				return err
			}
			printInstallSummary(stdout, m, cfg)
			return nil
		},
	}
	cmd.Flags().StringVar(&addr, "addr", daemon.DefaultAddr, "Collector listen address baked into the unit file")
	cmd.Flags().StringVar(&db, "db", "~/.apogee/apogee.duckdb", "DuckDB path baked into the unit file")
	cmd.Flags().StringVar(&label, "label", daemon.DefaultLabel, "Stable identifier for the unit (launchd Label / systemd unit name)")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite an existing unit file with different content")
	return cmd
}

func newDaemonUninstallCmd(stdout, stderr io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Stop the daemon and remove the unit file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			m, err := managerFactory()
			if err != nil {
				return err
			}
			if err := m.Uninstall(cmd.Context()); err != nil {
				return err
			}
			body := keyValueLines([][2]string{
				{"Label", m.Label()},
			})
			inner := styleHeading.Render("daemon uninstalled") + "\n\n" + body
			fmt.Fprintln(stdout, boxInfo.Render(inner))
			return nil
		},
	}
}

func newDaemonStartCmd(stdout, stderr io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the daemon",
		RunE: func(cmd *cobra.Command, _ []string) error {
			m, err := managerFactory()
			if err != nil {
				return err
			}
			if err := m.Start(cmd.Context()); err != nil {
				if errors.Is(err, daemon.ErrNotInstalled) {
					return friendlyNotInstalled()
				}
				return err
			}
			fmt.Fprintln(stdout, formatStatusLine("ok", fmt.Sprintf("daemon started (%s)", m.Label())))
			return nil
		},
	}
}

func newDaemonStopCmd(stdout, stderr io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the daemon",
		RunE: func(cmd *cobra.Command, _ []string) error {
			m, err := managerFactory()
			if err != nil {
				return err
			}
			if err := m.Stop(cmd.Context()); err != nil {
				if errors.Is(err, daemon.ErrNotInstalled) {
					return friendlyNotInstalled()
				}
				return err
			}
			fmt.Fprintln(stdout, formatStatusLine("ok", fmt.Sprintf("daemon stopped (%s)", m.Label())))
			return nil
		},
	}
}

func newDaemonRestartCmd(stdout, stderr io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart the daemon",
		RunE: func(cmd *cobra.Command, _ []string) error {
			m, err := managerFactory()
			if err != nil {
				return err
			}
			if err := m.Restart(cmd.Context()); err != nil {
				if errors.Is(err, daemon.ErrNotInstalled) {
					return friendlyNotInstalled()
				}
				return err
			}
			fmt.Fprintln(stdout, formatStatusLine("ok", fmt.Sprintf("daemon restarted (%s)", m.Label())))
			return nil
		},
	}
}

func newDaemonStatusCmd(stdout, stderr io.Writer) *cobra.Command {
	var addr string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the daemon's runtime status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			m, err := managerFactory()
			if err != nil {
				return err
			}
			s, serr := m.Status(cmd.Context())
			if serr != nil && !errors.Is(serr, daemon.ErrNotSupported) {
				return serr
			}
			renderDaemonStatusBox(stdout, m, s, addr)
			return nil
		},
	}
	cmd.Flags().StringVar(&addr, "addr", daemon.DefaultAddr, "Collector address to probe for /v1/healthz")
	return cmd
}

// renderDaemonStatusBox writes the styled two-section daemon-status
// page (Daemon box + Collector box) to out, plus the trailing "not
// installed" hint when applicable. Used by both `apogee daemon
// status` and the `apogee status` umbrella command.
func renderDaemonStatusBox(out io.Writer, m daemon.Manager, s daemon.Status, addr string) {
	fmt.Fprintln(out, renderHeading(fmt.Sprintf("Daemon: %s", labelOf(s, m))))
	fmt.Fprintln(out, daemonBox(m, s))
	if !s.Installed {
		fmt.Fprintln(out, styleMuted.Render("not installed — run `apogee daemon install` to register the service."))
		return
	}
	fmt.Fprintln(out)
	h := probeCollector(addr)
	fmt.Fprintln(out, renderHeading(fmt.Sprintf("Collector: http://%s", addr)))
	fmt.Fprintln(out, collectorBox(addr, h))
}

// labelOf returns the manager's label or the status label, whichever
// is non-empty, falling back to daemon.DefaultLabel.
func labelOf(s daemon.Status, m daemon.Manager) string {
	if s.Label != "" {
		return s.Label
	}
	if m != nil && m.Label() != "" {
		return m.Label()
	}
	return daemon.DefaultLabel
}

// daemonBox renders the Daemon section (info border for installed,
// muted box otherwise) using keyValueLines.
func daemonBox(m daemon.Manager, s daemon.Status) string {
	state := "not installed"
	switch {
	case s.Running:
		state = "running"
	case s.Installed:
		state = "stopped"
	}

	startedAt := "—"
	uptime := "—"
	if !s.StartedAt.IsZero() {
		startedAt = s.StartedAt.Format("2006-01-02 15:04:05")
		uptime = s.Uptime().Round(time.Second).String()
	}

	pid := "—"
	if s.PID > 0 {
		pid = fmt.Sprintf("%d", s.PID)
	}

	unitPath := s.UnitPath
	if unitPath == "" && m != nil {
		unitPath = m.UnitPath()
	}
	if unitPath == "" {
		unitPath = "—"
	}

	entries := [][2]string{
		{"Status", statusBadge(state)},
		{"Installed", boolBadge(s.Installed)},
		{"Loaded", boolBadge(s.Loaded)},
		{"Running", boolBadge(s.Running)},
		{"PID", pid},
		{"Started at", startedAt},
		{"Uptime", uptime},
		{"Last exit", fmt.Sprintf("%d", s.LastExitCode)},
		{"Unit path", unitPath},
		{"Logs", "~/.apogee/logs/apogee.{out,err}.log"},
	}
	body := keyValueLines(entries)
	if s.Installed {
		return boxInfo.Render(body)
	}
	return boxWarn.Render(body)
}

// boolBadge renders a boolean as styled "yes" / "no".
func boolBadge(b bool) string {
	if b {
		return styleSuccess.Render("yes")
	}
	return styleMuted.Render("no")
}

// collectorBox renders the Collector section. Border is success when
// the probe is OK, error otherwise.
func collectorBox(addr string, h collectorHealth) string {
	state := "ok"
	box := boxSuccess
	if h.Err != nil || !h.OK {
		state = "unreachable"
		box = boxError
	}
	healthLine := formatCollectorLine(h)
	latency := "—"
	if h.Latency > 0 {
		latency = h.Latency.Round(time.Millisecond).String()
	}
	body := keyValueLines([][2]string{
		{"Endpoint", "http://" + addr},
		{"Health", statusBadge(state)},
		{"Detail", healthLine},
		{"Latency", latency},
	})
	return box.Render(body)
}

// printInstallSummary writes the styled success block for `daemon
// install`. The glyph is a Unicode check mark (U+2713), not an
// emoji, so it satisfies the no-emoji design system rule.
func printInstallSummary(out io.Writer, m daemon.Manager, cfg daemon.Config) {
	body := keyValueLines([][2]string{
		{"Label", m.Label()},
		{"Unit path", m.UnitPath()},
		{"Collector", "http://" + trimAddr(cfg.Args)},
		{"Logs", cfg.LogDir + "/apogee.{out,err}.log"},
	})
	hint := "The daemon will start automatically on next login. To start it now:\n  apogee daemon start"
	inner := styleSuccess.Render(glyphCheck+" daemon installed") + "\n\n" + body + "\n\n" + styleMuted.Render(hint)
	fmt.Fprintln(out, boxSuccess.Render(inner))
}

func trimAddr(args []string) string {
	for i, a := range args {
		if a == "--addr" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return daemon.DefaultAddr
}

func friendlyNotInstalled() error {
	return errors.New("daemon is not installed; run `apogee daemon install` first")
}

// checkGlyph / crossGlyph are the Unicode check and ballot-X (U+2713,
// U+2717). They are NOT emoji per the design system and are used as
// inline status markers in CLI output.
const (
	checkGlyph = "\u2713"
	crossGlyph = "\u2717"
)

