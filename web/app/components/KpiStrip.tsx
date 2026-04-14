"use client";

import type { MetricSeries } from "../lib/api-types";
import { useApi } from "../lib/swr";
import KpiSparkline from "./KpiSparkline";

/**
 * KpiStrip — four sparklines in a row, each fetching its own metric series
 * from the collector. Refresh is 5 s, the default apogee tick cadence; SWR
 * shares the request across the tiles if they ever reuse the same key.
 */

interface KpiConfig {
  metric: string;
  label: string;
  kind: "gauge" | "counter";
  format?: (v: number) => string;
}

const CONFIG: KpiConfig[] = [
  { metric: "apogee.turns.active", label: "ACTIVE TURNS", kind: "gauge" },
  { metric: "apogee.tools.rate", label: "TOOLS / TICK", kind: "counter" },
  { metric: "apogee.errors.rate", label: "ERRORS / TICK", kind: "counter" },
  { metric: "apogee.hitl.pending", label: "HITL PENDING", kind: "gauge" },
];

function useSeries(
  metric: string,
  kind: "gauge" | "counter",
  scope: { sessionId?: string | null; sourceApp?: string | null },
) {
  const params = new URLSearchParams();
  params.set("name", metric);
  params.set("window", "5m");
  params.set("step", "10s");
  params.set("kind", kind);
  if (scope.sessionId) params.set("session_id", scope.sessionId);
  if (scope.sourceApp) params.set("source_app", scope.sourceApp);
  const { data } = useApi<MetricSeries>(`/v1/metrics/series?${params.toString()}`, {
    refreshInterval: 5_000,
  });
  return data?.points ?? [];
}

interface KpiStripProps {
  sessionId?: string | null;
  sourceApp?: string | null;
}

export default function KpiStrip({ sessionId, sourceApp }: KpiStripProps = {}) {
  return (
    <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
      {CONFIG.map((cfg) => (
        <KpiTile
          key={cfg.metric}
          config={cfg}
          sessionId={sessionId}
          sourceApp={sourceApp}
        />
      ))}
    </div>
  );
}

interface KpiTileProps {
  config: KpiConfig;
  sessionId?: string | null;
  sourceApp?: string | null;
}

function KpiTile({ config, sessionId, sourceApp }: KpiTileProps) {
  const points = useSeries(config.metric, config.kind, { sessionId, sourceApp });
  return (
    <KpiSparkline
      points={points}
      label={config.label}
      format={config.format}
    />
  );
}
