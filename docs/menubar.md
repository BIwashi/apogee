# apogee menubar (macOS)

A small macOS status item that polls the local apogee collector and shows
live counts in the menu bar. Run it manually with:

    apogee menubar &

The glyph shows:

- `apogee · ●`         — collector is up, no urgent attention
- `apogee · ● 3`       — 3 turns currently running
- `apogee · ▲ 1`       — 1 turn flagged as `intervene_now`
- `apogee · offline`   — collector is unreachable

Click the glyph for the dropdown:

- daemon status
- 12 sessions / 3 active / 1 intervene_now
- Open dashboard (browser)
- Open logs (Finder)
- Restart daemon (shells out to `apogee daemon restart`)
- Quit menubar

## How to keep it always running

PR #22's onboarding wizard will offer to register the menu bar as a second
launchd unit (`dev.biwashi.apogee.menubar`). Until then, add it to your
shell profile as a background process or to your login items manually:

    System Settings → General → Login Items → Open at Login → +
    Add the apogee binary with arguments `menubar`.
