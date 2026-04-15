/**
 * Tiny time-ago helper. Returns a compact relative label like "2s", "5m",
 * "3h", or "2d". Accepts a Date, an ISO string, or an epoch milliseconds
 * number. Pure function — no deps, no locale handling, safe on the server.
 */

export function timeAgo(
  input: Date | string | number,
  now: number = Date.now(),
): string {
  const ts =
    typeof input === "number"
      ? input
      : typeof input === "string"
        ? Date.parse(input)
        : input.getTime();
  if (!Number.isFinite(ts)) return "—";
  const diffMs = Math.max(0, now - ts);
  const seconds = Math.floor(diffMs / 1000);
  if (seconds < 5) return "now";
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h`;
  const days = Math.floor(hours / 24);
  if (days < 30) return `${days}d`;
  const months = Math.floor(days / 30);
  if (months < 12) return `${months}mo`;
  const years = Math.floor(days / 365);
  return `${years}y`;
}

/**
 * Format a timestamp as HH:MM:SS in the local timezone. Used for the live
 * ticker where absolute wall-clock helps operators correlate with their own
 * terminal scrollback.
 */
export function formatClock(input: Date | string | number): string {
  const ts =
    typeof input === "number"
      ? input
      : typeof input === "string"
        ? Date.parse(input)
        : input.getTime();
  if (!Number.isFinite(ts)) return "--:--:--";
  const d = new Date(ts);
  const hh = String(d.getHours()).padStart(2, "0");
  const mm = String(d.getMinutes()).padStart(2, "0");
  const ss = String(d.getSeconds()).padStart(2, "0");
  return `${hh}:${mm}:${ss}`;
}
