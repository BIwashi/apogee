"use client";

import {
  AlertOctagon,
  Layers,
  MessageSquare,
  Shield,
  TerminalSquare,
  Users,
  Wrench,
  type LucideIcon,
} from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";
import { useCallback, useMemo } from "react";

import type { FilterKey } from "../lib/api-types";
import { FILTER_KEYS } from "../lib/api-types";

/**
 * FilterChips — single-select chip group for the turn detail page. The
 * selected key is persisted in the `?filter=` URL query param so deep links
 * (e.g. ".../turns/abc?filter=errors") highlight the same view on reload.
 *
 * Chips render with a lucide icon and a short label. The active chip uses
 * the accent color; inactive chips fade to the muted text token.
 */

interface ChipDef {
  key: FilterKey;
  label: string;
  icon: LucideIcon;
}

const CHIPS: ChipDef[] = [
  { key: "all", label: "All", icon: Layers },
  { key: "commands", label: "Commands", icon: TerminalSquare },
  { key: "messages", label: "Messages", icon: MessageSquare },
  { key: "tools", label: "Tools", icon: Wrench },
  { key: "errors", label: "Errors", icon: AlertOctagon },
  { key: "hitl", label: "HITL", icon: Shield },
  { key: "subagents", label: "Subagents", icon: Users },
];

/**
 * useFilterState — hook that reads the `?filter=` query param and returns
 * the active filter plus a setter that updates the URL via the Next.js
 * router. The setter accepts `null` or `"all"` to clear the filter.
 */
export function useFilterState(): [FilterKey, (next: FilterKey) => void] {
  const router = useRouter();
  const params = useSearchParams();
  const raw = params.get("filter");
  const active: FilterKey = useMemo(() => {
    if (raw && (FILTER_KEYS as string[]).includes(raw)) {
      return raw as FilterKey;
    }
    return "all";
  }, [raw]);

  const setFilter = useCallback(
    (next: FilterKey) => {
      const url = new URL(window.location.href);
      if (next === "all") {
        url.searchParams.delete("filter");
      } else {
        url.searchParams.set("filter", next);
      }
      router.replace(url.pathname + url.search, { scroll: false });
    },
    [router],
  );

  return [active, setFilter];
}

interface FilterChipsProps {
  active: FilterKey;
  onChange: (next: FilterKey) => void;
}

export default function FilterChips({ active, onChange }: FilterChipsProps) {
  return (
    <div
      role="tablist"
      aria-label="Span filter"
      className="flex flex-wrap items-center gap-1.5"
    >
      {CHIPS.map(({ key, label, icon: Icon }) => {
        const isActive = active === key;
        const base =
          "inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-[11px] font-medium transition-colors focus:outline-none focus-visible:ring-1 focus-visible:ring-[var(--border-bright)]";
        const tone = isActive
          ? "border-[var(--accent)] bg-[var(--accent)]/10 text-[var(--accent)]"
          : "border-[var(--border)] bg-[var(--bg-surface)] text-[var(--text-muted)] hover:border-[var(--border-bright)] hover:text-[var(--artemis-white)]";
        return (
          <button
            key={key}
            role="tab"
            aria-selected={isActive}
            type="button"
            onClick={() => onChange(key)}
            className={`${base} ${tone}`}
          >
            <Icon size={13} strokeWidth={1.5} aria-hidden />
            <span>{label}</span>
          </button>
        );
      })}
    </div>
  );
}
