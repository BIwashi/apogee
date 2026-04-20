"use client";

import { Send } from "lucide-react";
import type { Intervention, PhaseBlock, Turn } from "../../lib/api-types";
import { timeAgo } from "../../lib/time";
import { KIND_ICON, KIND_TONE, NODE_R, SPINE_X } from "./constants";
import { formatDuration, shortHeadline } from "./utils";

/**
 * PhaseRow renders one row of the realised phase spine on the
 * Mission map: the spine column on the left (top + bottom segment +
 * node circle + optional running-turn pulse), and a clickable card on
 * the right with the phase content + any operator-intervention
 * branches that landed in this phase. Each row sizes to its card
 * height; the spine segments stretch to fill the row so there are no
 * gaps between consecutive nodes.
 */
export default function PhaseRow({
  phase,
  index,
  isFirst,
  isLast,
  branches,
  runningTurn,
  isCurrent,
  currentRef,
  onClick,
}: {
  phase: PhaseBlock;
  index: number;
  isFirst: boolean;
  isLast: boolean;
  branches: Intervention[];
  runningTurn: Turn | null;
  isCurrent?: boolean;
  currentRef?: React.Ref<HTMLLIElement>;
  onClick: () => void;
}) {
  const tone = KIND_TONE[phase.kind] ?? KIND_TONE.other;
  const Icon = KIND_ICON[phase.kind] ?? KIND_ICON.other;

  return (
    <li ref={currentRef} className="flex min-h-[112px] gap-3">
      {/* Graph column — fixed width, full height. Draws the spine
          segment + the node circle + branches. */}
      <div className="relative flex-shrink-0" style={{ width: SPINE_X * 2 }}>
        <svg
          className="h-full w-full"
          viewBox={`0 0 ${SPINE_X * 2} 100`}
          preserveAspectRatio="xMidYMid meet"
          style={{ overflow: "visible" }}
        >
          {/* Spine (top half): from top of row to node centre. Hidden
              on the first row so the graph visually "starts" there. */}
          {!isFirst && (
            <line
              x1={SPINE_X}
              y1="0"
              x2={SPINE_X}
              y2="50"
              stroke="var(--artemis-earth)"
              strokeWidth="2"
              strokeOpacity="0.55"
            />
          )}
          {/* Spine (bottom half): from node centre to bottom of row.
              Hidden on the very last row unless there is a forecast
              tail below. */}
          {!isLast && (
            <line
              x1={SPINE_X}
              y1="50"
              x2={SPINE_X}
              y2="100"
              stroke="var(--artemis-earth)"
              strokeWidth="2"
              strokeOpacity="0.55"
            />
          )}

          {/* Phase number label to the left of the node */}
          <text
            x={SPINE_X - NODE_R - 6}
            y="54"
            textAnchor="end"
            className="fill-[var(--text-muted)] font-mono"
            style={{ fontSize: "10px" }}
          >
            {String(index + 1).padStart(2, "0")}
          </text>

          {/* Node glow */}
          <circle
            cx={SPINE_X}
            cy="50"
            r={NODE_R + 4}
            fill={tone.fill}
            opacity="0.18"
          />
          {/* Node body */}
          <circle
            cx={SPINE_X}
            cy="50"
            r={NODE_R}
            fill={tone.fill}
            opacity="0.95"
          />
          <circle
            cx={SPINE_X}
            cy="50"
            r={NODE_R}
            fill="none"
            stroke={tone.ring}
            strokeWidth="1.5"
            opacity="0.7"
          />

          {/* Running-turn indicator on the trailing phase. A pulsing
              outer ring driven by a CSS animation. */}
          {runningTurn && (
            <circle
              cx={SPINE_X}
              cy="50"
              r={NODE_R + 6}
              fill="none"
              stroke="var(--status-success)"
              strokeWidth="1.5"
              strokeOpacity="0.9"
            >
              <animate
                attributeName="r"
                values={`${NODE_R + 2};${NODE_R + 10};${NODE_R + 2}`}
                dur="2s"
                repeatCount="indefinite"
              />
              <animate
                attributeName="stroke-opacity"
                values="0.9;0;0.9"
                dur="2s"
                repeatCount="indefinite"
              />
            </circle>
          )}
        </svg>

        {/* Kind icon sits in the middle of the node. Absolute
            positioning keeps the lucide stroke crisp at any scale. */}
        <div
          className="pointer-events-none absolute flex items-center justify-center text-[var(--artemis-white)]"
          style={{
            left: SPINE_X - NODE_R,
            top: `calc(50% - ${NODE_R}px)`,
            width: NODE_R * 2,
            height: NODE_R * 2,
          }}
        >
          <Icon size={14} strokeWidth={1.75} />
        </div>
      </div>

      {/* Right-hand card: phase content + any branch chips below.
          When this row is the "current" step (running turn lives on
          this phase, no in-progress todo), the card border softly
          pulses so operators can spot the foothold even while
          scrolling through a long timeline. */}
      <div className="flex flex-1 flex-col gap-2 pb-4">
        <button
          type="button"
          onClick={onClick}
          className={`group flex flex-col items-start gap-1 rounded border bg-[var(--bg-raised)] p-3 text-left transition-colors hover:bg-[var(--bg-overlay)] ${
            isCurrent
              ? "mission-current-pulse border-[var(--status-success)]/60"
              : "border-[var(--border)]"
          }`}
        >
          <div className="flex w-full items-center justify-between gap-2">
            <span
              className="font-display text-[10px] uppercase tracking-[0.14em]"
              style={{ color: tone.ring }}
            >
              {tone.label}
            </span>
            <span className="font-mono text-[10px] text-[var(--text-muted)]">
              {phase.turn_count} turn{phase.turn_count === 1 ? "" : "s"}
              {formatDuration(phase.duration_ms)
                ? ` · ${formatDuration(phase.duration_ms)}`
                : ""}
              {" · "}
              {timeAgo(phase.started_at)}
            </span>
          </div>
          <p className="text-[13px] leading-snug text-[var(--artemis-white)]">
            {phase.headline}
          </p>
          {phase.narrative ? (
            <p className="text-[11px] leading-snug text-[var(--text-muted)]">
              {shortHeadline(phase.narrative, 180)}
            </p>
          ) : null}
          {phase.key_steps.length > 0 ? (
            <ul className="mt-1 flex flex-col gap-0.5 text-[11px] leading-snug text-[var(--text-muted)]">
              {phase.key_steps.slice(0, 3).map((step, idx) => (
                <li key={idx} className="flex gap-1">
                  <span className="text-[var(--artemis-earth)]">·</span>
                  <span>{step}</span>
                </li>
              ))}
              {phase.key_steps.length > 3 ? (
                <li className="ml-2 font-mono text-[10px] text-[var(--text-muted)]">
                  +{phase.key_steps.length - 3} more
                </li>
              ) : null}
            </ul>
          ) : null}
        </button>

        {/* Branches — one row per intervention that landed in this
            phase. Drawn as an indented flex strip so they visually
            hang off the phase card. */}
        {branches.length > 0 && (
          <ul className="flex flex-col gap-1 pl-4">
            {branches.map((iv) => (
              <li
                key={iv.intervention_id}
                className="flex items-start gap-2 rounded border border-[var(--artemis-red)]/40 bg-[var(--artemis-red)]/10 p-2"
              >
                <div className="flex h-5 w-5 flex-shrink-0 items-center justify-center rounded-full bg-[var(--artemis-red)]/30 text-[var(--artemis-red)]">
                  <Send size={10} strokeWidth={1.75} />
                </div>
                <div className="flex-1 min-w-0">
                  <p className="font-display text-[9px] uppercase tracking-[0.14em] text-[var(--artemis-red)]">
                    Side quest · {iv.delivery_mode}
                  </p>
                  <p className="text-[11px] leading-snug text-[var(--artemis-white)]">
                    {shortHeadline(iv.message, 140)}
                  </p>
                  <p className="mt-0.5 font-mono text-[9px] text-[var(--text-muted)]">
                    injected {timeAgo(iv.created_at)} · {iv.status}
                  </p>
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>
    </li>
  );
}
