# apogee desktop (macOS)

A native macOS window that hosts the same dashboard `apogee serve`
renders, wrapped in Wails v2's WKWebView instead of a browser tab. The
implementation lives in [`desktop/`](../desktop) (`//go:build darwin`),
with a `//go:build !darwin` stub in
[`desktop/main_other.go`](../desktop/main_other.go) so `go build ./...`
on Linux/Windows CI runners stays green and any stray invocation on
those platforms prints a clean "macOS only" message. Wails itself
supports Linux and Windows, but apogee only validates the desktop
workflow on Darwin.

## Runtime modes

The desktop shell is **proxy-first**. On launch it probes the local
`apogee daemon` and, if the daemon answers, becomes a thin
WKWebView wrapper around `http://127.0.0.1:4100` via an in-process
`net/http/httputil.ReverseProxy`. Nothing is opened in the desktop
process — no DuckDB, no collector, no background workers — the daemon
owns all of that state.

```
                          ┌──────────────────────────────┐
                          │  Claude Code hook processes  │
                          └──────────────┬───────────────┘
                                         │  POST /v1/events
                                         ▼
┌──────────────────┐   GET /v1/*   ┌──────────────────────┐
│   Apogee.app     │ ────────────▶ │  apogee daemon       │
│   (WKWebView)    │ ◀──────────── │  127.0.0.1:4100      │
│   ReverseProxy   │   SSE stream  │  DuckDB + collector  │
└──────────────────┘               └──────────────────────┘
```

Why proxy-first: Claude Code hooks are configured (by `apogee onboard`)
to send events to the daemon on `127.0.0.1:4100`. If the desktop shell
owned its own collector with its own DuckDB, it would see an empty
database while the real events landed in the daemon's file — and any
operator intervention submitted from the desktop UI would never reach
the running Claude Code session because the hooks post to the daemon,
not the desktop. Proxy mode makes the desktop shell a *view of the
daemon's state*, which is the only mental model that stays consistent
with the hook topology.

### First-run bootstrap

When the daemon is unreachable, the desktop shell runs a first-run
setup flow so users who installed the cask fresh (no previous apogee
setup) still reach a working state in one click:

1. Native Cocoa confirmation dialog (via `osascript`): "apogee is not
   set up on this machine. Set it up now?"
2. On **Set up**: the shell runs `apogee onboard --yes` as a
   subprocess. Since the cask declares a formula dependency on
   `BIwashi/tap/apogee`, the CLI is guaranteed to be on `PATH`.
3. `apogee onboard --yes` installs the Claude Code hooks at user
   scope, installs + starts the daemon as a launchd user service,
   configures the summarizer against the local `claude` CLI, and
   returns.
4. The shell polls `http://127.0.0.1:4100/v1/healthz` with a 30 s
   budget until the daemon responds.
5. `~/.apogee/installed-by-desktop` is written as a marker so the
   cask's uninstall path knows it is responsible for tearing the
   daemon down.
6. The shell transitions to proxy mode — identical to the already-set-up
   path from this point on.

On **Cancel**, the shell exits cleanly without touching anything.

### Configuration

One environment variable, for the rare case where the daemon does not
live on the default port:

| Env var | Default | Behaviour |
|---|---|---|
| `APOGEE_DAEMON_ADDR` | `127.0.0.1:4100` | `host:port` the reverse proxy forwards to and the reachability probe hits. Must not include a scheme. |

The desktop shell intentionally has **no mode flag and no DuckDB
path**. It never opens a database, never constructs a collector, and
never starts a worker goroutine — everything the UI shows comes from
the running daemon at `APOGEE_DAEMON_ADDR`. That singular runtime
model is what lets the .app coexist cleanly with `apogee serve` /
`apogee daemon` and with running Claude Code hooks, and it is also
what keeps the shipped binary around 10 MB instead of 60 MB (no
DuckDB static lib linked in).

## Installing from Homebrew

