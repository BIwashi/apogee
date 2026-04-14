/**
 * API client — resolves the base URL for fetch calls against the apogee
 * collector.
 *
 * The production binary serves the static export and the /v1 API from the
 * same origin, so every call uses a relative path (`/v1/...`) and
 * `NEXT_PUBLIC_API_URL` is left empty. In dev mode Next.js rewrites forward
 * `/v1/*` from the :3000 dev server to the collector on :4100 (see
 * `next.config.ts`), so the same relative URLs work during local development
 * without touching this client.
 *
 * Usage:
 *   const data = await apiFetch<Session[]>("/v1/sessions/recent");
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
