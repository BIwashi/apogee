package cli

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"

	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"

	"github.com/BIwashi/apogee/internal/version"
)

// NewRootCmd builds the top-level `apogee` command and wires in every
// subcommand. It is constructed as a function (not a package-level var) so
// tests can instantiate fresh, unshared command trees.
//
// Output/error streams are injected so tests and alternative entry points
// can capture them without touching the global process state.
func NewRootCmd(stdout, stderr io.Writer) *cobra.Command {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}

	root := &cobra.Command{
		Use:   "apogee",
		Short: "Observability for multi-agent Claude Code sessions",
		Long: `apogee is a single-binary observability dashboard for multi-agent Claude Code sessions.

It captures every hook event, builds OpenTelemetry-shaped traces, persists
them in DuckDB, and ships a dark NASA-inspired Next.js dashboard embedded in
this very binary.`,
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	// Match the cobra convention of `apogee --version` printing just the
	// short string. `apogee version` (below) prints the full build info.
	root.SetVersionTemplate(version.Short() + "\n")

	root.AddCommand(NewServeCmd())
	root.AddCommand(newInitCmd(stdout, stderr))
	root.AddCommand(NewHookCmd())
	root.AddCommand(NewVersionCmd(stdout))
	root.AddCommand(NewDoctorCmd(stdout))
	root.AddCommand(NewDaemonCmd(stdout, stderr))
	root.AddCommand(NewStatusCmd(stdout, stderr))
	root.AddCommand(NewLogsCmd(stdout, stderr))
	root.AddCommand(NewOpenCmd(stdout, stderr))
	root.AddCommand(NewUninstallCmd(stdout, stderr))
	root.AddCommand(NewMenubarCmd(stdout, stderr))
	root.AddCommand(NewOnboardCmd(stdout, stderr))

	return root
}

// Execute is the process entry point wrapper used by cmd/apogee/main.go.
// It returns an error instead of calling os.Exit so the caller can decide
// how to render it. The root command is wrapped in charmbracelet/fang so
// `--help` and error output get styled section headers and lipgloss colors
// when stdout/stderr is a TTY, and degrades cleanly to plain text when
// piped.
func Execute(args []string, stdout, stderr io.Writer) error {
	root := NewRootCmd(stdout, stderr)
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	return fang.Execute(context.Background(), root)
}

// newInitCmd adapts the existing RunInit flag-driven helper into a cobra
// subcommand. The cobra wrapper parses nothing — it just forwards the raw
// arguments so the legacy flag.FlagSet inside RunInit keeps working
// unchanged, including flag docs, --help, and the dry-run plan output.
func newInitCmd(stdout, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:                "init [flags]",
		Short:              "Install apogee hooks into .claude/settings.json",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			return swallowFlagHelp(RunInit(args, stdout, stderr))
		},
	}
	return cmd
}

// swallowFlagHelp translates the stdlib flag.ErrHelp sentinel (returned when
// legacy flag.FlagSet-backed subcommands see `--help`) into a nil error so
// cobra does not print "apogee: flag: help requested" after the --help
// output we just wrote to stderr.
func swallowFlagHelp(err error) error {
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	return err
}
