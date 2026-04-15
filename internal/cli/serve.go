package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/BIwashi/apogee/internal/collector"
	"github.com/BIwashi/apogee/internal/store/duckdb"
)

// ServeOptions is exported so tests (and alternative entry points) can
// construct a serve command with custom wiring.
type ServeOptions struct {
	Addr         string
	DBPath       string
	ConfigPath   string
	OTelEndpoint string
	LogLevel     string
}

// NewServeCmd returns the `apogee serve` subcommand. The command boots the
// collector (which is responsible for the HTTP surface, the DuckDB store,
// the SSE hub, and the embedded dashboard) and blocks until it receives
// SIGINT or SIGTERM.
func NewServeCmd() *cobra.Command {
	opts := ServeOptions{}

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the collector with the embedded dashboard",
		Long: `Run the apogee HTTP collector.

The collector ingests Claude Code hook events on POST /v1/events, writes
them into DuckDB, exposes a read API under /v1/*, pushes SSE updates, and
serves the embedded Next.js dashboard from the same origin.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(cmd.Context(), opts)
		},
	}

	cmd.Flags().StringVar(&opts.Addr, "addr", ":4100", "HTTP listen address")
	cmd.Flags().StringVar(&opts.DBPath, "db", "~/.apogee/apogee.duckdb", "DuckDB path (use :memory: for an ephemeral store)")
	cmd.Flags().StringVar(&opts.ConfigPath, "config", "~/.apogee/config.toml", "Optional TOML config path (telemetry + summarizer)")
	cmd.Flags().StringVar(&opts.OTelEndpoint, "otel-endpoint", os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"), "Override the OTLP endpoint (falls back to OTEL_EXPORTER_OTLP_ENDPOINT)")
	cmd.Flags().StringVar(&opts.LogLevel, "log-level", "info", "Logger level: debug | info | warn | error")

	return cmd
}

func runServe(ctx context.Context, opts ServeOptions) error {
	level := parseLogLevel(opts.LogLevel)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	// Propagate the OTLP endpoint override so the telemetry package picks
	// it up from the environment. We intentionally avoid plumbing a new
	// knob into collector.Config so the CLI flag stays equivalent to
	// setting the env var.
	if opts.OTelEndpoint != "" {
		if err := os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", opts.OTelEndpoint); err != nil {
			return fmt.Errorf("serve: set OTEL endpoint: %w", err)
		}
	}
	if opts.ConfigPath != "" {
		expanded, err := expandHome(opts.ConfigPath)
		if err == nil && expanded != "" {
			_ = os.Setenv("APOGEE_CONFIG", expanded)
		}
	}

	resolved, err := expandDBPath(opts.DBPath)
	if err != nil {
		return err
	}
	if resolved != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
			return fmt.Errorf("serve: ensure db dir: %w", err)
		}
	}

	runCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := duckdb.Open(runCtx, resolved)
	if err != nil {
		var locked *duckdb.LockedError
		if errors.As(err, &locked) {
			fmt.Fprintln(styledWriter(os.Stderr), renderDBLockConflict(locked))
			os.Exit(1)
		}
		return fmt.Errorf("serve: open duckdb: %w", err)
	}
	defer store.Close()

	srv := collector.New(collector.Config{HTTPAddr: opts.Addr, DBPath: resolved}, store, logger)
	if err := srv.Run(runCtx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func parseLogLevel(lvl string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(lvl)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error", "err":
		return slog.LevelError
	case "", "info":
		fallthrough
	default:
		return slog.LevelInfo
	}
}

// expandDBPath expands a leading ~ for the db path, preserving the special
// `:memory:` sentinel untouched.
func expandDBPath(p string) (string, error) {
	if p == ":memory:" {
		return p, nil
	}
	return expandHome(p)
}
