//go:build darwin

package main

import (
	"context"
	"fmt"
	stdlog "log"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
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

// slogErrorLog returns a *log.Logger that routes Write calls into the
// given slog.Logger at Warn level. Used to adapt net/http/httputil's
// ErrorLog field (which wants a stdlib *log.Logger) into our
// structured logging stack so proxy errors carry the "proxy" tag and
// land alongside the rest of the desktop shell's output.
func slogErrorLog(logger *slog.Logger, tag string) *stdlog.Logger {
	return stdlog.New(&slogWriter{logger: logger, tag: tag}, "", 0)
}

type slogWriter struct {
	logger *slog.Logger
	tag    string
}

func (w *slogWriter) Write(p []byte) (int, error) {
	msg := strings.TrimRight(string(p), "\n")
	w.logger.Warn(w.tag, "msg", msg)
	return len(p), nil
}

// runProxy spins up a Wails window backed by a reverse proxy pointing
// at a running apogee daemon. No DuckDB is opened, no collector is
// constructed, no worker goroutines are started — the daemon owns all
// of that state and the desktop process is just a WKWebView chrome.
//
// This is the mode every user ends up in unless they explicitly force
// APOGEE_DESKTOP_MODE=embedded. The first-run bootstrap flow also
// lands here after `apogee onboard --yes` installs a daemon for a
// user who had nothing set up.
func runProxy(logger *slog.Logger, daemonAddr string) error {
	target, err := url.Parse("http://" + daemonAddr)
	if err != nil {
		return fmt.Errorf("proxy: parse daemon addr %q: %w", daemonAddr, err)
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	// SSE streams (/v1/events/stream) must not be buffered — the
	// live dashboard relies on server push for session/turn updates.
	// -1 forces an immediate flush after every write.
	proxy.FlushInterval = -1
	// Log proxy errors rather than letting httputil print them to
	// stderr with no context. The WebView already shows a native
	// "network error" page on 5xx, so we just need breadcrumbs for
	// debugging.
	proxy.ErrorLog = slogErrorLog(logger, "proxy")
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		logger.Warn("proxy forward error", "path", r.URL.Path, "err", err)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("apogee-desktop: upstream daemon at " + daemonAddr + " is unreachable.\n"))
	}

	logger.Info("apogee desktop proxy mode ready", "version", version.Version, "daemon", target.String())

	return wails.Run(buildWailsOptions(proxy, func(_ context.Context) {
		logger.Info("apogee desktop window ready (proxy)", "daemon", target.String())
	}, func(_ context.Context) {
		logger.Info("apogee desktop proxy window closing")
	}))
}

// runEmbedded is the legacy in-process path: open DuckDB, construct a
// collector, start background workers, and hand the resulting chi
// router to the Wails AssetServer. Reachable only via
// APOGEE_DESKTOP_MODE=embedded. Known caveats (documented in
// docs/desktop.md):
//
//   - Will fail to open the DuckDB store if an apogee daemon is
//     already running against the same database file.
//   - Operator interventions submitted in this window will NOT be
//     delivered to running Claude Code sessions, because the hooks
//     were configured against the daemon URL at onboard time, not
//     this process.
//
// The mode exists so advanced users can run a completely isolated
// in-memory session (`APOGEE_DESKTOP_MODE=embedded APOGEE_DB=:memory:
// open -a Apogee`) without fighting the daemon.
func runEmbedded(logger *slog.Logger) error {
	dbPath, err := resolveDBPath()
	if err != nil {
		return err
	}
	if dbPath != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
			return fmt.Errorf("ensure db dir: %w", err)
		}
	}

	bootCtx := context.Background()
	store, err := duckdb.Open(bootCtx, dbPath)
	if err != nil {
		// Embedded mode is where DuckDB lock conflicts surface.
		// Pop a native dialog so the user gets something more
		// informative than a stderr message Finder discards.
		showErrorDialog("apogee-desktop could not open the DuckDB store at " + dbPath + ":\n\n" + err.Error() + "\n\nStop the apogee daemon (`apogee daemon stop`) or set APOGEE_DB=:memory: to run with an isolated store.")
		return fmt.Errorf("open duckdb %q: %w", dbPath, err)
	}

	// HTTPAddr is surfaced verbatim on /v1/info → the Settings page.
	// Label the in-process transport so the UI does not render a
	// blank row.
	srv := collector.New(
		collector.Config{HTTPAddr: "in-process (wails webview)", DBPath: dbPath},
		store,
		logger,
	)

	// Start workers synchronously before wails.Run. The hook runs
	// on a Wails-owned goroutine, so starting workers there would
	// race the deferred teardown fallback if wails.Run returned
	// early. Doing it up front removes the race and lets OnStartup
	// be a plain "window ready" log callback.
	workerCtx, cancelWorkers := context.WithCancel(context.Background())
	srv.StartBackground(workerCtx)

	var teardownOnce sync.Once
	teardown := func(reason string) {
		teardownOnce.Do(func() {
			logger.Info("apogee desktop shutting down (embedded)", "reason", reason)
			cancelWorkers()
			// Bound the ctx-aware parts of StopBackground to 5 s so
			// a wedged OTel exporter cannot hang window exit.
			// summarizer.Stop() and interventions.Stop() drain their
			// own wait groups and do NOT honour this context; see
			// internal/collector/server.go StopBackground docs.
			stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			srv.StopBackground(stopCtx)
			if err := store.Close(); err != nil {
				logger.Warn("close duckdb store", "err", err)
			}
		})
	}
	defer teardown("fallback")

	logger.Info("apogee desktop embedded mode ready", "version", version.Version, "db", dbPath)

	return wails.Run(buildWailsOptions(srv.Router(), func(_ context.Context) {
		logger.Info("apogee desktop window ready (embedded)", "db", dbPath)
	}, func(_ context.Context) {
		teardown("onShutdown")
	}))
}

// buildWailsOptions returns the Wails App options common to both
// runmodes. Centralised so proxy and embedded share the exact same
// window chrome — titlebar, minimum size, appearance, About panel.
func buildWailsOptions(handler http.Handler, onStartup, onShutdown func(context.Context)) *options.App {
	return &options.App{
		Title:             "Apogee",
		Width:             1440,
		Height:            900,
		MinWidth:          1024,
		MinHeight:         640,
		BackgroundColour:  options.NewRGB(10, 12, 20),
		HideWindowOnClose: false,
		AssetServer: &assetserver.Options{
			Handler: handler,
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
	}
}

// resolveDBPath is only used by embedded mode. Proxy mode never opens
// a DuckDB store.
func resolveDBPath() (string, error) {
	if v := strings.TrimSpace(os.Getenv("APOGEE_DB")); v != "" {
		return expandHome(v)
	}
	return expandHome("~/.apogee/apogee.duckdb")
}

// expandHome replaces a leading ~ or ~/ with the user's home dir.
// Mirrors internal/cli/fsutil.go:expandHome so the desktop shell does
// not have to import the cli package (which pulls in cobra, huh, and
// everything else).
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
