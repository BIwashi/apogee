"use client";

import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type MouseEvent,
} from "react";
import {
  Bug,
  CheckCircle2,
  ClipboardList,
  Code2,
  Compass,
  Eye,
  GitBranch,
  Sparkles,
  Users,
  type LucideIcon,
} from "lucide-react";

import type { PhaseBlock, PhaseKind, Turn } from "../lib/api-types";
import { formatClock, timeAgo } from "../lib/time";

/**
 * PhaseCard — a single phase rendered as a clickable card with a hover
 * preview. The card is the primary interaction surface on the Timeline
 * tab: clicking opens a side drawer with the full phase detail, and
 * hovering shows a floating preview after a 350ms dwell.
 *
 * Keyboard: the card is focusable; Enter opens the drawer, Shift+Enter
 * toggles the hover preview so keyboard users can access the same
 * information as mouse users.
 */

interface PhaseCardProps {
  phase: PhaseBlock;
  turns: Turn[];
  onClick: (phase: PhaseBlock, turns: Turn[]) => void;
  onHover?: (phase: PhaseBlock, turns: Turn[]) => void;
}

const PHASE_ICONS: Record<PhaseKind, LucideIcon> = {
  implement: Code2,
  review: Eye,
  debug: Bug,
  plan: ClipboardList,
  test: CheckCircle2,
  commit: GitBranch,
  delegate: Users,
  explore: Compass,
  other: Sparkles,
};

export const PHASE_COLORS: Record<PhaseKind, string> = {
  implement: "var(--status-info)",
  review: "var(--artemis-earth)",
  debug: "var(--status-warning)",
  plan: "var(--text-muted)",
  test: "var(--status-warning)",
  commit: "var(--status-success)",
  delegate: "var(--accent)",
  explore: "var(--status-info)",
  other: "var(--text-muted)",
};

const HOVER_DELAY_MS = 350;

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

