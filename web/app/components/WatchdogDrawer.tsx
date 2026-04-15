"use client";

import { useMemo, useState } from "react";
import { AlertOctagon, AlertTriangle, Info, Radar } from "lucide-react";
import {
  Area,
  AreaChart,
  ReferenceLine,
  ResponsiveContainer,
  YAxis,
} from "recharts";

import { apiUrl } from "../lib/api";
import type {
  WatchdogAckResponse,
  WatchdogSeverity,
  WatchdogSignal,
} from "../lib/api-types";
import { timeAgo } from "../lib/time";
import SideDrawer from "./SideDrawer";

/**
 * WatchdogDrawer — Datadog-style anomaly drawer rendered on top of the
 * SideDrawer primitive. The component is a controlled overlay: the
 * parent (`WatchdogBell`) owns the open / signals / mutate triple and
 * passes them in.
 *
 * The drawer renders:
 *   - A header with a Radar icon and the "Watchdog" title.
 *   - Filter chips at the top: "Unacked / All / Critical / Warning".
 *   - A list of signal cards, newest first. Each card carries a
 *     severity icon, a relative timestamp, the headline, the metric
 *     name, the labels (rendered as key-value chips), a tiny sparkline
 *     of the window samples with a baseline mean reference line, and
 *     an "Acknowledge" button that POSTs to the collector.
 *   - An empty state when the filter resolves to zero rows.
 *   - A footer link pointing at the events history filtered to the
 *     watchdog.signal type. The route is not implemented yet but the
 *     link is harmless because the router falls back to /events.
 */

interface WatchdogDrawerProps {
  open: boolean;
  onClose: () => void;
  signals: WatchdogSignal[];
  onAcknowledged: (next: WatchdogSignal) => void;
}

type FilterKey = "unacked" | "all" | "critical" | "warning";

const SEVERITY_ICONS: Record<WatchdogSeverity, typeof Info> = {
  info: Info,
  warning: AlertTriangle,
  critical: AlertOctagon,
};

const SEVERITY_VAR: Record<WatchdogSeverity, string> = {
  info: "var(--status-info)",
  warning: "var(--status-warning)",
  critical: "var(--status-critical)",
};

export default function WatchdogDrawer({
  open,
  onClose,
  signals,
  onAcknowledged,
}: WatchdogDrawerProps) {
  const [filter, setFilter] = useState<FilterKey>("unacked");

  const counts = useMemo(() => {
    let unacked = 0;
    let warning = 0;
    let critical = 0;
    for (const s of signals) {
      if (!s.acknowledged) unacked += 1;
      if (s.severity === "warning") warning += 1;
      if (s.severity === "critical") critical += 1;
    }
    return { unacked, warning, critical, all: signals.length };
  }, [signals]);

  const visible = useMemo(() => {
    switch (filter) {
      case "unacked":
        return signals.filter((s) => !s.acknowledged);
      case "critical":
        return signals.filter((s) => s.severity === "critical");
      case "warning":
        return signals.filter((s) => s.severity === "warning");
      default:
        return signals;
    }
  }, [signals, filter]);

  return (
    <SideDrawer open={open} onClose={onClose} title="Watchdog" width="md">
      <div className="flex h-full flex-col gap-3">
        <header className="flex items-center gap-2 text-[var(--artemis-white)]">
          <Radar size={16} strokeWidth={1.5} />
          <span className="font-display text-[12px] uppercase tracking-[0.16em]">
            Anomalies
          </span>
        </header>

        <div className="flex flex-wrap items-center gap-1">
          <FilterChip
            label={`Unacked (${counts.unacked})`}
            active={filter === "unacked"}
            onClick={() => setFilter("unacked")}
          />
          <FilterChip
            label={`All (${counts.all})`}
            active={filter === "all"}
            onClick={() => setFilter("all")}
          />
          <FilterChip
            label={`Critical (${counts.critical})`}
            active={filter === "critical"}
            onClick={() => setFilter("critical")}
          />
          <FilterChip
            label={`Warning (${counts.warning})`}
            active={filter === "warning"}
            onClick={() => setFilter("warning")}
          />
        </div>

        <div className="flex flex-1 flex-col gap-2 overflow-y-auto pr-1">
          {visible.length === 0 ? (
            <p className="rounded border border-dashed border-[var(--border)] px-3 py-6 text-center font-mono text-[12px] text-[var(--text-muted)]">
              No anomalies in the last hour.
            </p>
          ) : (
            visible.map((sig) => (
              <SignalCard
                key={sig.id}
                signal={sig}
                onAcknowledged={onAcknowledged}
              />
            ))
          )}
        </div>

        <footer className="border-t border-[var(--border)] pt-2 text-right">
          <a
            href="/events?type=watchdog.signal"
            className="font-mono text-[11px] text-[var(--artemis-space)] hover:text-[var(--artemis-white)]"
          >
            View all history →
          </a>
        </footer>
      </div>
    </SideDrawer>
  );
}

interface FilterChipProps {
  label: string;
  active: boolean;
  onClick: () => void;
}

