"use client";

import type { ForecastPhase } from "../../lib/api-types";
import { KIND_ICON, KIND_TONE, NODE_R, SPINE_X } from "./constants";

/**
 * ForecastRow renders one row of the dashed forecast tail below the
 * realised phase spine. Nodes are dashed-outline circles and the
 * spine segment is a dashed line so the whole block visually reads
 * as "probable but not realised".
 */
export default function ForecastRow({
  entry,
  index,
  isLast,
  hasPrior,
}: {
  entry: ForecastPhase;
  index: number;
  isLast: boolean;
  hasPrior: boolean;
}) {
  const tone = KIND_TONE[entry.kind] ?? KIND_TONE.other;
  const Icon = KIND_ICON[entry.kind] ?? KIND_ICON.other;

  return (
    <li className="flex min-h-[96px] gap-3">
      <div className="relative flex-shrink-0" style={{ width: SPINE_X * 2 }}>
        <svg
          className="h-full w-full"
          viewBox={`0 0 ${SPINE_X * 2} 100`}
          preserveAspectRatio="xMidYMid meet"
          style={{ overflow: "visible" }}
        >
          {/* Top spine — always visible for forecast rows so the
              first forecast node visually connects to the last
              real phase sitting above it. */}
          {hasPrior || index > 0 ? (
            <line
              x1={SPINE_X}
              y1="0"
              x2={SPINE_X}
              y2="50"
              stroke="var(--text-muted)"
              strokeWidth="1.5"
              strokeOpacity="0.5"
              strokeDasharray="3 4"
            />
          ) : null}
          {/* Bottom spine — hidden on the final forecast node. */}
          {!isLast && (
            <line
              x1={SPINE_X}
              y1="50"
              x2={SPINE_X}
              y2="100"
              stroke="var(--text-muted)"
              strokeWidth="1.5"
              strokeOpacity="0.5"
              strokeDasharray="3 4"
            />
          )}

          {/* Label */}
          <text
            x={SPINE_X - NODE_R - 6}
            y="54"
            textAnchor="end"
            className="fill-[var(--text-muted)] font-mono"
            style={{ fontSize: "9px" }}
          >
            NEXT
          </text>

          <circle
            cx={SPINE_X}
            cy="50"
            r={NODE_R}
            fill="var(--bg-surface)"
            stroke={tone.ring}
            strokeWidth="1.25"
            strokeDasharray="3 3"
            strokeOpacity="0.7"
          />
        </svg>
        <div
          className="pointer-events-none absolute flex items-center justify-center text-[var(--text-muted)]"
          style={{
            left: SPINE_X - NODE_R,
            top: `calc(50% - ${NODE_R}px)`,
            width: NODE_R * 2,
            height: NODE_R * 2,
          }}
        >
          <Icon size={12} strokeWidth={1.5} />
        </div>
      </div>

      <div className="flex flex-1 flex-col gap-1 pb-3 opacity-80">
        <div className="rounded border border-dashed border-[var(--border)] bg-[var(--bg-raised)]/40 p-3">
          <span
            className="font-display text-[10px] uppercase tracking-[0.14em]"
            style={{ color: tone.ring }}
          >
            Next · {tone.label}
          </span>
          <p className="mt-1 text-[12px] leading-snug text-[var(--text-muted)]">
            {entry.headline}
          </p>
          {entry.rationale ? (
            <p className="mt-1 font-mono text-[10px] text-[var(--text-muted)]">
              rationale: {entry.rationale}
            </p>
          ) : null}
        </div>
      </div>
    </li>
  );
}
