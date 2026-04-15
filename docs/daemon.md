# Daemon core

apogee can install itself as a background service that starts on
every login. The same subcommand tree works on macOS (launchd) and
Linux (systemd `--user`), so operator muscle memory is the same on
both platforms.

```sh
apogee daemon install     # register the unit
apogee daemon start       # launch it now
apogee status             # high-level daemon + collector probe
apogee daemon status      # detailed daemon report
apogee logs -f            # tail the daemon logs
apogee daemon stop        # bring it down
apogee daemon uninstall   # remove the unit file
```

## Unit file locations

| Platform | Path                                                             |
|----------|------------------------------------------------------------------|
| macOS    | `~/Library/LaunchAgents/dev.biwashi.apogee.plist`                |
| Linux    | `$XDG_CONFIG_HOME/systemd/user/apogee.service` (default `~/.config/systemd/user/apogee.service`) |

Both unit files call into the currently-running `apogee` binary
with `serve --addr 127.0.0.1:4100 --db ~/.apogee/apogee.duckdb` by
default. Override the addr and db path at install time:

```sh
apogee daemon install --addr 127.0.0.1:9100 --db ~/.apogee/local.duckdb
```

## Supervisor primitives

### macOS / launchd

apogee shells out to `launchctl` with the `gui/$(id -u)/` domain, so
the daemon runs as your user and has access to your login
environment.

| Operation  | Invocation |
|------------|------------|
| Install    | `launchctl bootstrap gui/<uid> <plist>` |
| Start      | `launchctl kickstart gui/<uid>/dev.biwashi.apogee` |
| Restart    | `launchctl kickstart -k gui/<uid>/dev.biwashi.apogee` |
| Stop       | `launchctl bootout gui/<uid>/dev.biwashi.apogee` |
| Uninstall  | `launchctl bootout …` + `rm <plist>` |
| Status     | `launchctl print gui/<uid>/dev.biwashi.apogee` |

The plist sets `KeepAlive=true` and `RunAtLoad=true` by default, so
an accidental crash is restarted by launchd and a fresh login
brings the collector up automatically.

### Linux / systemd --user

apogee shells out to `systemctl --user`, so the daemon runs as your
user and does not require root.

| Operation  | Invocation |
|------------|------------|
| Install    | write unit, `systemctl --user daemon-reload`, `systemctl --user enable apogee.service` |
| Start      | `systemctl --user start apogee.service` |
| Restart    | `systemctl --user restart apogee.service` |
| Stop       | `systemctl --user stop apogee.service` |
| Uninstall  | `systemctl --user disable --now apogee.service` + `rm <unit>` |
| Status     | `systemctl --user show apogee.service --property=…` |

The unit sets `Restart=on-failure` with a 3s backoff by default.

Note: `systemctl --user` needs a user session bus. On a headless
Linux box you may need to enable it with `loginctl enable-linger $USER`
so the user instance stays alive across logouts.

## Debugging a stuck daemon

```sh
# Is the process actually up?
apogee daemon status

# Tail the live logs (Ctrl-C to exit).
apogee logs -f

# Probe the HTTP surface directly.
curl http://127.0.0.1:4100/v1/healthz

# Full reload.
apogee daemon restart
```

If `apogee daemon status` reports `Installed: yes` but `Running: no`
and the health probe fails, the collector is probably crash-looping
on a bad config or a port conflict. Check `apogee.err.log` for the
stack:

```sh
tail -n 200 ~/.apogee/logs/apogee.err.log
```

Common pitfalls:

- **Port 4100 already bound.** Some other `apogee serve` instance
  from a previous dev session is still listening. `pkill -f "apogee
  serve"` clears it.
- **DuckDB lock file.** A hard kill can leave a `.duckdb.wal` lock
  on disk that prevents re-open. Remove the `.wal` file and restart.
- **Stale plist / service file.** A mid-install crash can leave a
  partial unit file. Re-run `apogee daemon install --force`.

## Running without the daemon

For local development you usually don't want the daemon at all:

```sh
apogee serve --addr :4100 --db .local/apogee.duckdb
```

This is the exact command the unit file runs, but in the
foreground. Ctrl-C to stop. `apogee logs` does not apply in this
mode — stdout goes to your terminal directly.

## Config

The `[daemon]` block in `~/.apogee/config.toml` holds the knobs:

```toml
[daemon]
label         = "dev.biwashi.apogee"
addr          = "127.0.0.1:4100"
db_path       = "~/.apogee/apogee.duckdb"
log_dir       = "~/.apogee/logs"
keep_alive    = true
run_at_load   = true
```

Every value has a default, so the block is purely additive.
