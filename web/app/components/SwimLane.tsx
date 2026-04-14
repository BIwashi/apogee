"use client";

import { useMemo } from "react";

import type {
  FilterKey,
  HITLEvent,
  PhaseSegment,
  Span,
  Turn,
} from "../lib/api-types";

/**
 * SwimLane — SVG visualization of a turn's spans laid out across five rows:
 *
 *   row 0  turn       single bar covering the full turn duration
 *   row 1  phase      contiguous segments coloured by phase name
 *   row 2  tool       one bar per tool span, status-coloured
 *   row 3  subagent   one bar per subagent span, accent-coloured
 *   row 4  hitl       triangle markers at HITL span start times
 *
 * The component is a single inline <svg> with a viewBox so it scales with
 * the parent container. No external chart lib. Phase segments come from the
 * collector (computed by internal/attention.PhaseSegments) so the colours
 * stay in sync with the dashboard. The optional highlightedFilter prop
 * desaturates non-matching bars when a filter chip is active.
 */

interface SwimLaneProps {
  turn: Turn;
  spans: Span[];
  phases: PhaseSegment[];
  highlightedFilter?: FilterKey;
  hitlEvents?: HITLEvent[];
}

const ROW_HEIGHT = 18;
const ROW_GAP = 6;
const LABEL_WIDTH = 64;
const PADDING = 10;
const TIME_AXIS_HEIGHT = 18;
const ROWS = 5;
const VIEWBOX_HEIGHT =
  TIME_AXIS_HEIGHT + ROWS * ROW_HEIGHT + (ROWS - 1) * ROW_GAP + PADDING * 2;
const VIEWBOX_WIDTH = 1000;

const PHASE_COLOR: Record<string, string> = {
  plan: "var(--status-info)",
  planning: "var(--status-info)",
  exploring: "var(--status-info)",
  editing: "var(--artemis-earth)",
  testing: "var(--status-warning)",
  committing: "var(--status-success)",
  delegating: "var(--accent)",
  running: "var(--status-info)",
  idle: "var(--status-muted)",
  verify: "var(--status-success)",
};

function phaseColor(name: string): string {
  return PHASE_COLOR[name] ?? "var(--status-muted)";
}

function statusColor(status: string): string {
  switch (status) {
    case "OK":
      return "var(--status-success)";
    case "ERROR":
      return "var(--status-critical)";
    default:
      return "var(--status-muted)";
  }
}

function parseTime(value: string | undefined | null): number | null {
  if (!value) return null;
  const ms = Date.parse(value);
  return Number.isFinite(ms) ? ms : null;
}

function formatOffset(ms: number): string {
  const seconds = Math.max(0, Math.round(ms / 1000));
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const remainder = seconds % 60;
  if (remainder === 0) return `${minutes}m`;
  return `${minutes}m${remainder}s`;
}

interface ScaledBar {
  x: number;
  width: number;
}

function scaleBar(
  start: number,
  end: number,
  windowStart: number,
  windowEnd: number,
  laneStart: number,
  laneWidth: number,
): ScaledBar {
  const span = Math.max(1, windowEnd - windowStart);
  const rawX = ((start - windowStart) / span) * laneWidth;
  const rawW = ((end - start) / span) * laneWidth;
  const x = Math.max(0, rawX) + laneStart;
  const width = Math.max(2, rawW); // 2px floor so sub-1px bars stay visible
  return { x, width };
}

function isToolSpan(span: Span): boolean {
  return Boolean(span.tool_name) || span.name.startsWith("claude_code.tool");
}

function isSubagentSpan(span: Span): boolean {
  return span.name.startsWith("claude_code.subagent");
}

function isHitlSpan(span: Span): boolean {
  return span.name === "claude_code.hitl.permission";
}

function isCommandSpan(span: Span): boolean {
  return span.tool_name === "Bash" || span.tool_name === "KillShell";
}

function isErrorSpan(span: Span): boolean {
  return span.status_code === "ERROR";
}

/**
 * dimForFilter returns the opacity multiplier a bar should render with given
 * the active filter. A filter of "all" never dims anything. Filter "messages"
 * leaves only the turn bar visible because messages live as span events on
 * the turn root, not as their own spans.
 */
function dimForFilter(span: Span, filter: FilterKey | undefined): number {
  if (!filter || filter === "all") return 1;
  switch (filter) {
    case "tools":
      return isToolSpan(span) ? 1 : 0.18;
    case "commands":
      return isCommandSpan(span) ? 1 : 0.18;
    case "errors":
      return isErrorSpan(span) ? 1 : 0.18;
    case "hitl":
      return isHitlSpan(span) ? 1 : 0.18;
    case "subagents":
      return isSubagentSpan(span) ? 1 : 0.18;
    case "messages":
      return 0.18;
    default:
      return 1;
  }
}

