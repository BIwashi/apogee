/**
 * time-range.ts — parser for the global `time` URL param.
 *
 * Two serialisation shapes are supported:
 *   - Shorthand: `15m`, `1h`, `7d` → relative window ending at "now"
 *   - Custom:    `ISO1|ISO2`        → absolute range with both ends pinned
 *
 * The parser returns a normalized `TimeRange` object with `since`/`until`
 * set to concrete Dates or `null` depending on the shape. Callers that hit
 * the API convert the Dates to RFC3339 via `toIsoOrNull`.
 */

export interface TimeRange {
  /** Absolute lower bound, or null when unbounded. */
  since: Date | null;
  /** Absolute upper bound, or null when "now". */
  until: Date | null;
  /** User-facing label, e.g. "Last 15m" or "Custom". */
  label: string;
  /** The shorthand representation when applicable, otherwise null. */
  shorthand: string | null;
}

export interface TimeRangePreset {
  value: string; // shorthand encoding, e.g. "15m"
  label: string; // display label, e.g. "Last 15m"
  seconds: number;
}

/** Canonical preset list shown in the picker. Mirrors DefaultTimeRanges. */
export const TIME_RANGE_PRESETS: TimeRangePreset[] = [
  { value: "5m", label: "Last 5m", seconds: 5 * 60 },
  { value: "15m", label: "Last 15m", seconds: 15 * 60 },
  { value: "1h", label: "Last 1h", seconds: 60 * 60 },
  { value: "4h", label: "Last 4h", seconds: 4 * 60 * 60 },
  { value: "24h", label: "Last 24h", seconds: 24 * 60 * 60 },
  { value: "7d", label: "Last 7d", seconds: 7 * 24 * 60 * 60 },
];

export const DEFAULT_TIME_RANGE_VALUE = "15m";

const SHORTHAND_RE = /^(\d+)([smhd])$/;

function shorthandSeconds(raw: string): number | null {
  const m = SHORTHAND_RE.exec(raw);
  if (!m) return null;
  const n = Number.parseInt(m[1] ?? "", 10);
  if (!Number.isFinite(n) || n <= 0) return null;
  switch (m[2]) {
    case "s":
      return n;
    case "m":
      return n * 60;
    case "h":
      return n * 60 * 60;
    case "d":
      return n * 60 * 60 * 24;
    default:
      return null;
  }
}

function presetLabel(raw: string): string {
  const hit = TIME_RANGE_PRESETS.find((p) => p.value === raw);
  if (hit) return hit.label;
  return `Last ${raw}`;
}

/**
 * parseTimeRange — resolves the `time=` URL param into a TimeRange. Returns
 * the 15-minute default when `raw` is empty or unparseable so callers never
 * have to deal with `null`.
 */
export function parseTimeRange(
  raw: string | null | undefined,
  now: Date = new Date(),
): TimeRange {
  if (!raw) return defaultTimeRange(now);

  // Custom: ISO1|ISO2
  if (raw.includes("|")) {
    const [a, b] = raw.split("|", 2);
    const since = a ? new Date(a) : null;
    const until = b ? new Date(b) : null;
    if (
      since &&
      !Number.isNaN(since.getTime()) &&
      until &&
      !Number.isNaN(until.getTime())
    ) {
      return { since, until, label: "Custom", shorthand: null };
    }
    return defaultTimeRange(now);
  }

  // Shorthand
  const secs = shorthandSeconds(raw);
  if (secs === null) return defaultTimeRange(now);
  return {
    since: new Date(now.getTime() - secs * 1000),
    until: null,
    label: presetLabel(raw),
    shorthand: raw,
  };
}

export function defaultTimeRange(now: Date = new Date()): TimeRange {
  const secs = shorthandSeconds(DEFAULT_TIME_RANGE_VALUE) ?? 15 * 60;
  return {
    since: new Date(now.getTime() - secs * 1000),
    until: null,
    label: presetLabel(DEFAULT_TIME_RANGE_VALUE),
    shorthand: DEFAULT_TIME_RANGE_VALUE,
  };
}

/** Serialise a TimeRange back into a `time=` URL value. */
export function serializeTimeRange(tr: TimeRange): string {
  if (tr.shorthand) return tr.shorthand;
  if (tr.since && tr.until) {
    return `${tr.since.toISOString()}|${tr.until.toISOString()}`;
  }
  return DEFAULT_TIME_RANGE_VALUE;
}

/** Build an absolute custom range from two Dates. */
export function customTimeRange(since: Date, until: Date): TimeRange {
  return { since, until, label: "Custom", shorthand: null };
}

/** Convert a Date to an RFC3339 string, or null for null input. */
export function toIsoOrNull(d: Date | null): string | null {
  if (!d) return null;
  return d.toISOString();
}
