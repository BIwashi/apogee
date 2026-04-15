# apogee desktop (macOS)

A native macOS window that hosts the same collector + embedded Next.js
dashboard as `apogee serve`, built with [Wails v2](https://wails.io). The
real entry point lives in [`desktop/main.go`](../desktop/main.go) under
`//go:build darwin`, with a small `//go:build !darwin` stub in
[`desktop/main_other.go`](../desktop/main_other.go) so `go build ./...` on
Linux/Windows CI runners stays green and `./apogee-desktop` prints a clear
"macOS only" error if it ever gets invoked on those platforms. Wails
itself supports Linux and Windows, but apogee currently targets and
validates the desktop workflow on Darwin only.

Unlike `apogee menubar`, the desktop shell **owns** the collector: it opens
the DuckDB store directly, wires up the ingest/reconstruct pipeline, and
hands the resulting `chi.Router` to the Wails `AssetServer.Handler`. No extra
TCP listener is opened in the desktop process — WebView requests are
dispatched straight into the router, so `/v1/*` and the embedded SPA are
served from a single in-process handler.

```
DuckDB store ──▶ collector.New ──▶ Server.Router (chi.Router, http.Handler)
                                      │
                                      ▼
                        Wails AssetServer.Handler
                                      │
                                      ▼
                              WKWebView (native)
```

## Prerequisites

- macOS (Darwin) — Wails renders via WKWebView.
- Xcode Command Line Tools (`xcode-select --install`) — cgo + DuckDB + Wails.
- Go toolchain (matching `go.mod`).
- Node.js for the Next.js frontend build (only required when rebuilding
  `web/out`).
- [`wails` CLI](https://wails.io/docs/gettingstarted/installation) for
  `make desktop-app` / `make desktop-dev`:

  ```sh
  go install github.com/wailsapp/wails/v2/cmd/wails@latest
  ```

  Plain `make desktop-build` and `make desktop-run` do **not** need the Wails
  CLI — a stock `go build` is enough. The CLI is only required for `.app`
  bundling and hot-reload dev mode.

## Running

```sh
# Build the embedded web bundle and the desktop binary in one shot.
make desktop-build

# Build then launch the Wails window. The window opens against the same
# DuckDB store the rest of apogee uses (~/.apogee/apogee.duckdb by default);
# override with APOGEE_DB=/path/to/db.
make desktop-run
```

The window uses the same dark theme tokens as the browser UI. There is no
dock or menu-bar UI on top of the default Wails chrome yet; the traffic-light
buttons and the OS-level window menu are all standard Cocoa.

## Building an `.app` bundle

```sh
make desktop-app
# produces desktop/build/bin/Apogee.app
```

This target shells out to `wails build -platform darwin/universal`. Wails
handles `Info.plist`, icon, and universal (arm64 + amd64) compilation. Code
signing and notarization are **not yet wired up** — the resulting bundle
runs locally but will be flagged by Gatekeeper if distributed. Adding the
`--apple-id` / `AC_PASSWORD` flow to `make desktop-app` is a follow-up.

## Dev mode (hot reload)

Wails' dev mode renders inside the WebView but loads the frontend from an
externally managed Next.js dev server, so you need **three** processes
cooperating:

```sh
# Terminal 1: collector on :4100. This is where the Next.js dev server
# forwards /v1/* requests (see web/next.config.ts rewrite rule).
make run-collector

# Terminal 2: Next.js dev server on :3000.
make web-dev

# Terminal 3: Wails dev window. Proxies to http://localhost:3000 via the
# frontend:dev:serverUrl entry in desktop/wails.json — Wails itself does
# not spawn the Next.js server, so the dev mode is a pure attach.
make desktop-dev
```

Saving a file under `web/app/` triggers the Next.js HMR layer and the
WKWebView picks up the update automatically. The desktop binary itself
is not rebuilt by this flow — to pick up Go changes under `desktop/` or
`internal/collector/`, stop `make desktop-dev` and restart it.

If you only want to iterate on the UI, the simplest flow is actually the
original browser one: `make run-collector` + `make web-dev` + browse to
`http://localhost:3000`. The desktop dev mode only buys you the WKWebView
chrome on top.

## Architecture notes

- `internal/collector/server.go` exposes `StartBackground(ctx)` /
  `StopBackground(ctx)`. These start the metrics sampler, the summarizer,
  the HITL expiration ticker, and the intervention sweeper — i.e. everything
  `Run()` does except `ListenAndServe`. The desktop shell uses them to scope
  worker goroutines to the window lifetime via Wails' `OnStartup` /
  `OnShutdown` hooks.
- The Wails `AssetServer.Handler` is set to `srv.Router()`. The router already
  serves the embedded SPA (via `internal/webassets`) at `/` and the typed API
  at `/v1/*`, so there is no separation between "API" and "static assets" at
  the desktop layer.
- DuckDB is **exclusive** — you cannot run `apogee serve` and `apogee
  desktop` against the same `~/.apogee/apogee.duckdb` at the same time. Use
  `APOGEE_DB=:memory:` or a separate file for parallel experimentation.

## Known limitations

- macOS only. The code is portable but unverified on Linux/Windows.
- No code signing / notarization. Distributed `.app` will trigger Gatekeeper.
- No menu bar entries beyond the default Wails-provided ones (EditMenu +
  WindowMenu). Custom menus, tray icons, and the `apogee menubar` feature
  are independent and do not share code with the desktop shell yet.
- No single-instance lock. Running two desktop processes simultaneously will
  race on the DuckDB file (and the second will error out on open).

## Why Wails and not Electron/Tauri?

- **Go-native.** The apogee module is Go; Wails lets us keep the entire
  collector in-process without IPC or a subprocess shim.
- **WKWebView, not Chromium.** The `.app` bundle stays under ~20 MB and
  reuses the system WebView.
- **Zero new build languages.** Tauri would pull in a Rust toolchain; Electron
  would pull in Node for the shell. Wails is pure Go + the existing Next.js
  bundle.
