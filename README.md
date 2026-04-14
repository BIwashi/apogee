# apogee

> The highest vantage point over your Claude Code agents.

Real-time observability for multi-agent [Claude Code](https://docs.claude.com/en/docs/claude-code) sessions. Captures every hook event, stores them in DuckDB, and streams them to a dark, NASA-inspired dashboard — all shipped as a single Go binary with the web UI embedded.

## Status

Early development. Not yet ready for use.

## Why apogee

Running multi-agent Claude Code workflows means losing sight of what each agent is actually doing — which tools fire, which permissions get asked, which commands get blocked, which subagent is stuck. apogee puts every hook event on one timeline, grouped by session and agent, and keeps it there.

- **One binary.** `go install github.com/BIwashi/apogee/cmd/apogee@latest` and you have a collector, a web UI, and a CLI.
- **DuckDB under the hood.** Columnar storage, time-bucket aggregation, window functions. Query a million events in milliseconds.
- **OpenTelemetry native.** Every hook event becomes an OTLP span with `claude_code.*` semantic conventions. Send it to your own collector or keep it local.
- **Designed, not decorated.** Artemis Inter, NASA color system, lucide icons, zero emoji in the UI chrome.

## License

Apache License 2.0. See [LICENSE](LICENSE).