function hitlMarkerColor(event: HITLEvent | undefined): string {
  if (!event) return "var(--status-warning)";
  if (event.status === "pending") return "var(--status-warning)";
  if (event.status === "expired" || event.status === "timeout") {
    return "var(--status-muted)";
  }
  if (event.decision === "deny") return "var(--status-critical)";
  if (event.decision === "allow") return "var(--status-success)";
  return "var(--artemis-earth)";
}

export default function SwimLane({
  turn,
  spans,
  phases,
  highlightedFilter,
  hitlEvents,
}: SwimLaneProps) {
  const hitlBySpan = useMemo(() => {
    const map = new Map<string, HITLEvent>();
    for (const ev of hitlEvents ?? []) {
      if (ev.span_id) map.set(ev.span_id, ev);
    }
    return map;
  }, [hitlEvents]);
  const laneStart = LABEL_WIDTH + PADDING;
  const laneWidth = VIEWBOX_WIDTH - laneStart - PADDING;
  const windowStart = parseTime(turn.started_at) ?? 0;
  const fallbackEnd =
    parseTime(turn.ended_at) ??
    spans.reduce<number>((acc, sp) => {
      const end = parseTime(sp.end_time) ?? parseTime(sp.start_time) ?? acc;
      return Math.max(acc, end);
    }, windowStart);
  const windowEnd = Math.max(windowStart + 1000, fallbackEnd);

  const ticks = useMemo(() => {
    const out: { x: number; label: string }[] = [];
    for (let i = 0; i < 6; i++) {
      const t = i / 5;
      const ms = (windowEnd - windowStart) * t;
      out.push({
        x: laneStart + t * laneWidth,
        label: formatOffset(ms),
      });
    }
    return out;
  }, [laneStart, laneWidth, windowEnd, windowStart]);

  const rowY = (idx: number) =>
    PADDING + TIME_AXIS_HEIGHT + idx * (ROW_HEIGHT + ROW_GAP);

  const turnBar = scaleBar(
    windowStart,
    windowEnd,
    windowStart,
    windowEnd,
    laneStart,
    laneWidth,
  );

  const phaseBars = phases.map((segment, idx) => {
    const start = parseTime(segment.started_at) ?? windowStart;
    const end = parseTime(segment.ended_at) ?? start;
    const { x, width } = scaleBar(
      start,
      Math.max(end, start + 1),
      windowStart,
      windowEnd,
      laneStart,
      laneWidth,
    );
    return {
      key: `phase-${idx}`,
      x,
      width,
      color: phaseColor(segment.name),
      label: segment.name,
    };
  });

  const toolBars = spans.filter(isToolSpan).map((span) => {
    const start = parseTime(span.start_time) ?? windowStart;
    const end = parseTime(span.end_time) ?? Math.max(start, windowEnd);
    const { x, width } = scaleBar(
      start,
      end,
      windowStart,
      windowEnd,
      laneStart,
      laneWidth,
    );
    return {
      key: span.span_id,
      x,
      width,
      color: statusColor(span.status_code),
      opacity: dimForFilter(span, highlightedFilter),
      label: span.tool_name || span.name,
    };
  });

  const subBars = spans.filter(isSubagentSpan).map((span) => {
    const start = parseTime(span.start_time) ?? windowStart;
    const end = parseTime(span.end_time) ?? Math.max(start, windowEnd);
    const { x, width } = scaleBar(
      start,
      end,
      windowStart,
      windowEnd,
      laneStart,
      laneWidth,
    );
    return {
      key: span.span_id,
      x,
      width,
      opacity: dimForFilter(span, highlightedFilter),
      label: span.agent_id || span.name,
    };
  });

  const hitlMarkers = spans.filter(isHitlSpan).map((span) => {
    const start = parseTime(span.start_time) ?? windowStart;
    const { x } = scaleBar(
      start,
      start + 1,
      windowStart,
      windowEnd,
      laneStart,
      laneWidth,
    );
    const event = hitlBySpan.get(span.span_id);
    return {
      key: span.span_id,
      x,
      opacity: dimForFilter(span, highlightedFilter),
      color: hitlMarkerColor(event),
    };
  });

  // Cluster markers within ~16px of each other so the swim lane shows a
  // numeric badge instead of overlapping triangles.
  const hitlClusters = useMemo(() => {
    if (hitlMarkers.length === 0) return [] as { x: number; count: number }[];
    const sorted = [...hitlMarkers].sort((a, b) => a.x - b.x);
    const clusters: { x: number; count: number }[] = [];
    let current = { x: sorted[0].x, count: 1 };
    for (let i = 1; i < sorted.length; i++) {
      if (sorted[i].x - current.x < 16) {
        current.count++;
      } else {
        clusters.push(current);
        current = { x: sorted[i].x, count: 1 };
      }
    }
    clusters.push(current);
    return clusters.filter((c) => c.count > 1);
  }, [hitlMarkers]);

  return (
    <svg
      role="img"
      aria-label="Turn swim lane"
      viewBox={`0 0 ${VIEWBOX_WIDTH} ${VIEWBOX_HEIGHT}`}
      preserveAspectRatio="xMinYMin meet"
      className="w-full"
      style={{ height: "auto", maxHeight: 200 }}
    >
      {/* Time axis ticks */}
      {ticks.map((tick, idx) => (
        <g key={`tick-${idx}`}>
          <line
            x1={tick.x}
            x2={tick.x}
            y1={PADDING + TIME_AXIS_HEIGHT - 4}
            y2={PADDING + TIME_AXIS_HEIGHT}
            stroke="var(--border-bright)"
            strokeWidth={1}
          />
          <text
            x={tick.x}
            y={PADDING + TIME_AXIS_HEIGHT - 6}
            fill="var(--artemis-space)"
            fontSize={9}
            textAnchor="middle"
            fontFamily="var(--font-mono, ui-monospace)"
          >
            {tick.label}
          </text>
        </g>
      ))}
      <line
        x1={laneStart}
        x2={laneStart + laneWidth}
        y1={PADDING + TIME_AXIS_HEIGHT}
        y2={PADDING + TIME_AXIS_HEIGHT}
        stroke="var(--border)"
        strokeWidth={1}
      />

      {/* Row labels */}
      {["TURN", "PHASE", "TOOLS", "SUB", "HITL"].map((label, idx) => (
        <text
          key={`label-${label}`}
          x={PADDING}
          y={rowY(idx) + ROW_HEIGHT / 2 + 3}
          fill="var(--artemis-space)"
          fontSize={9}
          fontFamily="var(--font-mono, ui-monospace)"
        >
          {label}
        </text>
      ))}

      {/* Row 0: turn bar */}
      <rect
        x={turnBar.x}
        y={rowY(0)}
        width={turnBar.width}
        height={ROW_HEIGHT}
        rx={2}
        fill="var(--accent)"
        opacity={0.35}
      />

      {/* Row 1: phase segments */}
      {phaseBars.map((bar) => (
        <rect
          key={bar.key}
          x={bar.x}
          y={rowY(1)}
          width={bar.width}
          height={ROW_HEIGHT}
          rx={2}
          fill={bar.color}
          opacity={0.7}
        >
          <title>{bar.label}</title>
        </rect>
      ))}

      {/* Row 2: tool bars (focusable for keyboard) */}
      {toolBars.map((bar) => (
        <rect
          key={bar.key}
          x={bar.x}
          y={rowY(2)}
          width={bar.width}
          height={ROW_HEIGHT}
          rx={2}
          fill={bar.color}
          opacity={bar.opacity}
          tabIndex={0}
          focusable="true"
        >
          <title>{bar.label}</title>
        </rect>
      ))}

      {/* Row 3: subagent bars */}
      {subBars.map((bar) => (
        <rect
          key={bar.key}
          x={bar.x}
          y={rowY(3)}
          width={bar.width}
          height={ROW_HEIGHT}
          rx={2}
          fill="url(#swim-accent)"
          opacity={bar.opacity}
          tabIndex={0}
          focusable="true"
        >
          <title>{bar.label}</title>
        </rect>
      ))}

      {/* Row 4: HITL triangle markers */}
      {hitlMarkers.map((marker) => {
        const top = rowY(4) + 2;
        const bottom = rowY(4) + ROW_HEIGHT - 2;
        return (
          <polygon
            key={marker.key}
            points={`${marker.x - 5},${bottom} ${marker.x + 5},${bottom} ${marker.x},${top}`}
            fill={marker.color}
            opacity={marker.opacity}
          >
            <title>HITL permission</title>
          </polygon>
        );
      })}
      {hitlClusters.map((cluster, idx) => (
        <text
          key={`hitl-cluster-${idx}`}
          x={cluster.x + 7}
          y={rowY(4) + ROW_HEIGHT - 4}
          fill="var(--text-primary)"
          fontSize={9}
          fontFamily="var(--font-mono, ui-monospace)"
        >
          ×{cluster.count}
        </text>
      ))}

      <defs>
        <linearGradient id="swim-accent" x1="0%" y1="0%" x2="100%" y2="100%">
          <stop offset="0%" stopColor="#0B3D91" />
          <stop offset="50%" stopColor="#27AAE1" />
          <stop offset="100%" stopColor="#FC3D21" />
        </linearGradient>
      </defs>
    </svg>
  );
}
