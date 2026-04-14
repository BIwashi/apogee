"use client";

import { Area, AreaChart, ResponsiveContainer, YAxis } from "recharts";

import type { MetricSeriesPoint } from "../lib/api-types";

/**
 * KpiSparkline — a tiny area chart rendered with recharts. Axes are hidden,
 * colors are pulled from the --status-info CSS variable, and the component
 * overlays a big numeric label plus a short text label so the whole tile is
 * legible without a tooltip.
 */

interface KpiSparklineProps {
  points: MetricSeriesPoint[];
  label: string;
  format?: (v: number) => string;
}

function defaultFormat(v: number): string {
  if (Number.isNaN(v)) return "—";
  if (v >= 1_000_000) return `${(v / 1_000_000).toFixed(1)}M`;
  if (v >= 1_000) return `${(v / 1_000).toFixed(1)}k`;
  if (Number.isInteger(v)) return String(v);
  return v.toFixed(1);
}

export default function KpiSparkline({
  points,
  label,
  format = defaultFormat,
}: KpiSparklineProps) {
  const latest = points.length > 0 ? points[points.length - 1]!.value : 0;
  const data = points.map((p) => ({ at: p.at, value: p.value }));

  return (
    <div className="surface-card-raised flex flex-col gap-2 p-4">
      <div className="flex items-baseline justify-between">
        <span className="font-display text-[10px] tracking-[0.14em] text-[var(--text-muted)]">
          {label}
        </span>
        <span className="font-mono text-[18px] tabular-nums text-white">
          {format(latest)}
        </span>
      </div>
      <div style={{ height: 48, width: "100%" }}>
        <ResponsiveContainer width="100%" height={48} minWidth={0}>
          <AreaChart data={data} margin={{ top: 4, right: 0, left: 0, bottom: 0 }}>
            <defs>
              <linearGradient id="kpiGrad" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor="var(--status-info)" stopOpacity={0.5} />
                <stop offset="100%" stopColor="var(--status-info)" stopOpacity={0} />
              </linearGradient>
            </defs>
            <YAxis hide domain={[0, "auto"]} />
            <Area
              type="monotone"
              dataKey="value"
              stroke="var(--status-info)"
              strokeWidth={1.5}
              fill="url(#kpiGrad)"
              isAnimationActive={false}
            />
          </AreaChart>
        </ResponsiveContainer>
      </div>
    </div>
  );
}
