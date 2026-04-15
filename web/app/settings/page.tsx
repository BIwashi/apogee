"use client";

import { CheckCircle2, Circle, Copy } from "lucide-react";

import Card from "../components/Card";
import SectionHeader from "../components/SectionHeader";
import type { ApogeeInfo, TelemetryStatus } from "../lib/api-types";
import { useApi } from "../lib/swr";

/**
 * `/settings` — read-only collector info. Reads `/v1/info` for build
 * metadata and `/v1/telemetry/status` for the OTel exporter snapshot.
 * Daemon and hook install flows are CLI-only for now; the page points
 * the operator at the relevant commands instead of bundling install
 * UI here.
 */

function humanUptime(seconds: number): string {
  if (!seconds || seconds < 0) return "—";
  if (seconds < 60) return `${Math.floor(seconds)}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  const remMin = minutes % 60;
  if (hours < 24) return remMin ? `${hours}h${remMin}m` : `${hours}h`;
  const days = Math.floor(hours / 24);
  const remHour = hours % 24;
  return remHour ? `${days}d${remHour}h` : `${days}d`;
}

interface KVProps {
  label: string;
  value: React.ReactNode;
  mono?: boolean;
}

function KV({ label, value, mono }: KVProps) {
  return (
    <div className="grid grid-cols-[160px_1fr] items-start gap-3 border-b border-[var(--border)] py-2 text-[12px] last:border-b-0">
      <span className="font-display text-[10px] tracking-[0.14em] text-[var(--text-muted)]">
        {label}
      </span>
      <span className={mono ? "font-mono text-[11px] text-white" : "text-white"}>
        {value}
      </span>
    </div>
  );
}

export default function SettingsPage() {
  const { data: info } = useApi<ApogeeInfo>("/v1/info", {
    refreshInterval: 10_000,
  });
  const { data: telemetry } = useApi<TelemetryStatus>(
    "/v1/telemetry/status",
    { refreshInterval: 10_000 },
  );

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <header className="flex flex-wrap items-end justify-between gap-4 pt-6">
        <div>
          <h1 className="font-display text-3xl tracking-[0.16em] text-white">
            SETTINGS
          </h1>
          <div className="accent-gradient-bar mt-3 h-[3px] w-32 rounded-full" />
          <p className="mt-3 max-w-xl text-[13px] text-[var(--text-muted)]">
            Collector and exporter status. Read-only for now — install flows
            (daemon, hooks) are driven from the <code>apogee</code> CLI.
          </p>
        </div>
      </header>

      <section>
        <SectionHeader
          title="Collector"
          subtitle="Build + runtime metadata from /v1/info."
        />
        <Card>
          <KV
            label="Name"
            value={info?.name ?? "apogee"}
            mono
          />
          <KV
            label="Version"
            value={info?.version ?? "—"}
            mono
          />
          <KV
            label="Commit"
            value={info?.commit ?? "—"}
            mono
          />
          <KV
            label="Build date"
            value={info?.build_date ?? "—"}
            mono
          />
          <KV
            label="HTTP address"
            value={info?.collector_addr ?? "—"}
            mono
          />
          <KV
            label="Uptime"
            value={humanUptime(info?.uptime_seconds ?? 0)}
            mono
          />
        </Card>
      </section>

      <section>
        <SectionHeader
          title="OTel exporter"
          subtitle="Live snapshot from /v1/telemetry/status."
        />
        <Card>
          <KV
            label="Enabled"
            value={
              <span className="inline-flex items-center gap-2">
                {telemetry?.enabled ? (
                  <CheckCircle2
                    size={14}
                    strokeWidth={1.5}
                    className="text-[var(--status-success)]"
                  />
                ) : (
                  <Circle
                    size={14}
                    strokeWidth={1.5}
                    className="text-[var(--status-muted)]"
                  />
                )}
                <span>{telemetry?.enabled ? "enabled" : "disabled"}</span>
              </span>
            }
          />
          <KV
            label="Endpoint"
            value={telemetry?.endpoint || "—"}
            mono
          />
          <KV
            label="Protocol"
            value={telemetry?.protocol || "—"}
            mono
          />
          <KV
            label="Service name"
            value={telemetry?.service_name || "—"}
            mono
          />
          <KV
            label="Sample ratio"
            value={
              telemetry?.sample_ratio !== undefined
                ? telemetry.sample_ratio.toFixed(3)
                : "—"
            }
            mono
          />
          <KV
            label="Spans exported"
            value={String(telemetry?.spans_exported_total ?? 0)}
            mono
          />
        </Card>
      </section>

      <section>
        <SectionHeader
          title="Config file"
          subtitle="Read by the collector on startup."
        />
        <Card>
          <KV
            label="Path"
            value={
              <span className="inline-flex items-center gap-2">
                <code className="text-white">~/.apogee/config.toml</code>
                <Copy
                  size={12}
                  strokeWidth={1.5}
                  className="text-[var(--text-muted)]"
                />
              </span>
            }
          />
        </Card>
      </section>

      <section>
        <SectionHeader
          title="Daemon"
          subtitle="Background collector installation (CLI only for now)."
        />
        <Card>
          <p className="text-[12px] text-[var(--text-muted)]">
            Install the daemon with{" "}
            <code className="font-mono text-white">apogee daemon install</code>.
            A dashboard installer UI will land in a follow-up.
          </p>
        </Card>
      </section>

      <section>
        <SectionHeader
          title="Hooks"
          subtitle="Claude Code hook wiring (CLI only for now)."
        />
        <Card>
          <p className="text-[12px] text-[var(--text-muted)]">
            Install hooks with{" "}
            <code className="font-mono text-white">apogee init</code>. The
            init command writes <code>~/.claude/settings.json</code> so every
            hook event posts to this collector.
          </p>
        </Card>
      </section>
    </div>
  );
}
