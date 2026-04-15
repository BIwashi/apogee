"use client";

import { useMemo, useRef, useState } from "react";

import type { EventBucket } from "../lib/api-types";

/**
 * LogHistogram — the stacked-bar density chart that lives above the
 * /events table. Each bar represents one bucket from the backend's
 * /v1/events/timeseries helper; the bar is coloured by severity and
 * sized by total count. Hovering shows a tooltip with the bucket time
 * and count; click-and-drag selects a time range and fires onBrush with
 * the selected [since, until] tuple so the parent can push them as URL
 * params.
 *
 * Rendering is pure SVG (no chart library) to keep the bundle small and
 * to avoid recharts' animated reflow cost — the /events page polls this
 * endpoint every few seconds while filters are active so responsiveness
 * matters more than bells.
 */

const HEIGHT = 80;
const BAR_GAP = 1;

const SEVERITY_COLORS: Record<string, string> = {
  error: "var(--status-critical)",
  warn: "var(--status-warning)",
  warning: "var(--status-warning)",
  info: "var(--status-info)",
  debug: "var(--status-muted)",
  trace: "var(--status-muted)",
};

const SEVERITY_ORDER: readonly string[] = [
  "error",
  "warn",
  "warning",
  "info",
  "debug",
  "trace",
];

function severityColor(key: string): string {
  return SEVERITY_COLORS[key] ?? "var(--status-info)";
}

interface LogHistogramProps {
  buckets: EventBucket[];
  total: number;
  loading: boolean;
  onBrush?: (since: string, until: string) => void;
}

interface BrushState {
  startX: number;
  currentX: number;
}

export default function LogHistogram({
  buckets,
  total,
  loading,
  onBrush,
}: LogHistogramProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const [hoverIdx, setHoverIdx] = useState<number | null>(null);
  const [brush, setBrush] = useState<BrushState | null>(null);

  const maxTotal = useMemo(() => {
    let m = 0;
    for (const b of buckets) if (b.total > m) m = b.total;
    return m || 1;
  }, [buckets]);

  const barWidth = useMemo(() => {
    if (buckets.length === 0) return 0;
    return `calc((100% - ${(buckets.length - 1) * BAR_GAP}px) / ${buckets.length})`;
  }, [buckets.length]);

  const formatted = useMemo(() => {
    return buckets.map((b) => {
      const d = new Date(b.bucket);
      const label = `${d.toLocaleTimeString(undefined, {
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
      })}`;
      return { ...b, label };
    });
  }, [buckets]);

  const onMouseDown = (e: React.MouseEvent<HTMLDivElement>) => {
    if (!onBrush || !containerRef.current) return;
    const rect = containerRef.current.getBoundingClientRect();
    setBrush({ startX: e.clientX - rect.left, currentX: e.clientX - rect.left });
  };
  const onMouseMove = (e: React.MouseEvent<HTMLDivElement>) => {
    if (!brush || !containerRef.current) return;
    const rect = containerRef.current.getBoundingClientRect();
    setBrush({ ...brush, currentX: e.clientX - rect.left });
  };
  const onMouseUp = () => {
    if (!brush || buckets.length === 0 || !containerRef.current) {
      setBrush(null);
      return;
    }
    const rect = containerRef.current.getBoundingClientRect();
    const width = rect.width || 1;
    const a = Math.max(0, Math.min(brush.startX, brush.currentX));
    const b = Math.min(width, Math.max(brush.startX, brush.currentX));
    if (b - a < 4) {
      // Treat as a click — no range selection.
      setBrush(null);
      return;
    }
    const iStart = Math.floor((a / width) * buckets.length);
    const iEnd = Math.min(
      buckets.length - 1,
      Math.floor((b / width) * buckets.length),
    );
    const sinceBucket = buckets[iStart];
    const untilBucket = buckets[iEnd];
    if (sinceBucket && untilBucket && onBrush) {
      onBrush(sinceBucket.bucket, untilBucket.bucket);
    }
    setBrush(null);
  };

  const brushLeft = brush
    ? Math.min(brush.startX, brush.currentX)
    : 0;
  const brushWidth = brush
    ? Math.abs(brush.currentX - brush.startX)
    : 0;

  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-end justify-between">
        <span className="font-display text-[11px] uppercase tracking-[0.14em] text-[var(--text-muted)]">
          {loading && buckets.length === 0
            ? "Loading histogram…"
            : `${total.toLocaleString()} events found`}
        </span>
        <span className="font-mono text-[10px] text-[var(--text-muted)]">
          {buckets.length > 0 &&
            `${buckets.length} buckets · click-drag to zoom`}
        </span>
      </div>
      <div
        ref={containerRef}
        role="img"
        aria-label={`Event histogram: ${total} events across ${buckets.length} buckets`}
        className="relative flex select-none items-end overflow-hidden border border-[var(--border)] bg-[var(--bg-card)]"
        style={{ height: HEIGHT }}
        onMouseDown={onMouseDown}
        onMouseMove={onMouseMove}
        onMouseUp={onMouseUp}
        onMouseLeave={() => {
          setHoverIdx(null);
          if (brush) setBrush(null);
        }}
      >
        {formatted.map((b, idx) => {
          const totalH = Math.max(1, Math.round((b.total / maxTotal) * (HEIGHT - 4)));
          return (
            <div
              key={b.bucket + idx}
              className="relative h-full"
              style={{
                width: barWidth,
                marginLeft: idx === 0 ? 0 : BAR_GAP,
              }}
              onMouseEnter={() => setHoverIdx(idx)}
            >
              <div
                className="absolute bottom-0 left-0 right-0 flex flex-col-reverse"
                style={{ height: totalH }}
                aria-hidden
              >
                {SEVERITY_ORDER.map((sev) => {
                  const count = b.by_severity?.[sev] ?? 0;
                  if (count === 0) return null;
                  const h = Math.max(
                    1,
                    Math.round((count / b.total) * totalH),
                  );
                  return (
                    <div
                      key={sev}
                      style={{
                        height: h,
                        background: severityColor(sev),
                      }}
                    />
                  );
                })}
              </div>
              {hoverIdx === idx && (
                <div className="pointer-events-none absolute left-1/2 top-0 z-10 -translate-x-1/2 whitespace-nowrap rounded border border-[var(--border)] bg-[var(--bg-deepspace)] px-2 py-1 font-mono text-[10px] text-[var(--artemis-white)] shadow">
                  {b.label}: {b.total.toLocaleString()}
                </div>
              )}
            </div>
          );
        })}
        {brush && (
          <div
            aria-hidden
            className="pointer-events-none absolute top-0 bottom-0 border-x border-[var(--status-info)] bg-[var(--status-info)]/10"
            style={{ left: brushLeft, width: brushWidth }}
          />
        )}
        {formatted.length === 0 && !loading && (
          <span className="absolute inset-0 flex items-center justify-center font-mono text-[11px] text-[var(--text-muted)]">
            No events in this range
          </span>
        )}
      </div>
    </div>
  );
}
