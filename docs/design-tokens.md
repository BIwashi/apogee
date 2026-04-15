# apogee design tokens

apogee's visual identity is a dark, space-tech geometric system that shares a
color palette with the sibling project
[aperion](https://github.com/BIwashi/aperion). The palette hues are inspired
by NASA Artemis-program marketing materials but are generic hex values;
apogee does not bundle any NASA trademark, font, or logo. This document is
the canonical specification for the design system. The three sources that
must stay aligned are:

1. `web/app/globals.css` — CSS variables and utility classes
2. `web/app/lib/design-tokens.ts` — typed TypeScript re-exports
3. this file

If you change any one, update the other two in the same commit.

---

## Color palette

### Artemis core

| Token            | Hex       | Role                                             |
| ---------------- | --------- | ------------------------------------------------ |
| `artemis.red`    | `#FC3D21` | Error, critical alerts, intervene-now            |
| `artemis.blue`   | `#0B3D91` | Primary brand, links, active nav                 |
| `artemis.earth`  | `#27AAE1` | Secondary accent, info states, chart highlights  |
| `artemis.shadow` | `#58595B` | Muted text, borders on dark backgrounds          |
| `artemis.space`  | `#A7A9AC` | Secondary text, labels, placeholders             |
| `artemis.white`  | `#FFFFFF` | Primary text on dark backgrounds                 |
| `artemis.black`  | `#000000` | Deepest background reference                     |

### Dark surfaces

| Token                  | Hex       | Usage                                   |
| ---------------------- | --------- | --------------------------------------- |
| `surface.deepspace`    | `#06080f` | Page background                         |
| `surface.surface`      | `#0c1018` | Cards, sidebar                          |
| `surface.raised`       | `#141a24` | Hover states, elevated cards            |
| `surface.overlay`      | `#1c2333` | Modals, dropdowns                       |
| `surface.border`       | `#1e2a3a` | Card borders, dividers                  |
| `surface.borderBright` | `#2a3a50` | Active borders, focus rings             |

### Semantic status

| Token             | Hex       | Source             | Usage                        |
| ----------------- | --------- | ------------------ | ---------------------------- |
| `status.critical` | `#FC3D21` | NASA Red           | Tool failure, intervene now  |
| `status.warning`  | `#E08B27` | Warm Earth shift   | Permission request, watch    |
| `status.success`  | `#27E0A1` | Cool complement    | Session complete, healthy    |
| `status.info`     | `#27AAE1` | Earth Blue         | Running, informational       |
| `status.muted`    | `#58595B` | Shadow Gray        | Offline, inactive, idle      |

### Accent gradient

Used sparingly, reserved for brand moments (hero headings, brand bar next to
section headers, active nav indicators):

```
linear-gradient(135deg, #0B3D91 0%, #27AAE1 50%, #FC3D21 100%)
```

---

## Light theme (PR #33)

apogee was dark-only until PR #33. The second palette is keyed off
`:root[data-theme="light"]` in `web/app/globals.css`, driven by the
`ThemeProvider` in `web/app/lib/theme.tsx`, and surfaced through three
identical controls:

1. A tri-state toggle in the TopRibbon (`Monitor` / `Sun` / `Moon` icons)
2. An **Appearance** segmented control on `/settings`
3. A toggle at the top of the `/styleguide` page so designers can verify
   every token in both palettes without leaving the page

The preference cycles through `system → light → dark`. `system` follows
`prefers-color-scheme` live (via a `matchMedia` listener) and clears the
`apogee:theme` localStorage key. `light` / `dark` are persisted as
explicit overrides. An inline script in `app/layout.tsx` sets
`data-theme` **before** React hydrates so the first paint never flashes
the wrong palette.

### Palette comparison

| Token             | Dark       | Light      | Role                     |
| ----------------- | ---------- | ---------- | ------------------------ |
| `--bg-deepspace`  | `#06080f`  | `#f8fafc`  | Page background          |
| `--bg-surface`    | `#0c1018`  | `#ffffff`  | Card / sidebar bg        |
| `--bg-raised`     | `#141a24`  | `#f1f5f9`  | Hover / elevated         |
| `--bg-overlay`    | `#1c2333`  | `#ffffff`  | Modals / dropdowns       |
| `--border`        | `#1e2a3a`  | `#e2e8f0`  | Default borders          |
| `--border-bright` | `#2a3a50`  | `#cbd5e1`  | Active borders           |
| `--artemis-white` | `#ffffff`  | `#0f172a`  | Primary text (reversed)  |
| `--artemis-space` | `#A7A9AC`  | `#475569`  | Secondary text           |
| `--artemis-shadow`| `#58595B`  | `#64748b`  | Tertiary text            |
| `--artemis-red`   | `#FC3D21`  | `#FC3D21`  | Accent (shared)          |
| `--artemis-blue`  | `#0B3D91`  | `#0B3D91`  | Accent (shared)          |
| `--artemis-earth` | `#27AAE1`  | `#1d91c9`  | Accent (shifted darker)  |
| `--status-critical` | `#FC3D21` | `#dc2626` | Critical status          |
| `--status-warning`  | `#E08B27` | `#d97706` | Warning status           |
| `--status-success`  | `#27E0A1` | `#15803d` | Success status           |
| `--status-info`     | `#27AAE1` | `#0e7fbf` | Info status              |
| `--status-muted`    | `#58595B` | `#64748b` | Muted status             |

### Shadows and overlays

The light palette uses soft slate drop shadows rather than pure black:

| Token              | Dark                            | Light                              |
| ------------------ | ------------------------------- | ---------------------------------- |
| `--shadow-sm`      | `0 1px 2px rgba(0,0,0,0.4)`    | `0 1px 2px rgba(15,23,42,0.08)`   |
| `--shadow-md`      | `0 4px 12px rgba(0,0,0,0.5)`   | `0 4px 12px rgba(15,23,42,0.08)`  |
| `--shadow-lg`      | `0 12px 32px rgba(0,0,0,0.6)`  | `0 12px 32px rgba(15,23,42,0.12)` |
| `--overlay-backdrop` | `rgba(0,0,0,0.60)`            | `rgba(15,23,42,0.35)`             |

### Accent gradient

Both themes share the same three stops, but the light variant pulls the
midpoint slightly inward so the red does not dominate over a white page:

- **Dark:** `linear-gradient(135deg, #0B3D91 0%, #27AAE1 50%, #FC3D21 100%)`
- **Light:** `linear-gradient(135deg, #0B3D91 0%, #1d91c9 55%, #dc2626 100%)`

### Principles

- **Dark is still the default.** `:root` without any attribute resolves to
  the dark palette. Adding `data-theme="light"` opts in.
- **Accents stay branded.** NASA red and blue are identical across
  themes; only earth shifts a few LCH steps darker so chart lines are
  still legible on white.
- **Status tones keep their hue, lose lightness.** The five semantic
  status colors (`critical` / `warning` / `success` / `info` / `muted`)
  remain recognisable but contrast against light surfaces.
- **No structural changes.** Every component already consumed CSS
  variables; the PR is palette derivation + wiring.

---

## Typography

### Display — Space Grotesk (general) + Artemis Inter (brand accent)

apogee pairs two uppercase display faces. Space Grotesk does the
workhorse display load — every small ALL-CAPS label, section header,
caption, and button — because it stays legible at 10–14 px. Artemis
Inter reserves the heavy, tighter face for brand moments where the
extra weight reads as impact rather than noise.

#### Space Grotesk — the everyday display

- Files: `web/public/fonts/SpaceGrotesk-Medium.ttf` (weight 500) and
  `web/public/fonts/SpaceGrotesk-Bold.ttf` (weight 700), both instanced
  from the upstream variable font.
- License: SIL Open Font License 1.1. A verbatim copy of the license
  ships alongside the TTFs at `web/public/fonts/SpaceGrotesk-OFL.txt`.
  Space Grotesk is by Florian Karsten (upstream:
  https://github.com/floriankarsten/space-grotesk).
- CSS class: `.font-display` → resolves via `--font-display`.
- Use for: all section headers, caption labels, button text, facet
  titles, drawer headers, KPI labels. Default choice unless you are
  specifically reaching for the brand accent.

#### Artemis Inter — the brand accent

- File: `web/public/fonts/Artemis_Inter.otf`.
- Weight: `700`.
- CSS class: `.font-display-accent` → resolves via
  `--font-display-accent` (falls back to Space Grotesk if the file
  fails to load).
- Use for: the **APOGEE wordmark** (sidebar brand slot, TopRibbon home
  link) and a small number of hero-sized page h1s (Live, Events,
  Styleguide). At 3xl / 4xl / 5xl the tighter letterforms read as a
  confident brand voice; at caption sizes they get muddy, so do not
  reach for this class on small text.
- Rule: keep display strings short. One to three words per block. If
  the text runs longer, it belongs on `.font-display` (or below).

#### Shared mechanics

- `text-transform: uppercase`
- `letter-spacing: 0.12em` for Space Grotesk, `0.14em` for Artemis
  (bump to `0.16em`–`0.20em` for hero treatments).
- Both classes set `font-weight: 700`.

### Body — system stack

```css
font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Helvetica Neue",
  Helvetica, Arial, sans-serif;
```

### Monospace

```css
font-family: ui-monospace, "SF Mono", Menlo, Monaco, monospace;
```

Use for event ids, session ids, timestamps, and any code-shaped value.

### Scale

| Level   | Size     | Weight        | Role               |
| ------- | -------- | ------------- | ------------------ |
| Display | 20–60 px | 700 (Space Grotesk / Artemis for brand) | Hero, page title |
| Heading | 14–16 px | 600           | Section headers    |
| Body    | 13 px    | 400           | Default text       |
| Caption | 11 px    | 400           | Labels, timestamps |
| Tiny    | 10 px    | 500           | Badges, tags       |

---

## Icons

- Library: `lucide-react`, nothing else. No emoji anywhere in UI chrome.
- Default size: **16 px**
- Default stroke width: **1.5**
- For ultra-dense rows (table cells, inline tags), drop to 13–14 px at the
  same stroke width.

---

## Hook event catalogue

Every Claude Code hook event maps to a semantic tone and a lucide icon. The
canonical source is `web/app/lib/event-types.ts`; this table must mirror it.

| Event                | Tone       | Icon            | Notes                              |
| -------------------- | ---------- | --------------- | ---------------------------------- |
| `PreToolUse`         | `info`     | `Wrench`        | Fired before a tool call           |
| `PostToolUse`        | `info`     | `Wrench`        | Fired after a successful tool call |
| `PostToolUseFailure` | `critical` | `AlertOctagon`  | Tool returned an error             |
| `UserPromptSubmit`   | `info`     | `MessageSquare` | User submitted a new prompt        |
| `Notification`       | `warning`  | `Bell`          | Notification surfaced to user      |
| `PermissionRequest`  | `warning`  | `Shield`        | Awaiting a human decision          |
| `SessionStart`       | `earth`    | `PlayCircle`    | Session initialised                |
| `SessionEnd`         | `muted`    | `StopCircle`    | Session terminated                 |
| `Stop`               | `earth`    | `Octagon`       | Agent reached end of turn          |
| `SubagentStart`      | `accent`   | `Users`         | Subagent spawned                   |
| `SubagentStop`       | `accent`   | `UserCheck`     | Subagent reclaimed                 |
| `PreCompact`         | `muted`    | `Minimize2`     | About to compact history           |

Tones map to the semantic status scale, with two extras:

- `earth` — a lighter informational variant using `artemis.earth`, used for
  lifecycle markers that are neither success nor alarm.
- `accent` — renders as the blue-leaning brand gradient, reserved for subagent
  events so nested work is visually distinct on a long timeline.

---

## Session palette

apogee assigns a deterministic color to every session id so a single session
draws one consistent line across every chart. The palette is a ten-slot walk
of the OKLCH hue wheel at a uniform lightness (L ≈ 70%) and chroma (C ≈ 0.16),
spaced 36 degrees apart. The hex values are hard-coded after rounding so the
palette is deterministic across platforms:

| Slot | Hex       | Hue        | Flavor       |
| ---- | --------- | ---------- | ------------ |
| 1    | `#5BB8F0` | 240        | cyan-blue    |
| 2    | `#7FAEF6` | 264        | periwinkle   |
| 3    | `#A8A2F1` | 288        | lavender     |
| 4    | `#CE97D9` | 312        | orchid       |
| 5    | `#E894B4` | 336        | rose         |
| 6    | `#F29A85` | 0          | salmon       |
| 7    | `#E5A962` | 48         | amber        |
| 8    | `#BDB84D` | 96         | citron       |
| 9    | `#7FC96E` | 132        | leaf         |
| 10   | `#4BD2A5` | 168        | seafoam      |

Use `sessionColor(sessionId)` from `web/app/lib/design-tokens.ts` to map a
session id to a slot. The hash is a tiny FNV-1a walk of the id string, so the
same session always resolves to the same color across reloads, servers, and
client tabs.

These colors are **chart-only**. Do not use them for semantic status — use
`status.*` for that. Do not extend the palette past ten; if you need more
distinct series, group sessions instead.

---

## Usage examples

### Section header with brand bar

```tsx
<SectionHeader
  title="Live pulse"
  subtitle="Event stream from the collector."
/>
```

### Event badge

```tsx
import { getEventType } from "@/app/lib/event-types";
import EventTypeBadge from "@/app/components/EventTypeBadge";

const spec = getEventType("PostToolUseFailure")!;
<EventTypeBadge spec={spec} />;
```

### Status pill

```tsx
<StatusPill tone="warning">permission requested</StatusPill>
```

### Session color

```ts
import { sessionColor } from "@/app/lib/design-tokens";

const color = sessionColor(session.id); // deterministic hex
```

---

## Principles

1. **Dark-first.** Designed for multi-hour monitoring sessions. Light
   theme is opt-in (PR #33) and follows `prefers-color-scheme` when
   the preference is `system`.
2. **Information density.** Operators want data, not whitespace.
3. **Status at a glance.** Color coding follows the Artemis palette
   consistently — there is one `critical`, one `warning`, one `success`, one
   `info`, one `muted`.
4. **No emoji.** Ever. lucide-react is the only icon library.
5. **Space Grotesk for authority.** Display font is reserved for titles,
   brand marks, and short section headers — one to three words, uppercase,
   at display sizes. Everything else is body stack.
