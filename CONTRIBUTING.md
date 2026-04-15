# Contributing to apogee

Thanks for your interest. This guide covers everything you need to land a change.

## Code of conduct

apogee follows the [Contributor Covenant](https://www.contributor-covenant.org/version/2/1/code_of_conduct/). Be kind, assume good intent, and prefer concrete feedback over abstract grievances. Maintainers reserve the right to remove comments, commits, or contributors that violate the covenant.

## Issue triage

- **bug** — something is broken in shipped behaviour
- **enhancement** — additive feature work
- **discussion** — design questions that need consensus before code
- **good first issue** — small, well-scoped, no hidden context required
- **blocker** — must ship before the next tagged release

If you are unsure which label applies, open the issue without one and a maintainer will retag.

## PR workflow

1. Fork or branch from `main`.
2. Name your branch `feat/<slug>` or `fix/<slug>`.
3. Open a PR against `main`. CI must be green before review.
4. Squash-merge into `main`. We avoid merge commits.

A PR is small enough to review in one sitting. If it is not, split it.

## Commit message format

Follow [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/):

```
feat(collector): add session rollup endpoint
fix(web): debounce attention rescore broadcasts
docs(readme): refresh screenshots after rollup landing
```

PR titles are written in **English**. PR descriptions are written in **Japanese** (maintainer preference). Commit messages are written in **English**. There is exactly one exception to the language rule: PR descriptions may be Japanese.

## Testing expectations

Every PR must keep the following commands green:

```sh
# Go
go test ./... -race -count=1
go vet ./...
go build ./...

# Web (from web/)
npm run typecheck
npm run lint
npm run build
```

Non-trivial behavioural changes ship with tests in the same package. The store tests use table-driven cases and the helper `newTestStore`; mirror that style for new tables. The summarizer tests use a `FakeRunner` that returns canned JSON; reuse it instead of shelling out to the real CLI.

## Design system rules

apogee is designed, not decorated. The visual identity is a first-class feature and is non-negotiable inside the dashboard.

- **No emoji** anywhere in the UI chrome, the README, or commit messages.
- **lucide-react only.** No alternate icon sets.
- **CSS variables only.** Tailwind utility classes that resolve to `var(--…)` tokens defined in `web/app/globals.css`. Do not introduce hard-coded hex values.
- **Space Grotesk for display** (SIL OFL 1.1), system stack for body, SF Mono for code.
- See [`docs/design-tokens.md`](docs/design-tokens.md) for the full spec.

## How to add a new hook event type

Apogee's hook pipeline is end-to-end Go-typed. To add a new event:

1. **Wire payload** — add the constant and any new fields in `internal/ingest/payload.go`. Update `validateHookEvent` if the event has required fields beyond the base envelope.
2. **Reconstructor** — add a case in `Reconstructor.Apply` and a handler `handleX` that mutates `sessionState`, writes spans/logs through the store, and broadcasts SSE events. Mirror the handler to OTel via `otelmirror.go` if it has a span lifecycle.
3. **Store schema** — extend `internal/store/duckdb/schema.sql` if the event needs new columns. Add migrations to `internal/store/duckdb/migrate.go` (`applyColumnAdditions`).
4. **Web types** — add the event constant to `web/app/lib/api-types.ts` and surface it in the relevant component.
5. **Tests** — `internal/ingest/reconstructor_test.go` has table-driven coverage for hook lifecycles. Add a row.
6. **Hook subcommand** — if the new event is fired by Claude Code itself, make sure the Go `apogee hook` entry point in `internal/cli/hook.go` knows about it (the `flatHookFields` list) and that `HookEvents` in `internal/cli/init.go` lists it so `apogee init` wires it into settings.json.

## How to add a new subcommand

The CLI is a cobra command tree rooted at
[`internal/cli/root.go`](internal/cli/root.go). To add `apogee foo`:

1. Create `internal/cli/foo.go` with a `NewFooCmd(stdout, stderr io.Writer) *cobra.Command` constructor. Follow the conventions of the existing commands: inject stdout / stderr for tests, prefer returning errors from `RunE`, set `SilenceUsage` + `SilenceErrors` so errors render through `fang` only once.
2. Add a matching `internal/cli/foo_test.go` that exercises the command with a `bytes.Buffer` for stdout / stderr.
3. Wire the constructor into `NewRootCmd` in [`internal/cli/root.go`](internal/cli/root.go) via `root.AddCommand(NewFooCmd(stdout, stderr))`.
4. Document the new command in [`docs/cli.md`](docs/cli.md) **and** its Japanese counterpart [`docs/cli_ja.md`](docs/cli_ja.md). Both files have a stable section-per-command layout.
5. If the command has persistent config, add a matching block to `~/.apogee/config.toml` and document it in the relevant doc (e.g. [`docs/daemon.md`](docs/daemon.md) for the `[daemon]` block).

## How to update docs

Every English doc under `docs/` has a Japanese sibling named
`docs/<name>_ja.md` (the two files live in the same directory,
distinguished by the `_ja` suffix). Whenever you add or change an
English doc:

1. Update the English source file.
2. Update the `_ja.md` sibling in the same commit. Keep the same
   heading structure so deep links survive.
3. At the top of the Japanese file, keep the `[English version / 英語版](./<name>.md)` link that points back to the English source.
4. Keep code blocks, commands, identifiers, JSON examples, and API paths
   in English in both versions. Only the prose is translated.
5. `README.md` and `README.ja.md` are mirrored the same way. Update both
   when you ship a user-visible change to the CLI or the dashboard.

If you land a change that is hard to translate in the same sitting, it is
better to stub the Japanese file with "TODO: translate" than to skip it
entirely — `ls docs/*_ja.md` against `ls docs/*.md` will catch the gap
in the next doc refresh PR.

## How to add a new semconv attribute

The OTel attribute registry is the source of truth for `claude_code.*` semantics.

1. Add the attribute to `semconv/registry/claude_code.yaml` with type, description, and stability marker.
2. Run `go generate ./semconv/...` to refresh the typed Go constants under `semconv/`.
3. Use the new constant in the reconstructor where you emit it (`internal/ingest/reconstructor.go` or one of the helpers).
4. Update `docs/otel-semconv.md` if the attribute is meant to be public-facing.

## Architecture pointers

- [`docs/architecture.md`](docs/architecture.md) — end-to-end sketch
- [`docs/design-tokens.md`](docs/design-tokens.md) — visual system spec
- [`docs/otel-semconv.md`](docs/otel-semconv.md) — claude_code.* attribute registry
- [`internal/store/duckdb/schema.sql`](internal/store/duckdb/schema.sql) — every persistent table apogee writes
- [`internal/sse/event.go`](internal/sse/event.go) — every SSE event type the hub emits
- [`web/app/lib/api-types.ts`](web/app/lib/api-types.ts) — TypeScript mirror of the Go API surface

## Getting help

Open a discussion issue, ping the maintainers, or @-mention an existing reviewer in your PR. We do not have an official chat channel yet.
