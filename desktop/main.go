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
	"sync"
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

	// The HTTPAddr field is surfaced verbatim by /v1/info → the Settings
	// page. A blank string would render as an empty row in the UI, so
	// label the in-process transport explicitly instead.
	srv := collector.New(
		collector.Config{HTTPAddr: "in-process (wails webview)", DBPath: dbPath},
		store,
		logger,
	)

	// A single teardown function covers every exit path: the normal
	// OnShutdown hook, and the deferred fallback that fires if wails.Run
	// returns an error before OnShutdown is reached. Wrapped in
	// sync.Once so the two paths never race. Order is important —
	// workers must stop before the store closes, otherwise any pending
	// writes from the metrics sampler or summarizer would hit a closed
	// DuckDB handle.
	var (
		teardownOnce  sync.Once
		cancelWorkers context.CancelFunc
	)
	teardown := func(reason string) {
		teardownOnce.Do(func() {
			logger.Info("apogee desktop shutting down", "reason", reason)
			if cancelWorkers != nil {
				cancelWorkers()
			}
			// Bound the ctx-aware parts of StopBackground (currently
			// just the OTel span processor flush) to 5 s so a wedged
			// exporter cannot hold up window exit. summarizer.Stop()
			// and interventions.Stop() do a sync.WaitGroup drain
			// that does not honour this context — they block until
			// their in-flight jobs return. In practice both are
			// backed by subprocess calls that self-timeout, so we
			// accept the worst-case wait rather than force-killing
			// workers mid-write.
			stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			srv.StopBackground(stopCtx)
			if err := store.Close(); err != nil {
				logger.Warn("close duckdb store", "err", err)
			}
		})
	}
	defer teardown("fallback")

	// The workers (metrics sampler, summarizer, HITL ticker, intervention
	// sweeper) are scoped to the window's lifetime via OnStartup /
	// OnShutdown so they exit cleanly when the user closes the window.
	onStartup := func(ctx context.Context) {
		workerCtx, cancel := context.WithCancel(ctx)
		cancelWorkers = cancel
		srv.StartBackground(workerCtx)
		logger.Info("apogee desktop started", "version", version.Version, "db", dbPath)
	}

	onShutdown := func(_ context.Context) {
		teardown("onShutdown")
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
