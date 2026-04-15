# apogee menubar (macOS)

A small macOS status item that polls the local apogee collector and shows
live counts in the menu bar. The implementation lives in
[`internal/cli/menubar_darwin.go`](../internal/cli/menubar_darwin.go) and
uses [`caseymrm/menuet`](https://github.com/caseymrm/menuet). On non-darwin
builds the command compiles to a stub that prints a clear "macOS only"
message and exits non-zero.

Run it manually with:

    apogee menubar &

The menu bar app is a **client** of the collector, not a server. It requires
either `apogee daemon start` or a foreground `apogee serve` to be running.
If the collector is unreachable, the glyph switches to `offline` and every
menu entry that would otherwise hit the HTTP surface is hidden.

The glyph shows:

- `apogee · ●`         — collector is up, no urgent attention
- `apogee · ● 3`       — 3 turns currently running
- `apogee · ▲ 1`       — 1 turn flagged as `intervene_now`
- `apogee · offline`   — collector is unreachable

Click the glyph for the dropdown:

- daemon status (running / installed / stopped / missing)
- `N sessions / M active / K intervene_now` snapshot
- Open dashboard (browser, shells out to `open http://127.0.0.1:4100`)
- Open logs (Finder, opens `~/.apogee/logs/`)
- Restart daemon (shells out to `apogee daemon restart`)
- Quit menubar

## Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--addr` | `127.0.0.1:4100` | Collector endpoint to poll |
| `--interval` | `5s` | Poll interval |

## Why is the dot red?

The glyph uses a small state machine driven by `/v1/attention/counts`:

| Visual | Meaning |
| --- | --- |
| solid green dot `●` | at least one running turn, none above `healthy` |
| green dot with number `● 3` | multiple running turns, all healthy |
| yellow caret `▲` | at least one turn in `watch` or `watchlist` |
| red triangle with number `▲ 1` | at least one turn in `intervene_now` |
| grey `offline` | the collector HTTP probe failed |

A red triangle means the attention engine has flagged at least one live
turn as `intervene_now`. Click the glyph and use **Open dashboard** to jump
to the Live page, which will show the offending turn at the top of the
triage rail.

If you expected a healthy dot and you see `offline` instead:

1. `apogee status` — is the daemon running?
2. `apogee logs --err -n 200` — any crash in the error log?
3. `curl http://127.0.0.1:4100/v1/healthz` — is the HTTP surface up?

See [`daemon.md`](daemon.md#debugging-a-stuck-daemon) for the broader
troubleshooting flow.

## Install as a login item

The onboard wizard registers the menu bar as a **second** launchd unit so it
starts at every login without the user having to touch a shell:

    apogee menubar install     # macOS only
    apogee menubar status      # inspect the unit
    apogee menubar uninstall   # remove the login item

The unit is independent from the collector daemon unit — installing or
uninstalling the menubar does not touch `dev.biwashi.apogee`, and vice
versa. The install subcommand writes the plist atomically at
`~/Library/LaunchAgents/dev.biwashi.apogee.menubar.plist`, then asks
launchd to bootstrap it.

| Key | Value | Why |
| --- | --- | --- |
| `Label` | `dev.biwashi.apogee.menubar` | Distinct from the collector daemon label so the two can be installed / uninstalled / inspected independently. |
| `ProgramArguments` | `[ <absolute apogee>, "menubar" ]` | The binary resolves itself via `os.Executable()` at install time, so upgrading apogee just means re-running `menubar install`. |
| `RunAtLoad` | `true` | Start automatically the moment the unit is loaded (every login). |
| `KeepAlive` | `false` | The menubar is interactive — if the user picks **Quit menubar** from the dropdown, launchd does not resurrect it until the next login. |
| `LSUIElement` | `true` | Cocoa menu-bar-only app: no Dock icon, no main window. |
| `LimitLoadToSessionType` | `Aqua` | Only load in a real GUI login session. SSH and headless sessions see no plist, so the menubar never spins up without a user in front of it. |
| `StandardOutPath` / `StandardErrorPath` | `~/.apogee/logs/menubar.{out,err}.log` | Separate log files from the collector daemon so `apogee logs` and diagnostics stay focused. |

Idempotent: a second `menubar install` is a no-op when the plist content
matches. A conflicting plist without `--force` returns `menubar already
installed (pass --force to overwrite)`.

`apogee onboard` offers the same install path as a prompt in the wizard's
menubar group, defaulted to **Install** on a fresh mac and **Re-install**
when the plist already exists. Non-darwin platforms hide the group
entirely. If the collector daemon install succeeds but the menubar install
fails, the wizard logs a warning and continues — the menubar is a
convenience, not a load-bearing surface, so a partial success is better
than rolling back the daemon.

Either way, the menu bar app still needs the **daemon** (or a foreground
`apogee serve`) to be running — there is no point in the menu bar existing
without a collector to poll.

## Manual login-item registration (legacy)

Before `apogee menubar install` existed, the only way to run the menubar at
login was to add it to macOS login items by hand:

    System Settings → General → Login Items → Open at Login → +
    Add the apogee binary with arguments `menubar`.

This path still works and is a reasonable fallback if the launchd unit is
misbehaving. If you take it, remember to **uninstall** the launchd unit via
`apogee menubar uninstall` first, otherwise two menubar processes will
fight over the single menu-bar slot.