export default function PhaseCard({
  phase,
  turns,
  onClick,
  onHover,
}: PhaseCardProps) {
  const [previewOpen, setPreviewOpen] = useState(false);
  const hoverTimer = useRef<number | null>(null);
  const cardRef = useRef<HTMLButtonElement | null>(null);

  const Icon = PHASE_ICONS[phase.kind] ?? Sparkles;
  const color = PHASE_COLORS[phase.kind] ?? "var(--text-muted)";

  const clearHoverTimer = useCallback(() => {
    if (hoverTimer.current !== null) {
      window.clearTimeout(hoverTimer.current);
      hoverTimer.current = null;
    }
  }, []);

  useEffect(() => {
    return () => {
      clearHoverTimer();
    };
  }, [clearHoverTimer]);

  const onMouseEnter = useCallback(() => {
    clearHoverTimer();
    hoverTimer.current = window.setTimeout(() => {
      setPreviewOpen(true);
      onHover?.(phase, turns);
    }, HOVER_DELAY_MS);
  }, [clearHoverTimer, onHover, phase, turns]);

  const onMouseLeave = useCallback(() => {
    clearHoverTimer();
    setPreviewOpen(false);
  }, [clearHoverTimer]);

  const handleClick = useCallback(
    (_: MouseEvent<HTMLButtonElement>) => {
      clearHoverTimer();
      setPreviewOpen(false);
      onClick(phase, turns);
    },
    [clearHoverTimer, onClick, phase, turns],
  );

  const handleKeyDown = useCallback(
    (e: KeyboardEvent<HTMLButtonElement>) => {
      if (e.key === "Enter" && e.shiftKey) {
        e.preventDefault();
        setPreviewOpen((o) => !o);
        return;
      }
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        onClick(phase, turns);
      }
    },
    [onClick, phase, turns],
  );

  const narrativeSnippet = phase.narrative
    ? phase.narrative.length > 180
      ? `${phase.narrative.slice(0, 180)}…`
      : phase.narrative
    : "";
  const visibleSteps = phase.key_steps.slice(0, 3);
  const extraSteps = phase.key_steps.length - visibleSteps.length;

  return (
    <div className="relative">
      <button
        ref={cardRef}
        type="button"
        onClick={handleClick}
        onKeyDown={handleKeyDown}
        onMouseEnter={onMouseEnter}
        onMouseLeave={onMouseLeave}
        className="group w-full rounded-[10px] border border-[var(--border)] bg-[var(--bg-surface)] p-4 text-left transition-colors hover:border-[var(--border-bright)] focus:outline-none focus-visible:border-[var(--border-bright)] focus-visible:ring-1 focus-visible:ring-[var(--border-bright)]"
        aria-label={`Phase ${phase.index + 1}: ${phase.headline}. Press Enter to open details, Shift+Enter to preview.`}
      >
        <div className="flex items-start gap-3">
          <div
            className="mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-full border"
            style={{ borderColor: color, color }}
            aria-hidden
          >
            <Icon size={14} strokeWidth={1.5} />
          </div>
          <div className="flex min-w-0 flex-1 flex-col gap-2">
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-display text-[10px] uppercase tracking-[0.16em] text-[var(--text-muted)]">
                {formatClock(phase.started_at)}
              </span>
              <span
                className="rounded border px-2 py-[1px] font-mono text-[9px] uppercase tracking-[0.14em]"
                style={{ borderColor: color, color }}
              >
                {phase.kind}
              </span>
              <span className="font-mono text-[10px] text-[var(--text-muted)]">
                {phase.turn_count} turn{phase.turn_count === 1 ? "" : "s"} · {formatDuration(phase.duration_ms)}
              </span>
            </div>
            <p className="text-[14px] font-medium leading-snug text-white">
              {phase.headline}
            </p>
            {narrativeSnippet && (
              <p className="text-[12px] leading-relaxed text-[var(--text-muted)]">
                {narrativeSnippet}
              </p>
            )}
            {visibleSteps.length > 0 && (
              <ul className="flex flex-col gap-0.5 pl-1 text-[11px] text-[var(--text-muted)]">
                {visibleSteps.map((step, idx) => (
                  <li key={`${phase.index}-${idx}`} className="flex gap-2 leading-snug">
                    <span className="font-mono text-[10px]">·</span>
                    <span className="text-gray-200">{step}</span>
                  </li>
                ))}
                {extraSteps > 0 && (
                  <li className="ml-2 font-mono text-[10px] text-[var(--text-muted)]">
                    +{extraSteps} more
                  </li>
                )}
              </ul>
            )}
          </div>
        </div>
      </button>

      {previewOpen && (
        <div
          role="tooltip"
          className="pointer-events-none absolute left-8 top-full z-20 mt-2 w-[360px] rounded-[10px] border border-[var(--border-bright)] bg-[var(--bg-raised)] p-3 shadow-xl"
          style={{ borderColor: color }}
        >
          <p className="font-display text-[11px] uppercase tracking-[0.16em] text-[var(--text-muted)]">
            {phase.kind} · {phase.turn_count} turn{phase.turn_count === 1 ? "" : "s"} · {formatDuration(phase.duration_ms)}
          </p>
          <p className="mt-1 text-[12px] font-medium text-white">{phase.headline}</p>
          {phase.narrative && (
            <p className="mt-2 whitespace-pre-line text-[11px] leading-relaxed text-[var(--text-muted)]">
              {phase.narrative}
            </p>
          )}
          {phase.key_steps.length > 0 && (
            <ul className="mt-2 flex flex-col gap-0.5 text-[11px]">
              {phase.key_steps.map((step, idx) => (
                <li key={`preview-${phase.index}-${idx}`} className="flex gap-2 leading-snug text-gray-200">
                  <span className="font-mono text-[10px] text-[var(--text-muted)]">·</span>
                  <span>{step}</span>
                </li>
              ))}
            </ul>
          )}
          {Object.keys(phase.tool_summary).length > 0 && (
            <p className="mt-2 font-mono text-[10px] text-[var(--text-muted)]">
              tools:{" "}
              {Object.entries(phase.tool_summary)
                .sort((a, b) => b[1] - a[1])
                .slice(0, 6)
                .map(([name, count]) => `${name}:${count}`)
                .join(" · ")}
            </p>
          )}
          <p className="mt-2 font-mono text-[9px] uppercase tracking-[0.14em] text-[var(--text-muted)]">
            {timeAgo(phase.started_at)}
          </p>
        </div>
      )}
    </div>
  );
}
