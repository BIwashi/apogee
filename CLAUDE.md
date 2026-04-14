# apogee — project guide for Claude Code

apogee is a single-binary observability dashboard for multi-agent Claude Code
sessions. It captures every hook event, stores them in DuckDB, and streams them
to a dark, NASA-inspired Next.js dashboard that ships embedded in the Go binary.

## Repository layout

```
cmd/apogee/         Go entry point (CLI / collector / embedded server)
internal/version/   Build-version string
internal/...        Internal Go packages (collector, store, sse, otel)
web/                Next.js 16 dashboard (App Router, Tailwind v4)
  app/              Routes and React components
  app/lib/          Typed API client, SWR helpers, design tokens
  public/fonts/     Artemis Inter display font
semconv/            OpenTelemetry semantic conventions for claude_code.*
hooks/              Python reference hooks that post to the collector
docs/               Architecture and design-token specification
.github/workflows/  CI (go vet/build/test, web typecheck/lint/build)
```

## Common commands

```sh
# Go
go run ./cmd/apogee            # prints "apogee 0.0.0-dev"
go build ./...
go vet ./...
go test ./... -race

# Web (from web/)
npm install
npm run dev                    # Next.js dev server on :3000
npm run typecheck
npm run lint
npm run build

# Orchestrated
make dev                       # collector + web together
make build                     # build Go binary and Next.js bundle
```

## Design system

The visual identity is the product's competitive advantage and is documented in
[`docs/design-tokens.md`](docs/design-tokens.md). Do not introduce alternate
color scales, emoji, or component libraries. lucide-react is the only icon set.
Artemis Inter is the only display font.

## Architecture

See [`docs/architecture.md`](docs/architecture.md) for the end-to-end sketch
(hooks → collector → DuckDB → SSE → web UI, with an OTel side channel).

## Building

The collector links against DuckDB through `github.com/marcboeker/go-duckdb/v2`,
which is a cgo binding. A working C toolchain is required:

- macOS: install Xcode Command Line Tools (`xcode-select --install`).
- Linux: install `build-essential` (or the equivalent `gcc` + `libc` headers).

`CGO_ENABLED=1` must be set when running `go build`, `go test`, or `go run`
(this is the default for native builds).

## Hooks subsystem

The Python hook library lives in [`hooks/`](hooks/) and is embedded into the
Go binary via `//go:embed all:hooks` (declared in `hooksfs.go` at the repo
root). `apogee init` extracts those files into
`~/.apogee/hooks/<version>/` and rewrites the target `.claude/settings.json`
to point all 12 hook events at `send_event.py`. The hook library is stdlib
only: no `uv`, no third-party packages. Network failures must never break
Claude Code — `apogee_hook.send_event` logs to stderr and returns on error.
See [`hooks/README.md`](hooks/README.md) for the wire contract.

Run the Python unit tests with `python3 -m unittest discover hooks/tests` and
the end-to-end shell smoke test with `hooks/smoke_test.sh`.

## Pull request workflow

- One feature branch per PR, named `feat/<slug>` or `fix/<slug>`.
- Commit messages and PR titles are written in English.
- PR descriptions are written in Japanese (author preference).
- Squash-merge into `main`. CI on the `go` and `web` jobs must be green.
- Never commit `.duckdb` files, `.env*`, or anything under `/data/`.

## Data model

apogee treats **one Claude Code user turn as one OpenTelemetry trace**. The
trace starts at `UserPromptSubmit` and ends at `Stop`. Every tool call,
subagent run, and HITL request inside the turn is a child span. Subagent tool
calls are parented to the subagent span, which is parented to the turn root.

Storage is DuckDB, with OTel-shaped tables for `spans`, `logs`, and
`metric_points`, plus denormalized `sessions` and `turns` tables for fast
dashboard rendering. Attention state is derived and written back onto the
`turns` row (populated by PR #4).

## What is in scope for each PR

PRs land in order. Each PR is small and reviewable. Current status:

- PR #1 — scaffold + design system (merged)
- PR #2 — collector core: DuckDB + trace reconstructor + ingest HTTP
- PR #3 — SSE fan-out + live dashboard skeleton
- PR #4 — attention engine + KPI strip
- PR #5 — turn detail + swim lane + filter chips
- PR #6 — LLM summarizer (Haiku via claude CLI subprocess)
- PR #7 — HITL as structured record
- PR #8 — OTel registry + OTLP integration
- PR #9 — Python hook library + install UX
- PR #10 — embed frontend + CLI + distribution
- PR #11 — polish: README, screenshots, session rollup
