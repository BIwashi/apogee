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

## Pull request workflow

- One feature branch per PR, named `feat/<slug>` or `fix/<slug>`.
- Commit messages and PR titles are written in English.
- PR descriptions are written in Japanese (author preference).
- Squash-merge into `main`. CI on the `go` and `web` jobs must be green.
- Never commit `.duckdb` files, `.env*`, or anything under `/data/`.

## What is in scope for each PR

The scaffold (this PR) adds no business logic. Collector ingest, SSE fan-out,
session detail, HITL, OpenTelemetry wiring, frontend embedding, and the CLI
land in subsequent PRs in that order. Keep additions small and reviewable.
