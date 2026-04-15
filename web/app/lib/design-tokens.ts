/**
 * Typed design token exports for the apogee UI.
 *
 * Keep this file in sync with `app/globals.css` and `docs/design-tokens.md` —
 * those three are the canonical source of truth for the design system and a
 * change to any one of them must be reflected in the other two.
 *
 * Everything here is a pure data export (no React, no Tailwind). Consumers
 * that need inline styles (charts, SVG, d3) pull from this module; consumers
 * that render HTML prefer the corresponding CSS variables so the light /
 * dark theme override flows through automatically.
 *
 * PR #33 splits the registry into `dark` / `light` / `shared` buckets. Most
 * callers stay on the legacy top-level exports (`artemis`, `surface`,
 * `status`, `accentGradient`) — those continue to return the **dark** hex
 * values so existing chart code keeps rendering. New code that actually
 * needs a theme-aware hex should read from `DESIGN_TOKENS[theme]`.
 */

// ── Theme type ──────────────────────────────────────────────
export type ThemeName = "dark" | "light";

// ── Shared (theme-independent) ──────────────────────────────
const shared = {
  accent: {
    red: "#FC3D21",
    blue: "#0B3D91",
    // earth is one of the few accents that shifts slightly between themes;
    // it lives on the theme buckets below too. The shared value is the
    // dark-mode source of truth for chart code that wants a stable color.
    earth: "#27AAE0",
  },
  palette: {
    // 10-color session palette — identical in both themes. These are
    // intentionally light-leaning so they read fine on both a dark and
    // a white page; do NOT use them for semantic status.
    sessions: [
      "#5BB8F0",
      "#7FAEF6",
      "#A8A2F1",
      "#CE97D9",
      "#E894B4",
      "#F29A85",
      "#E5A962",
      "#BDB84D",
      "#7FC96E",
      "#4BD2A5",
    ],
  },
  typography: {
    display: `"Space Grotesk", -apple-system, BlinkMacSystemFont, "Segoe UI", "Helvetica Neue", Helvetica, Arial, sans-serif`,
    displayAccent: `"Artemis Inter", "Space Grotesk", ui-sans-serif, system-ui, sans-serif`,
    body: `-apple-system, BlinkMacSystemFont, "Segoe UI", "Helvetica Neue", Helvetica, Arial, sans-serif`,
    mono: `ui-monospace, "SF Mono", Menlo, Monaco, monospace`,
  },
  icon: {
    library: "lucide-react",
    defaultSize: 16,
    defaultStrokeWidth: 1.5,
  },
} as const;

// ── Dark palette ────────────────────────────────────────────
const darkTokens = {
  bg: {
    deepspace: "#06080f",
    surface: "#0c1018",
    raised: "#141a24",
    overlay: "#1c2333",
  },
  border: {
    default: "#1e2a3a",
    bright: "#2a3a50",
  },
  text: {
    primary: "#FFFFFF",
    secondary: "#A7A9AC",
    tertiary: "#58595B",
  },
  accent: {
    red: "#FC3D21",
    blue: "#0B3D91",
    earth: "#27AAE0",
  },
  status: {
    critical: "#FC3D21",
    warning: "#E08B27",
    success: "#27E0A1",
    info: "#27AAE0",
    muted: "#58595B",
  },
  shadow: {
    sm: "0 1px 2px rgba(0, 0, 0, 0.40)",
    md: "0 4px 12px rgba(0, 0, 0, 0.50)",
    lg: "0 12px 32px rgba(0, 0, 0, 0.60)",
  },
  accentGradient:
    "linear-gradient(135deg, #0B3D91 0%, #27AAE0 50%, #FC3D21 100%)",
} as const;

// ── Light palette ───────────────────────────────────────────
const lightTokens = {
  bg: {
    deepspace: "#f8fafc",
    surface: "#ffffff",
    raised: "#f1f5f9",
    overlay: "#ffffff",
  },
  border: {
    default: "#e2e8f0",
    bright: "#cbd5e1",
  },
  text: {
    primary: "#0f172a",
    secondary: "#475569",
    tertiary: "#64748b",
  },
  accent: {
    red: "#FC3D21",
    blue: "#0B3D91",
    earth: "#1d91c9",
  },
  status: {
    critical: "#dc2626",
    warning: "#d97706",
    success: "#15803d",
    info: "#0e7fbf",
    muted: "#64748b",
  },
  shadow: {
    sm: "0 1px 2px rgba(15, 23, 42, 0.08)",
    md: "0 4px 12px rgba(15, 23, 42, 0.08)",
    lg: "0 12px 32px rgba(15, 23, 42, 0.12)",
  },
  accentGradient:
    "linear-gradient(135deg, #0B3D91 0%, #1d91c9 55%, #dc2626 100%)",
} as const;

// ── Canonical registry ──────────────────────────────────────
export const DESIGN_TOKENS = {
  dark: darkTokens,
  light: lightTokens,
  shared,
} as const;

// ── Legacy top-level exports (dark-mode values) ─────────────
// These are preserved verbatim so existing call sites keep compiling.
// New code should prefer `DESIGN_TOKENS[theme]` when it needs a value
// that actually shifts between palettes.

export const artemis = {
  red: darkTokens.accent.red,
  blue: darkTokens.accent.blue,
  earth: darkTokens.accent.earth,
  shadow: darkTokens.text.tertiary,
  space: darkTokens.text.secondary,
  white: darkTokens.text.primary,
  black: "#000000",
} as const;

export const surface = {
  deepspace: darkTokens.bg.deepspace,
  surface: darkTokens.bg.surface,
  raised: darkTokens.bg.raised,
  overlay: darkTokens.bg.overlay,
  border: darkTokens.border.default,
  borderBright: darkTokens.border.bright,
} as const;

export const status = {
  critical: darkTokens.status.critical,
  warning: darkTokens.status.warning,
  success: darkTokens.status.success,
  info: darkTokens.status.info,
  muted: darkTokens.status.muted,
} as const;

export type StatusKey = keyof typeof status;

export const accentGradient = darkTokens.accentGradient;

export const TYPOGRAPHY = {
  display: shared.typography.display,
  body: shared.typography.body,
  mono: shared.typography.mono,
} as const;

export const typography = {
  display: TYPOGRAPHY.display,
  body: TYPOGRAPHY.body,
  mono: TYPOGRAPHY.mono,
  displayLetterSpacing: "0.12em",
  displayWeight: 700,
} as const;

export const icon = shared.icon;

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
export const sessionPalette: readonly string[] = shared.palette.sessions;

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
