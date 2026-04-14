/**
 * API client — resolves the base URL for fetch calls against the apogee
 * collector.
 *
 * In development, Next.js rewrites proxy `/api/*` to the collector running on
 * :8000 (see next.config.ts). In production the Go binary serves both the
 * embedded web UI and the API from the same origin, so `NEXT_PUBLIC_API_URL`
 * is left empty and we emit same-origin URLs.
 *
 * Usage:
 *   const data = await apiFetch<Session[]>("/api/v1/sessions");
 */

const API_BASE = process.env.NEXT_PUBLIC_API_URL ?? "";

export function apiUrl(path: string): string {
  return `${API_BASE}${path}`;
}

export async function apiFetch<T = unknown>(
  path: string,
  init?: RequestInit,
): Promise<T> {
  const resp = await fetch(apiUrl(path), init);
  if (!resp.ok) {
    throw new Error(`apogee API error: ${resp.status} ${resp.statusText}`);
  }
  return resp.json() as Promise<T>;
}
