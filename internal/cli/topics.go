package cli

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/BIwashi/apogee/internal/store/duckdb"
	"github.com/BIwashi/apogee/internal/summarizer"
)

// NewTopicsCmd builds the `apogee topics` parent command. The
// subcommand surface is intentionally narrow: `list` and `show`
// for inspection, `backfill` for the offline classifier. Future
// commands (`merge`, `close`, `forget`) will hang off the same
// group so the operator surface stays organised.
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
inspect the tree per session, backfill historical sessions, or to manage
the tree by hand.`,
	}
	cmd.AddCommand(newTopicsBackfillCmd(stdout, stderr))
	cmd.AddCommand(newTopicsListCmd(stdout, stderr))
	cmd.AddCommand(newTopicsShowCmd(stdout, stderr))
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

// newTopicsListCmd registers `apogee topics list`. Per-session
// catalog: one row per session that has any classifier output, with
// the active topic goal, classified-vs-total turn counts, and
// low-confidence (unknown) counts. Useful for "did the classifier
// run?" sanity checks before diving into Mission UI.
func newTopicsListCmd(stdout, stderr io.Writer) *cobra.Command {
	var (
		dbPath string
		limit  int
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List sessions with classifier output and their active topic",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			path, err := resolveBackfillDBPath(dbPath)
			if err != nil {
				return err
			}
			store, err := duckdb.Open(ctx, path)
			if err != nil {
				return fmt.Errorf("open duckdb: %w", err)
			}
			defer store.Close()

			rows, err := store.ListSessionTopicSummaries(ctx, limit)
			if err != nil {
				return err
			}
			if len(rows) == 0 {
				fmt.Fprintln(stdout, "No sessions with classified topics yet.")
				return nil
			}

			// Tab-aligned single-line-per-session output. We do not
			// pull in a tablewriter dep — short-form CLI tables are
			// fine with text/tabwriter from stdlib.
			tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "SESSION\tSOURCE\tTOPICS\tOPEN\tTURNS\tCLASS\tLOWCONF\tACTIVE GOAL\tLAST SEEN")
			for _, r := range rows {
				active := "-"
				if r.ActiveGoal.Valid && r.ActiveGoal.String != "" {
					active = truncateForCLI(r.ActiveGoal.String, 60)
				}
				fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%d\t%d\t%d\t%s\t%s\n",
					shortID(r.SessionID),
					orDash(r.SourceApp),
					r.TopicCount,
					r.OpenTopicCount,
					r.TurnsTotal,
					r.TurnsClassified,
					r.TurnsLowConfidence,
					active,
					r.LastSeenAt.Local().Format("2006-01-02 15:04"),
				)
			}
			_ = tw.Flush()
			_ = stderr // keep parity with other CLI commands
			return nil
		},
	}
	cmd.Flags().StringVar(&dbPath, "db", "~/.apogee/apogee.duckdb", "DuckDB path; ~/ expanded")
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum number of sessions to print (0 = no limit)")
	return cmd
}

// newTopicsShowCmd registers `apogee topics show <session-id>`.
// Prints the topic forest + audit trail for one session so an
// operator can sanity-check the classifier's per-turn decisions
// without dropping into the DuckDB shell.
func newTopicsShowCmd(stdout, stderr io.Writer) *cobra.Command {
	var (
		dbPath          string
		showTransitions bool
	)
	cmd := &cobra.Command{
		Use:   "show <session-id>",
		Short: "Print the topic forest + transition audit trail for one session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			sessionID := args[0]
			path, err := resolveBackfillDBPath(dbPath)
			if err != nil {
				return err
			}
			store, err := duckdb.Open(ctx, path)
			if err != nil {
				return fmt.Errorf("open duckdb: %w", err)
			}
			defer store.Close()

			topics, err := store.ListSessionTopics(ctx, sessionID)
			if err != nil {
				return err
			}
			fmt.Fprintf(stdout, "Session: %s\n", sessionID)
			if len(topics) == 0 {
				fmt.Fprintln(stdout, "No topics recorded for this session yet.")
				return nil
			}
			fmt.Fprintf(stdout, "Topics: %d\n\n", len(topics))

			// Build a lookup so we can render parent-child indents.
			parents := map[string]string{}
			for _, t := range topics {
				if t.ParentTopicID.Valid && t.ParentTopicID.String != "" {
					parents[t.TopicID] = t.ParentTopicID.String
				}
			}

			for _, t := range topics {
				prefix := ""
				if _, ok := parents[t.TopicID]; ok {
					prefix = "  ↳ "
				}
				state := "OPEN"
				if t.ClosedAt.Valid {
					state = "CLOSED"
				}
				fmt.Fprintf(stdout, "%s%s  [%s]\n", prefix, t.TopicID, state)
				fmt.Fprintf(stdout, "    Goal:      %s\n", t.Goal)
				fmt.Fprintf(stdout, "    Opened:    %s\n", t.OpenedAt.Local().Format("2006-01-02 15:04:05"))
				fmt.Fprintf(stdout, "    Last seen: %s\n", t.LastSeenAt.Local().Format("2006-01-02 15:04:05"))
				if parentID, ok := parents[t.TopicID]; ok {
					fmt.Fprintf(stdout, "    Parent:    %s\n", parentID)
				}
				fmt.Fprintln(stdout, "")
			}

			if !showTransitions {
				return nil
			}

			tr, err := store.ListTopicTransitions(ctx, sessionID, 0)
			if err != nil {
				return err
			}
			if len(tr) == 0 {
				fmt.Fprintln(stdout, "No transitions recorded.")
				return nil
			}
			fmt.Fprintf(stdout, "Transitions: %d\n\n", len(tr))
			tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "#\tTURN\tKIND\tCONF\tFROM\tTO\tMODEL")
			for i, x := range tr {
				conf := "-"
				if x.Confidence.Valid {
					conf = fmt.Sprintf("%.2f", x.Confidence.Float64)
				}
				from := "-"
				if x.FromTopicID.Valid {
					from = x.FromTopicID.String
				}
				to := "-"
				if x.ToTopicID.Valid {
					to = x.ToTopicID.String
				}
				fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
					i+1,
					shortID(x.TurnID),
					x.Kind,
					conf,
					from,
					to,
					x.Model,
				)
			}
			_ = tw.Flush()
			_ = stderr
			return nil
		},
	}
	cmd.Flags().StringVar(&dbPath, "db", "~/.apogee/apogee.duckdb", "DuckDB path; ~/ expanded")
	cmd.Flags().BoolVar(&showTransitions, "transitions", true, "Also print the per-turn transition audit trail")
	return cmd
}

// shortID returns the first 12 characters of a long id so CLI tables
// stay narrow without losing uniqueness.
func shortID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}

// orDash returns "-" for empty strings so the tabwriter never prints
// a stray empty cell.
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// truncateForCLI clamps a string to maxLen runes with an ellipsis.
// Reused by the topics list command so long goals do not wrap.
func truncateForCLI(s string, maxLen int) string {
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	return string(r[:maxLen-1]) + "…"
}
