"use client";

import { Loader, Square } from "lucide-react";
import type { TodoItem } from "../../lib/api-types";
import { NODE_R, SPINE_X } from "./constants";

/**
 * TodoRow renders one row of Claude's own self-declared plan,
 * sourced from the most recent TodoWrite tool call. The visual sits
 * between the realised phase spine (solid) and the tier-3 forecast
 * tail (dashed). An `in_progress` todo gets a solid filled node with
 * a pulsing outer ring so operators can see exactly which step
 * Claude thinks it is currently on; a `pending` todo gets a hollow
 * dashed node because the step is declared but not started.
 *
 * Unlike phase nodes, todo rows are not clickable — the underlying
 * span is already surfaced in the Events tab and clicking here would
 * collide with the PhaseDrawer flow. The row exists to answer the
 * question "what is Claude planning to do next", not to deep-link.
 */
export default function TodoRow({
  todo,
  index,
  isLast,
  currentRef,
}: {
  todo: TodoItem;
  index: number;
  isLast: boolean;
  currentRef?: React.Ref<HTMLLIElement>;
}) {
  const inProgress = todo.status === "in_progress";
  const label = inProgress ? "In flight" : "Planned";
  const body =
    (inProgress && todo.active_form?.trim()) ||
    todo.content?.trim() ||
    "(no content)";

  return (
    <li ref={currentRef} className="flex min-h-[80px] gap-3">
      <div className="relative flex-shrink-0" style={{ width: SPINE_X * 2 }}>
        <svg
          className="h-full w-full"
          viewBox={`0 0 ${SPINE_X * 2} 100`}
          preserveAspectRatio="xMidYMid meet"
          style={{ overflow: "visible" }}
        >
          {/* Top spine — always drawn: todo rows always sit below a
              phase node or another todo row, so there is never a gap
              above. Solid for in-progress (continues the realised
              spine); dashed for pending (forward-looking). */}
          <line
            x1={SPINE_X}
            y1="0"
            x2={SPINE_X}
            y2="50"
            stroke="var(--artemis-earth)"
            strokeWidth="1.75"
            strokeOpacity="0.5"
            strokeDasharray={inProgress ? undefined : "3 4"}
          />
          {/* Bottom spine — hidden on the final todo row unless a
              forecast tail is going to render below it. */}
          {!isLast && (
            <line
              x1={SPINE_X}
              y1="50"
              x2={SPINE_X}
              y2="100"
              stroke="var(--artemis-earth)"
              strokeWidth="1.75"
              strokeOpacity="0.5"
              strokeDasharray="3 4"
            />
          )}

          {/* Label: "TODO" column so the row visually parallels the
              numbered phase labels on PhaseRow and the "NEXT" label
              on ForecastRow. */}
          <text
            x={SPINE_X - NODE_R - 6}
            y="54"
            textAnchor="end"
            className="fill-[var(--text-muted)] font-mono"
            style={{ fontSize: "9px" }}
          >
            {inProgress ? "NOW" : `T${String(index + 1).padStart(2, "0")}`}
          </text>

          {/* In-progress nodes get a solid fill + glow so they pop on
              the spine as the current foothold. Pending nodes are
              hollow with a dashed outline. */}
          {inProgress ? (
            <>
              <circle
                cx={SPINE_X}
                cy="50"
                r={NODE_R + 3}
                fill="var(--status-success)"
                opacity="0.2"
              />
              <circle
                cx={SPINE_X}
                cy="50"
                r={NODE_R - 2}
                fill="var(--status-success)"
                opacity="0.95"
              />
              {/* Pulsing outer ring — the same SMIL cue PhaseRow uses
                  for a running turn, lifted verbatim so the two
                  visuals read as the same "alive right now" signal. */}
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
            </>
          ) : (
            <circle
              cx={SPINE_X}
              cy="50"
              r={NODE_R - 2}
              fill="var(--bg-surface)"
              stroke="var(--artemis-earth)"
              strokeWidth="1.25"
              strokeDasharray="3 3"
              strokeOpacity="0.8"
            />
          )}
        </svg>
        <div
          className="pointer-events-none absolute flex items-center justify-center"
          style={{
            left: SPINE_X - NODE_R,
            top: `calc(50% - ${NODE_R}px)`,
            width: NODE_R * 2,
            height: NODE_R * 2,
            color: inProgress ? "var(--artemis-white)" : "var(--artemis-earth)",
          }}
        >
          {inProgress ? (
            <Loader size={12} strokeWidth={1.75} className="animate-spin" />
          ) : (
            <Square size={12} strokeWidth={1.5} />
          )}
        </div>
      </div>

      <div className="flex flex-1 flex-col gap-1 pb-3">
        <div
          className={
            inProgress
              ? "mission-current-pulse rounded border border-[var(--status-success)]/60 bg-[var(--status-success)]/10 p-3"
              : "rounded border border-dashed border-[var(--artemis-earth)]/40 bg-[var(--bg-raised)]/60 p-3"
          }
        >
          <span
            className="font-display text-[10px] uppercase tracking-[0.14em]"
            style={{
              color: inProgress
                ? "var(--status-success)"
                : "var(--artemis-earth)",
            }}
          >
            {label} · todo
          </span>
          <p
            className={
              inProgress
                ? "mt-1 text-[12px] leading-snug text-[var(--artemis-white)]"
                : "mt-1 text-[12px] leading-snug text-[var(--text-muted)]"
            }
          >
            {body}
          </p>
        </div>
      </div>
    </li>
  );
}
