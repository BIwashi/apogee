/**
 * Typed design token exports for the apogee UI.
 *
 * Keep this file in sync with `app/globals.css` and `docs/design-tokens.md` —
 * those three are the canonical source of truth for the design system and a
 * change to any one of them must be reflected in the other two.
 *
 * Everything here is a pure data export (no React, no Tailwind). Consumers
 * that need inline styles (charts, SVG, d3) pull from this module; consumers
 * that render HTML prefer the corresponding CSS variables.
 */

// ── Artemis core palette ────────────────────────────────────
export const artemis = {
  red: "#FC3D21",
  blue: "#0B3D91",
  earth: "#27AAE1",
  shadow: "#58595B",
  space: "#A7A9AC",
  white: "#FFFFFF",
  black: "#000000",
} as const;

// ── Dark UI surfaces ────────────────────────────────────────
export const surface = {
  deepspace: "#06080f",
  surface: "#0c1018",
  raised: "#141a24",
  overlay: "#1c2333",
  border: "#1e2a3a",
  borderBright: "#2a3a50",
} as const;

// ── Semantic status ─────────────────────────────────────────
export const status = {
  critical: "#FC3D21",
  warning: "#E08B27",
  success: "#27E0A1",
  info: "#27AAE1",
  muted: "#58595B",
} as const;

export type StatusKey = keyof typeof status;

// ── Accent gradient ─────────────────────────────────────────
export const accentGradient =
  "linear-gradient(135deg, #0B3D91 0%, #27AAE1 50%, #FC3D21 100%)";

// ── Typography ──────────────────────────────────────────────
export const typography = {
  display: '"Artemis Inter", ui-sans-serif, system-ui, sans-serif',
  body: '-apple-system, BlinkMacSystemFont, "Segoe UI", "Noto Sans", Helvetica, Arial, sans-serif',
  mono: 'ui-monospace, "SF Mono", Menlo, monospace',
  displayLetterSpacing: "0.12em",
  displayWeight: 700,
} as const;

// ── Icon library ────────────────────────────────────────────
export const icon = {
  library: "lucide-react",
  defaultSize: 16,
  defaultStrokeWidth: 1.5,
} as const;

// ── 10-color session palette ────────────────────────────────
//
// Derived by walking the OKLCH hue wheel at a uniform lightness of L=70% and
// chroma C=0.16, starting near the NASA Blue hue and spacing 36 degrees so
// every session gets a visually distinct but equally-weighted chart color.
// Values were rounded to sRGB hex at generation time and hard-coded here so
// the palette is deterministic across platforms with no runtime OKLCH math.
//
// These colors are intended for series coloring (per-session lines, bars,
// dots). Do NOT use them for semantic status — use `status.*` for that.
export const sessionPalette: readonly string[] = [
  "#5BB8F0", // 1  ~hue 240  — cyan-blue   (echoes artemis-earth)
  "#7FAEF6", // 2  ~hue 264  — periwinkle
  "#A8A2F1", // 3  ~hue 288  — lavender
  "#CE97D9", // 4  ~hue 312  — orchid
  "#E894B4", // 5  ~hue 336  — rose
  "#F29A85", // 6  ~hue   0  — salmon      (warm shift of artemis-red)
  "#E5A962", // 7  ~hue  48  — amber
  "#BDB84D", // 8  ~hue  96  — citron
  "#7FC96E", // 9  ~hue 132  — leaf
  "#4BD2A5", // 10 ~hue 168  — seafoam     (echoes status-success)
] as const;

/**
 * sessionColor — deterministic mapping from a session id (or any string) to
 * one of the 10 palette slots. Uses a small FNV-1a style hash so the same
 * sessionId always resolves to the same color across reloads and servers.
 */
export function sessionColor(sessionId: string): string {
  let h = 0x811c9dc5;
  for (let i = 0; i < sessionId.length; i++) {
    h ^= sessionId.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  const idx = (h >>> 0) % sessionPalette.length;
  return sessionPalette[idx]!;
}

// ── Exported union of everything for convenience ───────────
export const tokens = {
  artemis,
  surface,
  status,
  accentGradient,
  typography,
  icon,
  sessionPalette,
} as const;
