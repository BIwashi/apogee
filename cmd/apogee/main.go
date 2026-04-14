// Command apogee is the single-binary entry point for the apogee
// observability dashboard. PR #2 introduces the `serve` subcommand which runs
// the HTTP collector backed by DuckDB. Without arguments the command prints
// the build version, preserving the scaffold behaviour until PR #10 swaps in
// cobra-based dispatch.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/BIwashi/apogee/internal/cli"
	"github.com/BIwashi/apogee/internal/collector"
	"github.com/BIwashi/apogee/internal/store/duckdb"
	"github.com/BIwashi/apogee/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "apogee:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	// `-version` flag short-circuit.
	for _, a := range args {
		if a == "-version" || a == "--version" {
			fmt.Printf("apogee %s\n", version.Version)
			return nil
		}
	}
	if len(args) == 0 {
		fmt.Printf("apogee %s\n", version.Version)
		return nil
	}
	switch args[0] {
	case "serve":
		return runServe(args[1:])
	case "init":
		return cli.RunInit(args[1:], os.Stdout, os.Stderr)
	case "hooks":
		return cli.RunHooks(args[1:], os.Stdout, os.Stderr)
	case "version":
		fmt.Printf("apogee %s\n", version.Version)
		return nil
	default:
		return fmt.Errorf("unknown subcommand %q (try `apogee serve`, `apogee init`, `apogee hooks extract`, or `apogee version`)", args[0])
	}
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", ":4100", "HTTP listen address")
	dbPath := fs.String("db", "~/.apogee/apogee.duckdb", "DuckDB path (use :memory: for ephemeral)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	resolved, err := expandPath(*dbPath)
	if err != nil {
		return err
	}
	if resolved != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
			return fmt.Errorf("ensure db dir: %w", err)
		}
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := duckdb.Open(ctx, resolved)
	if err != nil {
		return err
	}
	defer store.Close()

	srv := collector.New(collector.Config{HTTPAddr: *addr, DBPath: resolved}, store, logger)
	if err := srv.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// expandPath substitutes a leading ~ for the user's home directory.
func expandPath(p string) (string, error) {
	if p == ":memory:" {
		return p, nil
	}
	if strings.HasPrefix(p, "~/") || p == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, strings.TrimPrefix(p, "~")), nil
	}
	return p, nil
}
