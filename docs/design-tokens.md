# apogee design tokens

apogee inherits its visual identity from the NASA Artemis Graphic Standards
Guide (September 2021), the same palette shared with the sibling project
[aperion](https://github.com/BIwashi/aperion). This document is the canonical
specification for the design system. The three sources that must stay aligned
are:

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

## Typography

### Display — Artemis Inter

- File: `web/public/fonts/Artemis_Inter.otf` (copied verbatim from aperion)
- Weight: `700`
- `text-transform: uppercase`
- `letter-spacing: 0.12em` (bump to `0.16em`–`0.20em` for hero treatments)
- CSS class: `.font-display`
- Rule: keep display runs short — three words maximum per block, per NASA
  guideline.

### Body — system stack

```css
font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Noto Sans",
  Helvetica, Arial, sans-serif;
```

### Monospace

```css
font-family: ui-monospace, "SF Mono", Menlo, monospace;
```

Use for event ids, session ids, timestamps, and any code-shaped value.

### Scale

| Level   | Size     | Weight        | Role               |
| ------- | -------- | ------------- | ------------------ |
| Display | 20–60 px | 700 (Artemis) | Hero, page title   |
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

1. **Dark-first.** Designed for multi-hour monitoring sessions.
2. **Information density.** Operators want data, not whitespace.
3. **Status at a glance.** Color coding follows the Artemis palette
   consistently — there is one `critical`, one `warning`, one `success`, one
   `info`, one `muted`.
4. **No emoji.** Ever. lucide-react is the only icon library.
5. **Artemis for authority.** Display font is reserved for titles, brand
   marks, and short section headers.
