//go:build darwin

// Package main is the Apogee desktop shell — a Wails v2 entry point that
// wraps the same collector + embedded Next.js dashboard that `apogee serve`
// hosts, but renders it inside a native macOS window instead of a browser
// tab. It shares the Go module with cmd/apogee so refactors land in one
// place; only the process entry and the surrounding window chrome are new.
//
// A non-darwin stub lives in main_other.go so `go build ./...` on
// linux/windows CI runners prints a clear "macOS only" error instead of
// pulling the WKWebView bindings.
//
// Architecture:
//
//	DuckDB store ──▶ collector.New ──▶ Server.Router (chi.Router, http.Handler)
//	                                      │
//	                                      ▼
//	                        Wails AssetServer.Handler
//	                                      │
//	                                      ▼
//	                              WKWebView (native)
//
// No extra TCP listener is opened in the desktop process — the Wails
// AssetServer dispatches WebView requests straight into the chi router, so
// /v1/* API calls and the embedded SPA are served from the same in-process
// handler the HTTP serve command uses.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"

	"github.com/BIwashi/apogee/internal/collector"
	"github.com/BIwashi/apogee/internal/store/duckdb"
	"github.com/BIwashi/apogee/internal/version"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "apogee desktop: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	dbPath, err := resolveDBPath()
	if err != nil {
		return err
	}
	if dbPath != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
			return fmt.Errorf("ensure db dir: %w", err)
		}
	}

	// Open the store and build the collector up front — this is the same
	// wiring `apogee serve` uses, minus the HTTP listener. The Wails
	// AssetServer will dispatch WebView requests straight into the router
	// we get back from the Server, so we never bind a TCP port.
	bootCtx := context.Background()
	store, err := duckdb.Open(bootCtx, dbPath)
	if err != nil {
		return fmt.Errorf("open duckdb %q: %w", dbPath, err)
	}

	srv := collector.New(
		collector.Config{HTTPAddr: "", DBPath: dbPath},
		store,
		logger,
	)

	// The workers (metrics sampler, summarizer, HITL ticker, intervention
	// sweeper) are scoped to the window's lifetime via OnStartup /
	// OnShutdown so they exit cleanly when the user closes the window.
	var cancelWorkers context.CancelFunc

	onStartup := func(ctx context.Context) {
		workerCtx, cancel := context.WithCancel(ctx)
		cancelWorkers = cancel
		srv.StartBackground(workerCtx)
		logger.Info("apogee desktop started", "version", version.Version, "db", dbPath)
	}

	onShutdown := func(_ context.Context) {
		logger.Info("apogee desktop shutting down")
		if cancelWorkers != nil {
			cancelWorkers()
		}
		// Bound the shutdown to 5 s so a stuck telemetry exporter or
		// summarizer worker can never hang app exit. Matches the
		// deadline `apogee serve` uses for graceful shutdown.
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.StopBackground(stopCtx)
		if err := store.Close(); err != nil {
			logger.Warn("close duckdb store", "err", err)
		}
	}

	return wails.Run(&options.App{
		Title:             "Apogee",
		Width:             1440,
		Height:            900,
		MinWidth:          1024,
		MinHeight:         640,
		BackgroundColour:  options.NewRGB(10, 12, 20),
		HideWindowOnClose: false,
		AssetServer: &assetserver.Options{
			Handler: srv.Router(),
		},
		OnStartup:  onStartup,
		OnShutdown: onShutdown,
		Mac: &mac.Options{
			TitleBar: &mac.TitleBar{
				TitlebarAppearsTransparent: true,
				HideTitle:                  false,
				HideTitleBar:               false,
				FullSizeContent:            true,
				UseToolbar:                 false,
				HideToolbarSeparator:       true,
			},
			Appearance:           mac.NSAppearanceNameDarkAqua,
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			About: &mac.AboutInfo{
				Title:   "Apogee",
				Message: "Single-binary observability for multi-agent Claude Code sessions.\nVersion " + version.Version,
			},
		},
	})
}

func resolveDBPath() (string, error) {
	if v := strings.TrimSpace(os.Getenv("APOGEE_DB")); v != "" {
		return expandHome(v)
	}
	return expandHome("~/.apogee/apogee.duckdb")
}

func expandHome(p string) (string, error) {
	if p == "" || p == ":memory:" {
		return p, nil
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, strings.TrimPrefix(p, "~")), nil
	}
	return p, nil
}
