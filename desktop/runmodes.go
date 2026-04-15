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
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"

	"github.com/BIwashi/apogee/internal/version"
)

// runProxy spins up the Wails window backed by a reverse proxy
// pointing at a running apogee daemon. No DuckDB is opened, no
// collector is constructed, no worker goroutines are started — the
// daemon owns all of that state and the desktop process is just a
// WKWebView chrome.
//
// This is the only runtime mode the desktop shell supports. First-run
// users reach it via runBootstrap after `apogee onboard --yes`
// installs a daemon; returning users reach it directly from run().
func runProxy(logger *slog.Logger, daemonAddr string) error {
	target, err := url.Parse("http://" + daemonAddr)
	if err != nil {
		return fmt.Errorf("proxy: parse daemon addr %q: %w", daemonAddr, err)
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	// SSE streams (/v1/events/stream, /v1/interventions/stream) must
	// not be buffered — the live dashboard and operator intervention
	// timeline rely on server push. -1 forces an immediate flush
	// after every write.
	proxy.FlushInterval = -1
	// Log proxy errors rather than letting httputil print them to
	// stderr with no context. The WebView already shows a native
	// "network error" page on 5xx, so we just need breadcrumbs for
	// debugging after the fact.
	proxy.ErrorLog = slogErrorLog(logger, "proxy")
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		logger.Warn("proxy forward error", "path", r.URL.Path, "err", err)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("apogee-desktop: upstream daemon at " + daemonAddr + " is unreachable.\n"))
	}

	logger.Info("apogee desktop proxy mode ready", "version", version.Version, "daemon", target.String())

	return wails.Run(buildWailsOptions(proxy, func(_ context.Context) {
		logger.Info("apogee desktop window ready", "daemon", target.String())
	}, func(_ context.Context) {
		logger.Info("apogee desktop window closing")
	}))
}

// buildWailsOptions returns the Wails App options used by the
// desktop shell. Centralised so the proxy mode and the first-run
// bootstrap (which also ends in proxy mode) share identical window
// chrome — titlebar, minimum size, appearance, About panel.
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

// slogErrorLog returns a *log.Logger that routes Write calls into
// the given slog.Logger at Warn level. Used to adapt
// net/http/httputil's ErrorLog field (which wants a stdlib
// *log.Logger) into our structured logging stack so proxy errors
// carry the "proxy" tag and land alongside the rest of the desktop
// shell's output.
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
