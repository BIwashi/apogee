package cli

import (
	"errors"
	"fmt"
	"io"

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
	cmd.AddCommand(newDaemonInstallCmd(stdout, stderr))
	cmd.AddCommand(newDaemonUninstallCmd(stdout, stderr))
	cmd.AddCommand(newDaemonStartCmd(stdout, stderr))
	cmd.AddCommand(newDaemonStopCmd(stdout, stderr))
	cmd.AddCommand(newDaemonRestartCmd(stdout, stderr))
	cmd.AddCommand(newDaemonStatusCmd(stdout, stderr))
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
			fmt.Fprintf(stdout, "%s daemon uninstalled (%s)\n", checkGlyph, m.Label())
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
			fmt.Fprintf(stdout, "%s daemon started (%s)\n", checkGlyph, m.Label())
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
			fmt.Fprintf(stdout, "%s daemon stopped (%s)\n", checkGlyph, m.Label())
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
			fmt.Fprintf(stdout, "%s daemon restarted (%s)\n", checkGlyph, m.Label())
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
			fmt.Fprint(stdout, daemon.FormatStatus(s))
			if s.Installed {
				fmt.Fprintf(stdout, "  Logs:         ~/.apogee/logs/apogee.{out,err}.log\n")
			} else {
				fmt.Fprintf(stdout, "\n%s not installed — run `apogee daemon install` to register the service.\n", crossGlyph)
				return nil
			}

			h := probeCollector(addr)
			fmt.Fprintln(stdout)
			fmt.Fprintf(stdout, "Collector: http://%s\n", addr)
			fmt.Fprintf(stdout, "  Health:       %s\n", formatCollectorLine(h))
			return nil
		},
	}
	cmd.Flags().StringVar(&addr, "addr", daemon.DefaultAddr, "Collector address to probe for /v1/healthz")
	return cmd
}

// printInstallSummary writes the styled success block for `daemon
// install`. The glyph is a Unicode check mark (U+2713), not an
// emoji, so it satisfies the no-emoji design system rule.
func printInstallSummary(out io.Writer, m daemon.Manager, cfg daemon.Config) {
	fmt.Fprintf(out, "%s daemon installed\n", checkGlyph)
	fmt.Fprintf(out, "  Label:        %s\n", m.Label())
	fmt.Fprintf(out, "  Unit path:    %s\n", m.UnitPath())
	fmt.Fprintf(out, "  Collector:    http://%s\n", trimAddr(cfg.Args))
	fmt.Fprintf(out, "  Logs:         %s/apogee.{out,err}.log\n", cfg.LogDir)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "The daemon will start automatically on next login. To start it now:")
	fmt.Fprintln(out, "  apogee daemon start")
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

