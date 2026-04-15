"use client";

import { useMemo, useState } from "react";
import { ChevronDown, ChevronRight } from "lucide-react";
import type { FacetDimension, FacetValue } from "../lib/api-types";

/**
 * FacetGroup — one collapsible section inside the /events page left rail.
 * Renders a header (e.g. "SOURCE APP") + a checkbox list of distinct
 * values sorted by count descending. Click anywhere on a row to toggle
 * that value in the selection set; the parent FacetPanel owns the set
 * and threads it down as a URL-backed `selected` Set.
 *
 * When the number of values exceeds `initialVisibleCount` the group
 * surfaces a "Show more" affordance so the 50-value backend ceiling can
 * still scroll into view without occupying the full viewport at rest.
 */

const INITIAL_VISIBLE = 8;

interface FacetGroupProps {
  dimension: FacetDimension;
  label: string;
  selected: ReadonlySet<string>;
  onToggle: (value: string) => void;
}

export default function FacetGroup({
  dimension,
  label,
  selected,
  onToggle,
}: FacetGroupProps) {
  const [open, setOpen] = useState(true);
  const [showAll, setShowAll] = useState(false);

  const visible = useMemo<FacetValue[]>(() => {
    if (showAll) return dimension.values;
    return dimension.values.slice(0, INITIAL_VISIBLE);
  }, [dimension.values, showAll]);

  const hasMore = dimension.values.length > INITIAL_VISIBLE;
  const empty = dimension.values.length === 0;

  return (
    <section className="border-b border-[var(--border)] py-2 last:border-b-0">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center gap-2 px-3 py-1 font-display text-[10px] uppercase tracking-[0.14em] text-[var(--text-muted)] hover:text-[var(--artemis-white)]"
        aria-expanded={open}
      >
        {open ? (
          <ChevronDown size={12} strokeWidth={2} />
        ) : (
          <ChevronRight size={12} strokeWidth={2} />
        )}
        <span>{label}</span>
      </button>
      {open && (
        <ul className="mt-1 flex flex-col">
          {empty && (
            <li className="px-5 py-1 font-mono text-[10px] text-[var(--text-muted)]">
              no values
            </li>
          )}
          {visible.map((v) => {
            const isSelected = selected.has(v.value);
            return (
              <li key={v.value}>
                <label
                  className={`group flex cursor-pointer items-center gap-2 px-3 py-[3px] font-mono text-[11px] transition-colors hover:bg-[var(--bg-raised)] ${
                    isSelected
                      ? "text-[var(--artemis-white)]"
                      : "text-[var(--text-muted)]"
                  }`}
                >
                  <input
                    type="checkbox"
                    checked={isSelected}
                    onChange={() => onToggle(v.value)}
                    className="h-[11px] w-[11px] accent-[var(--status-info)]"
                    aria-label={`Toggle ${label} ${v.value}`}
                  />
                  <span className="flex-1 truncate" title={v.value}>
                    {v.value}
                  </span>
                  <span className="tabular-nums text-[10px]">
                    {v.count.toLocaleString()}
                  </span>
                </label>
              </li>
            );
          })}
          {hasMore && !showAll && (
            <li>
              <button
                type="button"
                onClick={() => setShowAll(true)}
                className="w-full px-5 py-1 text-left font-mono text-[10px] text-[var(--text-muted)] hover:text-[var(--artemis-white)]"
              >
                show {dimension.values.length - INITIAL_VISIBLE} more
              </button>
            </li>
          )}
          {hasMore && showAll && (
            <li>
              <button
                type="button"
                onClick={() => setShowAll(false)}
                className="w-full px-5 py-1 text-left font-mono text-[10px] text-[var(--text-muted)] hover:text-[var(--artemis-white)]"
              >
                show less
              </button>
            </li>
          )}
        </ul>
      )}
    </section>
  );
}
