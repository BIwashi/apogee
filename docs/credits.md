# Credits

apogee is built on top of the work of many others. This file lists third-party
assets, fonts, libraries, and inspirations that ship with the binary or are
required at runtime.

## Display fonts

apogee ships two display faces and pairs them by role.

**Space Grotesk** by Florian Karsten handles the everyday display workload —
every small ALL-CAPS label, section header, caption, and button — because it
stays legible at 10–14 px where the other display face does not. It is
licensed under the SIL Open Font License 1.1. A copy of the license ships
with the font file under `web/public/fonts/SpaceGrotesk-OFL.txt`.

- Upstream: https://github.com/floriankarsten/space-grotesk
- License: SIL Open Font License 1.1

**Artemis Inter** is reserved as a brand accent for the APOGEE wordmark
(sidebar + top ribbon) and a few hero-sized page h1s (Live, Events,
Styleguide). The heavier face reads as brand impact at large sizes and is
never used below ~16 px.

- File: `web/public/fonts/Artemis_Inter.otf`

## Body font

apogee uses the operating system's native UI font for body text (San Francisco
on macOS, Segoe UI on Windows, Helvetica Neue as the default fallback). Nothing
is bundled.

## Icons

**lucide** (ISC License, MIT-compatible) — https://lucide.dev/

## Go libraries

| Package | License |
|---|---|
| `github.com/marcboeker/go-duckdb/v2` | MIT |
| `github.com/go-chi/chi/v5` | MIT |
| `github.com/spf13/cobra` | Apache-2.0 |
| `github.com/charmbracelet/fang` | MIT |
| `github.com/charmbracelet/lipgloss/v2` | MIT |
| `github.com/caseymrm/menuet` | MIT |
| `go.opentelemetry.io/otel` | Apache-2.0 |
| `github.com/BurntSushi/toml` | MIT |

## Web libraries

| Package | License |
|---|---|
| `next` (16.x) | MIT |
| `react`, `react-dom` (19.x) | MIT |
| `tailwindcss` (4.x) | MIT |
| `lucide-react` | ISC |
| `recharts` | MIT |
| `swr` | MIT |
| `d3` | ISC |

## Inspirations

- The attention-state model and the phase / episode / intervention language
  are inspired by mitou-adv (https://github.com/MichinokuAI/mitou-adv) and by
  aperion, both by BIwashi.
- The Datadog APM control plane informed the top ribbon, command palette, and
  session-scoped page layout.
- disler's
  [claude-code-hooks-multi-agent-observability](https://github.com/disler/claude-code-hooks-multi-agent-observability)
  was the original prototype that started this project.

## NASA brand marks

apogee does not bundle any NASA trademark or logo. The Artemis-program-inspired
color palette (deep space blue, Artemis red, Earth blue) consists of generic
hex values used under their ordinary design-system meaning and does not claim
affiliation with NASA.