Each tagged release publishes an `apogee-desktop` Cask alongside the
`apogee` CLI Formula in
[`BIwashi/homebrew-tap`](https://github.com/BIwashi/homebrew-tap):

```sh
brew tap BIwashi/tap
brew install --cask apogee-desktop
open -a Apogee
```

The cask declares a `depends_on formula: BIwashi/tap/apogee`
dependency, so the CLI is auto-installed even if you only asked for
the cask. The cask's `postflight` hook runs
`xattr -dr com.apple.quarantine` on the staged bundle so macOS 15
Gatekeeper does not block first launch on the unsigned `.app`.

If you download the release zip directly from GitHub you have to
strip the quarantine flag yourself
(`xattr -dr com.apple.quarantine /Applications/Apogee.app`) or
right-click → Open on older macOS.

### Upgrading

```sh
brew upgrade --cask apogee-desktop
```

This does an uninstall+install pair under the hood. The cask's
`uninstall_preflight` intentionally leaves
`~/Library/LaunchAgents/dev.biwashi.apogee.plist` in place during
`bootout`, so the next launch of `Apogee.app` picks the existing
daemon setup back up without re-running `apogee onboard`.

### Uninstalling

```sh
# Plain: stop the daemon (if the desktop shell installed it), remove
# the .app, remove the /opt/homebrew/bin/apogee-desktop launcher.
# Leaves ~/.apogee intact so history survives.
brew uninstall --cask apogee-desktop

# Full nuke: the above plus ~/.apogee (DuckDB store + logs + config)
# and the LaunchAgents plist. Loses all observability history.
brew uninstall --zap --cask apogee-desktop
```

The plain uninstall only tears down the daemon when the
`~/.apogee/installed-by-desktop` marker file is present, which means
**desktop-first users** (people who started from `brew install
--cask apogee-desktop`) get a clean teardown, while **CLI-first
users** (people whose daemon was installed by `apogee daemon install`
or `apogee onboard` from a terminal) keep their daemon running after
they remove the desktop cask. The CLI formula can always be removed
separately with `brew uninstall BIwashi/tap/apogee`.

## Running from source

```sh
# Build the desktop binary with the Wails production build tag
# wired in (-tags production is mandatory for Wails v2 and is baked
# into the Makefile target already). The target does NOT depend on
# `make build-web` because the desktop shell never imports
# internal/webassets — it only proxies to the daemon.
make desktop-build

# Build and launch. With a running daemon this immediately enters
# proxy mode against 127.0.0.1:4100; without, it runs the first-run
# bootstrap flow. APOGEE_DAEMON_ADDR overrides the default port.
make desktop-run
```

The window uses the same dark theme tokens as the browser UI. There
is no dock or custom menu bar on top of the default Wails chrome
yet — the traffic-light buttons and the OS-level window menu are
standard Cocoa.

## Building an `.app` bundle locally

Two options, depending on whether you have the Wails CLI:

```sh
# Option A — Wails CLI. Produces desktop/build/bin/Apogee.app.
# Convenient for iterating inside the wails dev toolchain.
make desktop-app

# Option B — goreleaser snapshot. Mirrors the CI release path
# exactly: builds the universal binary, runs
# scripts/bundle-desktop-app.sh to wrap it in Apogee.app + the
# launcher shim, zips everything into
# dist/apogee-desktop_<version>_darwin_universal.zip, and
# regenerates dist/homebrew/Casks/apogee-desktop.rb.
goreleaser release --snapshot --clean --skip=before,publish,validate
```

Option B is the authoritative path for release builds. It never
invokes the Wails CLI — `scripts/bundle-desktop-app.sh` assembles the
`.app` itself from `sips` + `iconutil` + an inline `Info.plist`
heredoc. Code signing and notarization are not yet wired up; the
unsigned bundle ships through Homebrew because the cask's
`postflight` strips the quarantine xattr on install.

## Dev mode (hot reload)

Wails' dev mode renders inside the WebView but loads the frontend
from an externally managed Next.js dev server, so you need **three**
processes cooperating:

```sh
# Terminal 1: collector on :4100. This is the reverse proxy target
# AND the rewrite target for the Next.js dev server's /v1/* rule.
make run-collector

# Terminal 2: Next.js dev server on :3000.
make web-dev

# Terminal 3: Wails dev window. Proxies its WebView asset requests
# to http://localhost:3000 via frontend:dev:serverUrl in
# desktop/wails.json — Wails itself does not spawn Next.js.
make desktop-dev
```

Saving a file under `web/app/` triggers the Next.js HMR layer and
the WKWebView picks up the update automatically. The desktop binary
itself is not rebuilt by this flow — to pick up Go changes under
`desktop/` or `internal/collector/`, stop `make desktop-dev` and
restart it.

If you only want to iterate on the UI, the simplest flow is still
the original browser one: `make run-collector` + `make web-dev` +
browse to `http://localhost:3000`. The desktop dev mode only buys
you the WKWebView chrome on top.

## Architecture notes

- **Proxy handler**: `runProxy()` in `desktop/runmodes.go` wraps
  `httputil.NewSingleHostReverseProxy(target)` with
  `FlushInterval: -1` so SSE streams (`/v1/events/stream`,
  `/v1/interventions/stream`) are not buffered.
- **Bootstrap**: `runBootstrap()` in `desktop/bootstrap.go` handles
  the first-run flow. All native dialogs are spawned via `osascript`
  (no cgo / AppKit bindings), so the bootstrap module builds and
  tests without touching the Cocoa runtime. When the subprocess
  finishes the shell calls `runProxy()` — there is no separate
  "post-setup mode", it is the same proxy mode as a warm start.
- **No in-process collector**: the desktop shell does not import
  `internal/collector`, `internal/store/duckdb`, or
  `internal/webassets`. The release binary clocks in at ~10 MB
  instead of the ~60 MB it would be with the DuckDB static lib
  linked in, and there is exactly one place in the process tree
  that owns the DuckDB file (the daemon).
- **UniformTypeIdentifiers framework link**:
  `desktop/cgo_darwin.go` carries a single `#cgo darwin LDFLAGS:
  -framework UniformTypeIdentifiers` directive. Without it, Wails'
  WebKit binding's weak reference to `_OBJC_CLASS_$_UTType` is
  stripped by the release build's `-ldflags "-s -w"` and the link
  step fails with `Undefined symbols for architecture arm64`. This
  is a known Wails v2 / Xcode 15 quirk.

## Prerequisites (source builds)

- macOS (Darwin) — Wails renders via WKWebView.
- Xcode Command Line Tools (`xcode-select --install`) — cgo + DuckDB + Wails.
- Go toolchain (matching `go.mod`).
- Node.js for the Next.js frontend build (only required when
  rebuilding `web/out`).
- [`wails` CLI](https://wails.io/docs/gettingstarted/installation)
  for `make desktop-app` / `make desktop-dev`:

  ```sh
  brew install wails
  ```

  Plain `make desktop-build` / `make desktop-run` do **not** need
  the Wails CLI — a stock `go build -tags production` is enough.

## Known limitations

- **macOS only.** The code is portable but unverified on Linux/Windows.
- **No code signing / notarization.** The cask strips
  `com.apple.quarantine` on install so Gatekeeper lets first launch
  through; manually-downloaded release zips still need the user to
  run `xattr -dr com.apple.quarantine /Applications/Apogee.app`.
- **Custom menus are default-only.** Only Wails' built-in EditMenu +
  WindowMenu are present. The desktop shell does not yet share code
  with `apogee menubar`.
- **Requires the daemon.** The desktop shell cannot show a dashboard
  without a reachable daemon. First-run users get the bootstrap flow
  that installs one for them; users who go out of their way to
  uninstall the daemon while keeping the cask will see the dashboard
  go blank until they reinstall it.

## Why Wails and not Electron/Tauri?

- **Go-native.** The apogee module is Go; Wails lets us ship the
  desktop shell as a ~10 MB reverse-proxy WKWebView wrapper that
  links against nothing heavier than the standard library +
  `wails/v2`.
- **WKWebView, not Chromium.** The `.app` bundle stays under ~8 MB
  compressed and reuses the system WebView, compared to ~150 MB for
  an Electron app with a bundled Chromium.
- **Zero new build languages.** Tauri would pull in a Rust toolchain;
  Electron would pull in a full Node runtime. Wails is pure Go.
