"use client";

import type { AttentionCounts, AttentionState } from "../lib/api-types";
import { ATTENTION_STATES } from "../lib/api-types";

/**
 * CountPills — four pill buttons, one per attention bucket. Clicking a pill
 * toggles a client-side filter on the active-turns list. Colors come from
 * the existing --status-* palette via hard-coded tone names so the backend
 * does not need to send a tone for this aggregate endpoint.
 */

interface CountPillsProps {
  counts: AttentionCounts | undefined;
  activeFilter?: AttentionState | null;
  onSelect: (state: AttentionState | null) => void;
}

interface PillSpec {
  state: AttentionState;
  label: string;
  tone: "critical" | "warning" | "info" | "success";
}

const PILLS: PillSpec[] = [
  { state: "intervene_now", label: "INTERVENE", tone: "critical" },
  { state: "watch", label: "WATCH", tone: "warning" },
  { state: "watchlist", label: "WATCHLIST", tone: "info" },
  { state: "healthy", label: "HEALTHY", tone: "success" },
];

const BG_BY_TONE: Record<PillSpec["tone"], string> = {
  critical: "var(--tint-critical)",
  warning: "var(--tint-warning)",
  info: "var(--tint-info)",
  success: "var(--tint-success)",
};

const COLOR_BY_TONE: Record<PillSpec["tone"], string> = {
  critical: "var(--status-critical)",
  warning: "var(--status-warning)",
  info: "var(--status-info)",
  success: "var(--status-success)",
};

function countFor(
  counts: AttentionCounts | undefined,
  state: AttentionState,
): number {
  if (!counts) return 0;
  return counts[state] ?? 0;
}

export default function CountPills({
  counts,
  activeFilter,
  onSelect,
}: CountPillsProps) {
  void ATTENTION_STATES; // kept to surface the re-export in one file
  return (
    <div className="flex flex-wrap items-center gap-2">
      {PILLS.map((pill) => {
        const n = countFor(counts, pill.state);
        const isActive = activeFilter === pill.state;
        const next: AttentionState | null = isActive ? null : pill.state;
        return (
          <button
            key={pill.state}
            type="button"
            onClick={() => onSelect(next)}
            style={{
              background: BG_BY_TONE[pill.tone],
              color: COLOR_BY_TONE[pill.tone],
              borderColor: isActive
                ? COLOR_BY_TONE[pill.tone]
                : "var(--border)",
            }}
            className={`inline-flex items-center gap-2 rounded-[4px] border px-3 py-[6px] text-[11px] font-medium transition-colors hover:brightness-125 ${
              isActive ? "ring-1 ring-current" : ""
            }`}
          >
            <span className="font-display tracking-[0.12em]">{pill.label}</span>
            <span className="font-mono tabular-nums">{n}</span>
          </button>
        );
      })}
    </div>
  );
}
