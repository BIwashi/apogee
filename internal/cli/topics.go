package cli

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/BIwashi/apogee/internal/store/duckdb"
	"github.com/BIwashi/apogee/internal/summarizer"
)

// NewTopicsCmd builds the `apogee topics` parent command. The only
// subcommand at the moment is `backfill`; future commands (`list`,
// `merge`, `close`) hang off the same group so the operator surface
// stays organised.
func NewTopicsCmd(stdout, stderr io.Writer) *cobra.Command {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	cmd := &cobra.Command{
		Use:   "topics",
		Short: "Inspect and maintain the per-session topic tree",
		Long: `Topics are per-session conversation branches the per-turn classifier writes
into the topic_transitions / session_topics tables. Use the subcommands to
backfill historical sessions or to manage the tree by hand.`,
	}
	cmd.AddCommand(newTopicsBackfillCmd(stdout, stderr))
	return cmd
}

// newTopicsBackfillCmd registers `apogee topics backfill`. The
// command opens the local DuckDB store, builds a CLI runner that
// shells out to `claude -p`, and walks closed-turn-with-recap rows
// the live classifier missed.
func newTopicsBackfillCmd(stdout, stderr io.Writer) *cobra.Command {
	var (
		dbPath    string
		sessionID string
		limit     int
		force     bool
		dryRun    bool
		model     string
		cliPath   string
		verbose   bool
	)
	cmd := &cobra.Command{
		Use:   "backfill",
		Short: "Run the topic classifier on previously-recapped turns",
		Long: `Walks closed turns that already carry a per-turn recap and asks the
local claude CLI to classify each one into the session's topic tree.
Useful for sessions that pre-date the live classifier, or to recover
from a worker outage.

The backfill is single-threaded and chronological per session — each
turn sees the topics opened by every preceding turn in the same
session.

The classifier piggybacks on the operator's existing Claude Code CLI
authentication (no API key required).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			path, err := resolveBackfillDBPath(dbPath)
			if err != nil {
				return err
			}
			fmt.Fprintf(stdout, "topics backfill: opening %s\n", path)

			store, err := duckdb.Open(ctx, path)
			if err != nil {
				return fmt.Errorf("open duckdb: %w", err)
			}
			defer store.Close()

			level := slog.LevelInfo
			if verbose {
				level = slog.LevelDebug
			}
			logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level}))

			runner := summarizer.NewCLIRunner(cliPath, 0, logger)

			prefs := summarizer.Defaults()

			res, err := summarizer.BackfillTopics(ctx, store, runner, prefs, summarizer.BackfillOptions{
				SessionID: sessionID,
				Force:     force,
				Limit:     limit,
				Model:     model,
				DryRun:    dryRun,
			}, logger)
			if err != nil {
				return fmt.Errorf("backfill: %w", err)
			}

			fmt.Fprintln(stdout, "")
			fmt.Fprintln(stdout, "topics backfill: done")
			fmt.Fprintf(stdout, "  sessions:   %d\n", res.SessionsConsidered)
			fmt.Fprintf(stdout, "  considered: %d\n", res.TurnsConsidered)
			fmt.Fprintf(stdout, "  classified: %d\n", res.TurnsClassified)
			fmt.Fprintf(stdout, "  skipped:    %d\n", res.TurnsSkipped)
			fmt.Fprintf(stdout, "  errored:    %d\n", res.TurnsErrored)
			return nil
		},
	}

	cmd.Flags().StringVar(&dbPath, "db", "~/.apogee/apogee.duckdb", "DuckDB path; ~/ expanded")
	cmd.Flags().StringVar(&sessionID, "session", "", "Restrict to one session id (empty = every session)")
	cmd.Flags().IntVar(&limit, "limit", 0, "Stop after this many turns total (0 = no limit)")
	cmd.Flags().BoolVar(&force, "force", false, "Re-classify turns that already have topic_id")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Build prompts but do not call the LLM or write rows")
	cmd.Flags().StringVar(&model, "model", "", "Override the classifier model alias (default: cheapest UseCaseAgentSummary)")
	cmd.Flags().StringVar(&cliPath, "cli", "claude", "Path to the claude CLI binary")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "Enable debug logging")
	return cmd
}

// resolveBackfillDBPath expands a leading ~ and returns the absolute
// path. Empty input is treated as the default location.
func resolveBackfillDBPath(p string) (string, error) {
	if p == "" {
		p = "~/.apogee/apogee.duckdb"
	}
	if len(p) >= 2 && p[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve ~: %w", err)
		}
		p = filepath.Join(home, p[2:])
	}
	return p, nil
}
