/**
 * Typed client wrappers around the /v1/preferences API. The collector
 * persists every key in DuckDB; this module owns the wire shape and the
 * fetch boilerplate so call sites stay tiny.
 */

import { apiFetch, apiUrl } from "./api";
import type { PreferencesResponse, SummarizerPreferences } from "./api-types";

/** GET /v1/preferences — returns the merged defaults + persisted view. */
export async function fetchPreferences(): Promise<PreferencesResponse> {
  return apiFetch<PreferencesResponse>("/v1/preferences");
}

/**
 * PATCH /v1/preferences — sparse merge update. Only the keys present in
 * patch are written; everything else is left alone. Throws on non-2xx so
 * callers can surface the validation error from the collector body.
 */
export async function patchPreferences(
  patch: SummarizerPreferences,
): Promise<PreferencesResponse> {
  const resp = await fetch(apiUrl("/v1/preferences"), {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(patch),
  });
  if (!resp.ok) {
    let detail = `${resp.status} ${resp.statusText}`;
    try {
      const body = (await resp.json()) as { error?: string };
      if (body.error) detail = body.error;
    } catch {
      // body was not JSON — keep the status text fallback
    }
    throw new Error(`patch preferences: ${detail}`);
  }
  return (await resp.json()) as PreferencesResponse;
}

/** DELETE /v1/preferences — wipes every summarizer.* override. */
export async function resetPreferences(): Promise<PreferencesResponse> {
  const resp = await fetch(apiUrl("/v1/preferences"), { method: "DELETE" });
  if (!resp.ok) {
    throw new Error(`reset preferences: ${resp.status} ${resp.statusText}`);
  }
  return (await resp.json()) as PreferencesResponse;
}

/**
 * Hard-coded defaults used by the UI before the first GET resolves and as
 * the "revert" target on the settings form. Mirrors
 * internal/summarizer.Defaults() / collector.buildPreferencesResponse.
 */
export const DEFAULT_SUMMARIZER_PREFERENCES: Required<SummarizerPreferences> = {
  "summarizer.language": "en",
  "summarizer.recap_system_prompt": "",
  "summarizer.rollup_system_prompt": "",
  "summarizer.recap_model": "",
  "summarizer.rollup_model": "",
};
