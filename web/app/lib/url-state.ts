"use client";

import { useCallback, useMemo } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import {
  DEFAULT_TIME_RANGE_VALUE,
  type TimeRange,
  parseTimeRange,
  serializeTimeRange,
} from "./time-range";

/**
 * url-state.ts — the single source of truth for the global ribbon selection.
 *
 * The state lives entirely in the query string so refreshing the page and
 * sharing deep links preserves exactly what the operator was looking at. All
 * three selectors (session, env, time) write through useSelection's
 * `setSelection(patch)` helper.
 *
 * Sentinels:
 *   - `sess`: session id. Empty / missing = fleet view.
 *   - `env`:  source_app. Empty / missing = all apps.
 *   - `time`: shorthand (`15m`) or custom range (`ISO1|ISO2`). Default 15m.
 */

export interface Selection {
  sess: string | null;
  env: string | null;
  time: TimeRange;
}

export interface UseSelectionResult {
  selection: Selection;
  setSelection: (patch: Partial<SelectionInput>) => void;
  /** URLSearchParams the SWR hooks compose into scoped API calls. */
  apiParams: URLSearchParams;
  /** Hard clear of every selector (fleet view). */
  clear: () => void;
}

/**
 * SelectionInput mirrors Selection but lets callers pass a string for the
 * `time` field when they want to switch to a preset without building a
 * TimeRange themselves.
 */
export interface SelectionInput {
  sess: string | null;
  env: string | null;
  time: TimeRange | string;
}

export function useSelection(): UseSelectionResult {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();

  const selection: Selection = useMemo(() => {
    const sess = searchParams.get("sess");
    const env = searchParams.get("env");
    const timeRaw = searchParams.get("time");
    return {
      sess: sess && sess.length > 0 ? sess : null,
      env: env && env.length > 0 ? env : null,
      time: parseTimeRange(timeRaw),
    };
  }, [searchParams]);

  const apiParams = useMemo(() => {
    const p = new URLSearchParams();
    if (selection.sess) p.set("session_id", selection.sess);
    if (selection.env) p.set("source_app", selection.env);
    if (selection.time.since)
      p.set("since", selection.time.since.toISOString());
    if (selection.time.until)
      p.set("until", selection.time.until.toISOString());
    return p;
  }, [selection]);

  const setSelection = useCallback(
    (patch: Partial<SelectionInput>) => {
      const next = new URLSearchParams(searchParams.toString());

      if ("sess" in patch) {
        if (patch.sess) next.set("sess", patch.sess);
        else next.delete("sess");
      }
      if ("env" in patch) {
        if (patch.env) next.set("env", patch.env);
        else next.delete("env");
      }
      if ("time" in patch && patch.time !== undefined) {
        const raw =
          typeof patch.time === "string"
            ? patch.time
            : serializeTimeRange(patch.time);
        if (raw && raw !== DEFAULT_TIME_RANGE_VALUE) next.set("time", raw);
        else next.delete("time");
      }

      const qs = next.toString();
      router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false });
    },
    [router, pathname, searchParams],
  );

  const clear = useCallback(() => {
    setSelection({ sess: null, env: null, time: DEFAULT_TIME_RANGE_VALUE });
  }, [setSelection]);

  return { selection, setSelection, apiParams, clear };
}

/**
 * buildQuery — convenience that merges a caller's own params with the
 * currently-selected scope. Used by dashboard SWR hooks so they pick up
 * session_id/source_app/since/until automatically.
 */
export function buildQuery(
  apiParams: URLSearchParams,
  extra?: Record<string, string | number | null | undefined>,
): string {
  const p = new URLSearchParams(apiParams.toString());
  if (extra) {
    for (const [k, v] of Object.entries(extra)) {
      if (v === null || v === undefined || v === "") continue;
      p.set(k, String(v));
    }
  }
  const qs = p.toString();
  return qs ? `?${qs}` : "";
}
