package cli

import (
	"errors"
	"flag"
	"io"
	"os"

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
	root.AddCommand(newHooksCmd(stdout, stderr))
	root.AddCommand(NewVersionCmd(stdout))
	root.AddCommand(NewDoctorCmd(stdout))

	return root
}

// Execute is the process entry point wrapper used by cmd/apogee/main.go.
// It returns an error instead of calling os.Exit so the caller can decide
// how to render it.
func Execute(args []string, stdout, stderr io.Writer) error {
	root := NewRootCmd(stdout, stderr)
	root.SetArgs(args)
	return root.Execute()
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

// newHooksCmd builds the `apogee hooks` umbrella and attaches its verbs.
func newHooksCmd(stdout, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hooks",
		Short: "Manage the embedded Python hook library",
	}
	cmd.AddCommand(newHooksExtractCmd(stdout, stderr))
	return cmd
}

func newHooksExtractCmd(stdout, stderr io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:                "extract [flags]",
		Short:              "Write the embedded Python hook library to disk",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			return swallowFlagHelp(RunHooksExtract(args, stdout, stderr))
		},
	}
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
