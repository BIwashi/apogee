"use client";

import Link from "next/link";
import { useMemo } from "react";

import type { PhaseBlock, Turn } from "../lib/api-types";
import { formatClock, timeAgo } from "../lib/time";
import SideDrawer from "./SideDrawer";
import { PHASE_COLORS } from "./PhaseCard";

/**
 * PhaseDrawer — the full-detail side drawer that opens when an operator
 * clicks a PhaseCard. Reuses the PR #29 SideDrawer primitive for the
 * slide animation, focus trap, Esc handler, and click-outside dismissal.
 *
 * Contents:
 *   - Headline + time range + kind chip + duration
 *   - Full narrative (no truncation)
 *   - Every key step (not just the first three)
 *   - Tool summary as a horizontal bar chart
 *   - The list of turns in this phase, each a link to /turn/
 */

interface PhaseDrawerProps {
  open: boolean;
  sessionId: string;
  phase: PhaseBlock | null;
  turns: Turn[];
  onClose: () => void;
}

function formatDuration(ms: number): string {
  if (!ms || ms < 0) return "—";
  if (ms < 1000) return `${ms}ms`;
  const seconds = Math.round(ms / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const remSec = seconds % 60;
  if (minutes < 60) return remSec ? `${minutes}m${remSec}s` : `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  const remMin = minutes % 60;
  return remMin ? `${hours}h${remMin}m` : `${hours}h`;
}

export default function PhaseDrawer({
  open,
  sessionId,
  phase,
  turns,
  onClose,
}: PhaseDrawerProps) {
  const turnIndex = useMemo(() => {
    const map = new Map<string, Turn>();
    for (const t of turns) map.set(t.turn_id, t);
    return map;
  }, [turns]);

  const phaseTurns = useMemo(() => {
    if (!phase) return [] as Turn[];
    return phase.turn_ids
      .map((id) => turnIndex.get(id))
      .filter((t): t is Turn => Boolean(t));
  }, [phase, turnIndex]);

  const toolEntries = useMemo(() => {
    if (!phase) return [] as Array<[string, number]>;
    return Object.entries(phase.tool_summary).sort((a, b) => b[1] - a[1]);
  }, [phase]);

  const maxTool = toolEntries.length > 0 ? toolEntries[0][1] : 0;

  const title = phase ? `Phase ${phase.index + 1}` : "Phase";
  const color = phase ? PHASE_COLORS[phase.kind] : "var(--text-muted)";

  return (
    <SideDrawer open={open} onClose={onClose} title={title} width="lg">
      {!phase ? null : (
        <div className="flex flex-col gap-4">
          <header className="flex flex-col gap-2 border-b border-[var(--border)] pb-3">
            <div className="flex flex-wrap items-center gap-2">
              <span
                className="rounded border px-2 py-[1px] font-mono text-[9px] uppercase tracking-[0.16em]"
                style={{ borderColor: color, color }}
              >
                {phase.kind}
              </span>
              <span className="font-mono text-[10px] text-[var(--text-muted)]">
                {formatClock(phase.started_at)} → {formatClock(phase.ended_at)}
              </span>
              <span className="font-mono text-[10px] text-[var(--text-muted)]">
                {formatDuration(phase.duration_ms)} · {phase.turn_count} turn
                {phase.turn_count === 1 ? "" : "s"}
              </span>
            </div>
            <h3 className="font-display text-[18px] leading-snug text-white">
              {phase.headline}
            </h3>
            <p className="font-mono text-[10px] text-[var(--text-muted)]">
              started {timeAgo(phase.started_at)}
            </p>
          </header>

          {phase.narrative && (
            <section>
              <h4 className="font-display text-[10px] uppercase tracking-[0.16em] text-[var(--text-muted)]">
                Narrative
              </h4>
              <p className="mt-1 whitespace-pre-line text-[12px] leading-relaxed text-white">
                {phase.narrative}
              </p>
            </section>
          )}

          {phase.key_steps.length > 0 && (
            <section>
              <h4 className="font-display text-[10px] uppercase tracking-[0.16em] text-[var(--text-muted)]">
                Key steps
              </h4>
              <ul className="mt-1 flex flex-col gap-1 text-[12px] text-white">
                {phase.key_steps.map((step, idx) => (
                  <li
                    key={`step-${phase.index}-${idx}`}
                    className="flex gap-2 leading-snug"
                  >
                    <span className="font-mono text-[10px] text-[var(--text-muted)]">
                      ·
                    </span>
                    <span>{step}</span>
                  </li>
                ))}
              </ul>
            </section>
          )}

          {toolEntries.length > 0 && (
            <section>
              <h4 className="font-display text-[10px] uppercase tracking-[0.16em] text-[var(--text-muted)]">
                Tool usage
              </h4>
              <div className="mt-1 flex flex-col gap-1">
                {toolEntries.map(([name, count]) => {
                  const width = maxTool > 0 ? Math.round((count / maxTool) * 100) : 0;
                  return (
                    <div key={name} className="flex items-center gap-2">
                      <span className="w-20 truncate font-mono text-[11px] text-white">
                        {name}
                      </span>
                      <div className="relative h-2 flex-1 rounded-full bg-[var(--bg-raised)]">
                        <div
                          className="absolute inset-y-0 left-0 rounded-full"
                          style={{ width: `${width}%`, background: color }}
                        />
                      </div>
                      <span className="w-8 text-right font-mono text-[10px] text-[var(--text-muted)] tabular-nums">
                        {count}
                      </span>
                    </div>
                  );
                })}
              </div>
            </section>
          )}

          {phaseTurns.length > 0 && (
            <section>
              <h4 className="font-display text-[10px] uppercase tracking-[0.16em] text-[var(--text-muted)]">
                Turns in this phase
              </h4>
              <ul className="mt-1 flex flex-col gap-1">
                {phaseTurns.map((t) => (
                  <li key={t.turn_id}>
                    <Link
                      href={`/turn/?sess=${sessionId}&turn=${t.turn_id}`}
                      className="flex items-center justify-between gap-3 rounded border border-transparent px-2 py-1 font-mono text-[11px] text-white transition hover:border-[var(--border)] hover:bg-[var(--bg-raised)]"
                    >
                      <span className="truncate">
                        {t.headline || t.prompt_text?.slice(0, 60) || t.turn_id}
                      </span>
                      <span className="shrink-0 text-[10px] text-[var(--text-muted)]">
                        {formatClock(t.started_at)}
                      </span>
                    </Link>
                  </li>
                ))}
              </ul>
            </section>
          )}
        </div>
      )}
    </SideDrawer>
  );
}
