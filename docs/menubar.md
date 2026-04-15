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

## How to keep it always running

PR #22's onboarding wizard will offer to register the menu bar as a second
launchd unit (`dev.biwashi.apogee.menubar`). Until then, add it to your
shell profile as a background process or to your login items manually:

    System Settings → General → Login Items → Open at Login → +
    Add the apogee binary with arguments `menubar`.

Either way, the menu bar app still needs the **daemon** (or a foreground
`apogee serve`) to be running — there is no point in the menu bar existing
without a collector to poll.
