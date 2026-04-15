/**
 * recent-sessions.ts — localStorage cache for the last 5 sessions picked in
 * the command palette. Uses an LRU eviction policy: the most-recently
 * activated session moves to the head and the tail drops off when the list
 * is full.
 *
 * Storage key: `apogee:recent-sessions`. The cached shape is intentionally
 * tight — we keep just enough to render a row without a network round-trip
 * when the palette opens, and a full `SessionSearchHit` arrives later via
 * the ACTIVE / ALL SESSIONS queries.
 */

export interface RecentSessionEntry {
  session_id: string;
  source_app: string;
  label: string;
  last_seen_at: string;
}

const STORAGE_KEY = "apogee:recent-sessions";
const MAX = 5;

function isBrowser(): boolean {
  return (
    typeof window !== "undefined" && typeof window.localStorage !== "undefined"
  );
}

export function getRecentSessions(): RecentSessionEntry[] {
  if (!isBrowser()) return [];
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw) as unknown;
    if (!Array.isArray(parsed)) return [];
    return parsed
      .filter((x): x is RecentSessionEntry => {
        if (!x || typeof x !== "object") return false;
        const r = x as Record<string, unknown>;
        return (
          typeof r.session_id === "string" &&
          typeof r.source_app === "string" &&
          typeof r.label === "string" &&
          typeof r.last_seen_at === "string"
        );
      })
      .slice(0, MAX);
  } catch {
    return [];
  }
}

export function addRecentSession(
  entry: RecentSessionEntry,
): RecentSessionEntry[] {
  if (!isBrowser()) return [];
  const existing = getRecentSessions().filter(
    (x) => x.session_id !== entry.session_id,
  );
  const next = [entry, ...existing].slice(0, MAX);
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
  } catch {
    // Private mode or quota — swallow.
  }
  return next;
}

export function clearRecentSessions(): void {
  if (!isBrowser()) return;
  try {
    window.localStorage.removeItem(STORAGE_KEY);
  } catch {
    // Swallow.
  }
}