function FilterChip({ label, active, onClick }: FilterChipProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`rounded-full border px-2 py-0.5 font-mono text-[11px] transition-colors ${
        active
          ? "border-[var(--border-bright)] bg-[var(--bg-overlay)] text-[var(--artemis-white)]"
          : "border-[var(--border)] bg-[var(--bg-raised)] text-[var(--artemis-space)] hover:bg-[var(--bg-overlay)] hover:text-[var(--artemis-white)]"
      }`}
    >
      {label}
    </button>
  );
}

interface SignalCardProps {
  signal: WatchdogSignal;
  onAcknowledged: (next: WatchdogSignal) => void;
}

function SignalCard({ signal, onAcknowledged }: SignalCardProps) {
  const Icon = SEVERITY_ICONS[signal.severity] ?? Info;
  const tint = SEVERITY_VAR[signal.severity] ?? "var(--status-info)";
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const points = signal.evidence?.window ?? [];
  const data = points.map((p) => ({
    at: p.at,
    value: p.value,
  }));
  const labels = Object.entries(signal.labels ?? {});

  const ack = async () => {
    if (busy || signal.acknowledged) return;
    setBusy(true);
    setError(null);
    try {
      const resp = await fetch(apiUrl(`/v1/watchdog/signals/${signal.id}/ack`), {
        method: "POST",
      });
      if (!resp.ok) {
        throw new Error(`HTTP ${resp.status}`);
      }
      const body = (await resp.json()) as WatchdogAckResponse;
      onAcknowledged(body.signal);
    } catch (e) {
      setError(e instanceof Error ? e.message : "ack failed");
    } finally {
      setBusy(false);
    }
  };

  return (
    <article
      className="surface-card-raised flex flex-col gap-2 p-3"
      style={{ borderLeft: `2px solid ${tint}` }}
    >
      <header className="flex items-start justify-between gap-2">
        <div className="flex items-center gap-2">
          <Icon size={14} strokeWidth={1.5} style={{ color: tint }} />
          <span className="font-mono text-[11px] uppercase tracking-[0.08em]" style={{ color: tint }}>
            {signal.severity}
          </span>
        </div>
        <span className="font-mono text-[11px] text-[var(--text-muted)]">
          {timeAgo(signal.detected_at)}
        </span>
      </header>
      <p className="text-[13px] text-[var(--artemis-white)]">{signal.headline}</p>
      <div className="flex flex-wrap items-center gap-1 font-mono text-[10px] text-[var(--text-muted)]">
        <span className="rounded bg-[var(--bg-overlay)] px-1.5 py-0.5 text-[var(--artemis-space)]">
          {signal.metric_name}
        </span>
        {labels.map(([k, v]) => (
          <span
            key={`${k}=${v}`}
            className="rounded bg-[var(--bg-overlay)] px-1.5 py-0.5 text-[var(--artemis-space)]"
          >
            {k}: {v}
          </span>
        ))}
        <span className="ml-auto">
          z = {signal.z_score.toFixed(2)}
        </span>
      </div>
      {data.length > 1 && (
        <div style={{ height: 40, width: "100%" }}>
          <ResponsiveContainer width="100%" height={40} minWidth={0}>
            <AreaChart data={data} margin={{ top: 4, right: 0, left: 0, bottom: 0 }}>
              <defs>
                <linearGradient id={`watchdogGrad-${signal.id}`} x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor={tint} stopOpacity={0.45} />
                  <stop offset="100%" stopColor={tint} stopOpacity={0} />
                </linearGradient>
              </defs>
              <YAxis hide domain={["auto", "auto"]} />
              <ReferenceLine
                y={signal.baseline_mean}
                stroke="var(--text-muted)"
                strokeDasharray="2 2"
                strokeWidth={1}
                ifOverflow="extendDomain"
              />
              <Area
                type="monotone"
                dataKey="value"
                stroke={tint}
                strokeWidth={1.5}
                fill={`url(#watchdogGrad-${signal.id})`}
                isAnimationActive={false}
              />
            </AreaChart>
          </ResponsiveContainer>
        </div>
      )}
      <div className="flex items-center justify-between gap-2">
        <span className="font-mono text-[10px] text-[var(--text-muted)]">
          baseline {signal.baseline_mean.toFixed(2)} ± {signal.baseline_stddev.toFixed(2)}
        </span>
        {signal.acknowledged ? (
          <span className="font-mono text-[10px] text-[var(--text-muted)]">acknowledged</span>
        ) : (
          <button
            type="button"
            onClick={ack}
            disabled={busy}
            className="rounded border border-[var(--border)] bg-[var(--bg-raised)] px-2 py-1 font-mono text-[11px] text-[var(--artemis-white)] hover:bg-[var(--bg-overlay)] disabled:opacity-50"
          >
            {busy ? "Acknowledging…" : "Acknowledge"}
          </button>
        )}
      </div>
      {error && (
        <p className="font-mono text-[10px] text-[var(--status-warning)]">{error}</p>
      )}
    </article>
  );
}
