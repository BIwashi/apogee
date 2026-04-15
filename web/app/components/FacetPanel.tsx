"use client";

import { useCallback, useMemo } from "react";
import type { FacetDimension } from "../lib/api-types";
import FacetGroup from "./FacetGroup";

/**
 * FacetPanel — Datadog-style left rail on the /events page. Owns the
 * selected values per dimension (as a Record<dimensionKey, Set<string>>)
 * and surfaces a "CLEAR ALL" affordance at the top when anything is
 * selected. Selection changes fire the onSelectionChange callback; the
 * parent is responsible for serialising the Sets into URL query params
 * (`?facets.source_app=a,b`) so deep links reproduce the exact filter.
 *
 * The panel is intentionally stateless — it reads the current selection
 * from props so it can re-render when the URL updates via the browser's
 * back/forward buttons without having to reconcile local state.
 */

export type FacetSelections = Record<string, ReadonlySet<string>>;

interface FacetPanelProps {
  facets: FacetDimension[] | undefined;
  loading: boolean;
  selections: FacetSelections;
  onSelectionChange: (next: FacetSelections) => void;
}

const FACET_LABELS: Record<string, string> = {
  source_app: "SOURCE APP",
  hook_event: "HOOK EVENT",
  severity_text: "SEVERITY",
  session_id: "SESSION",
};

const FACET_ORDER = ["source_app", "hook_event", "severity_text", "session_id"];

function countSelected(selections: FacetSelections): number {
  let n = 0;
  for (const key of Object.keys(selections)) {
    n += selections[key]?.size ?? 0;
  }
  return n;
}

export default function FacetPanel({
  facets,
  loading,
  selections,
  onSelectionChange,
}: FacetPanelProps) {
  const totalSelected = countSelected(selections);

  const byKey = useMemo<Record<string, FacetDimension>>(() => {
    const out: Record<string, FacetDimension> = {};
    for (const dim of facets ?? []) {
      out[dim.key] = dim;
    }
    return out;
  }, [facets]);

  const onToggle = useCallback(
    (dimKey: string, value: string) => {
      const current = new Set(selections[dimKey] ?? []);
      if (current.has(value)) {
        current.delete(value);
      } else {
        current.add(value);
      }
      const next: FacetSelections = { ...selections };
      if (current.size === 0) {
        delete next[dimKey];
      } else {
        next[dimKey] = current;
      }
      onSelectionChange(next);
    },
    [selections, onSelectionChange],
  );

  const onClearAll = useCallback(() => {
    onSelectionChange({});
  }, [onSelectionChange]);

  return (
    <aside
      aria-label="Event facets"
      className="flex w-full flex-col overflow-hidden border border-[var(--border)] bg-[var(--bg-card)] md:w-[240px] md:flex-shrink-0"
    >
      <header className="flex items-center justify-between border-b border-[var(--border)] px-3 py-2">
        <span className="font-display text-[11px] uppercase tracking-[0.14em] text-[var(--artemis-white)]">
          Facets
        </span>
        {totalSelected > 0 && (
          <button
            type="button"
            onClick={onClearAll}
            className="font-mono text-[10px] text-[var(--text-muted)] hover:text-[var(--artemis-white)]"
          >
            clear all ({totalSelected})
          </button>
        )}
      </header>
      {loading && !facets && (
        <p className="px-3 py-3 font-mono text-[11px] text-[var(--text-muted)]">
          Loading facets…
        </p>
      )}
      <div className="max-h-[70vh] overflow-y-auto md:max-h-none">
        {FACET_ORDER.map((key) => {
          const dim = byKey[key];
          if (!dim) return null;
          return (
            <FacetGroup
              key={key}
              dimension={dim}
              label={FACET_LABELS[key] ?? key.toUpperCase()}
              selected={selections[key] ?? new Set<string>()}
              onToggle={(v) => onToggle(key, v)}
            />
          );
        })}
      </div>
    </aside>
  );
}
