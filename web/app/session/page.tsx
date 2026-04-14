"use client";

import { useCallback, useEffect, useMemo } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { Activity, BarChart3, Layers, List, ScrollText } from "lucide-react";

import Breadcrumb from "../components/Breadcrumb";
import Card from "../components/Card";
import KpiStrip from "../components/KpiStrip";
import RawLogsPanel from "../components/RawLogsPanel";
import RecentTurnsTable from "../components/RecentTurnsTable";
import RollupPanel from "../components/RollupPanel";
import SectionHeader from "../components/SectionHeader";
import SpanTree from "../components/SpanTree";
import SwimLane from "../components/SwimLane";
import Tabs, { type TabItem } from "../components/Tabs";
import type {
  SessionLogsResponse,
  SessionSummary,
  SessionTurnsResponse,
  TurnSpansResponse,
  Turn,
} from "../lib/api-types";
import { useApi } from "../lib/swr";
import { formatClock, timeAgo } from "../lib/time";
import { useSelection } from "../lib/url-state";

/**
 * `/session?id=<id>` — tabbed session detail page. The five tabs mirror
 * Datadog APM's service detail: Overview / Turns / Trace / Logs / Metrics.
 * The active tab is stored in the `tab` URL param so deep links land on the
 * right surface.
 *
 * This route is a flat query-string page, not a dynamic segment. That lets
 * `next.config.ts` use `output: "export"` (which forbids dynamic params that
 * cannot be enumerated at build time) without losing the deep-linkable
 * session view. The session id arrives via `useSearchParams()`; every piece
 * of data is fetched client-side exactly like the old dynamic route.
 */

type TabKey = "overview" | "turns" | "trace" | "logs" | "metrics";

const TABS: TabItem<TabKey>[] = [
  { key: "overview", label: "Overview", icon: Layers },
  { key: "turns", label: "Turns", icon: List },
  { key: "trace", label: "Trace", icon: Activity },
  { key: "logs", label: "Logs", icon: ScrollText },
  { key: "metrics", label: "Metrics", icon: BarChart3 },
];

function shortId(id: string, len = 8): string {
  if (!id) return "—";
  return id.length <= len ? id : id.slice(0, len);
}

function sortTurns(turns: Turn[]): Turn[] {
  return [...turns].sort((a, b) => b.started_at.localeCompare(a.started_at));
}

export default function SessionDetailPage() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const { setSelection } = useSelection();

  const id = searchParams.get("id") ?? "";

  // Ensure the ribbon's selection reflects the session the user is viewing.
  useEffect(() => {
    if (id) setSelection({ sess: id });
  }, [id, setSelection]);

  const rawTab = searchParams.get("tab") as TabKey | null;
  const active: TabKey = rawTab && TABS.some((t) => t.key === rawTab) ? rawTab : "overview";

  const setActive = useCallback(
    (key: TabKey) => {
      const next = new URLSearchParams(searchParams.toString());
      next.set("tab", key);
      router.replace(`?${next.toString()}`, { scroll: false });
    },
    [router, searchParams],
  );

  const { data: summary } = useApi<SessionSummary>(
    id ? `/v1/sessions/${id}/summary` : null,
    { refreshInterval: 5_000 },
  );
  const { data: turnsData } = useApi<SessionTurnsResponse>(
    id ? `/v1/sessions/${id}/turns` : null,
    { refreshInterval: 3_000 },
  );
  const { data: logsData } = useApi<SessionLogsResponse>(
    id ? `/v1/sessions/${id}/logs?limit=200` : null,
    { refreshInterval: 5_000 },
  );

  const turns = useMemo(() => sortTurns(turnsData?.turns ?? []), [turnsData]);
  const logs = logsData?.logs ?? [];
  const latestTurnId = summary?.latest_turn_id ?? turns[0]?.turn_id ?? null;

  const { data: traceData } = useApi<TurnSpansResponse>(
    latestTurnId && active === "trace" ? `/v1/turns/${latestTurnId}/spans` : null,
  );
  const traceSpans = traceData?.spans ?? [];
  const tracePhases = traceData?.phases ?? [];
  const latestTurn = latestTurnId
    ? turns.find((t) => t.turn_id === latestTurnId) ?? null
    : null;

  const headline = summary?.latest_headline || "";
  const onCopyPrompt = useCallback(() => {
    const prompt = turns[0]?.prompt_text;
    if (!prompt || typeof navigator === "undefined" || !navigator.clipboard) return;
    void navigator.clipboard.writeText(prompt);
  }, [turns]);

  if (!id) {
    return (
      <div className="mx-auto flex max-w-6xl flex-col gap-6 pt-6">
        <Breadcrumb segments={[{ label: "Sessions", href: "/sessions" }, { label: "(missing id)" }]} />
        <Card>
          <p className="px-4 py-10 text-center text-[12px] text-[var(--text-muted)]">
            No session id supplied. Use the command palette (⌘K) to pick a session.
          </p>
        </Card>
      </div>
    );
  }

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <header className="flex flex-col gap-3 pt-6">
        <Breadcrumb
          segments={[
            { label: "Sessions", href: "/sessions" },
            {
              label: `${shortId(id)}${headline ? ` · ${headline}` : ""}`,
            },
          ]}
        />
        <div className="flex flex-wrap items-end justify-between gap-4">
          <div>
            <p className="font-display text-[10px] uppercase tracking-[0.2em] text-[var(--artemis-space)]">
              Session
            </p>
            <button
              type="button"
              onClick={onCopyPrompt}
              title="Copy latest prompt"
              className="text-left font-display text-3xl tracking-[0.14em] text-white hover:text-[var(--accent)]"
            >
              {shortId(id)}
            </button>
            <div className="accent-gradient-bar mt-3 h-[3px] w-32 rounded-full" />
            {headline && (
              <p className="mt-3 max-w-2xl text-[13px] text-[var(--text-muted)]">
                {headline}
              </p>
            )}
          </div>
          {summary && (
            <div className="flex flex-col items-end gap-1 font-mono text-[11px] text-[var(--text-muted)]">
              <span>{summary.source_app || "—"}</span>
              <span>started {formatClock(summary.started_at)}</span>
              <span>
                last seen {timeAgo(summary.last_seen_at)} · {summary.turn_count} turns
              </span>
            </div>
          )}
        </div>
      </header>

      <Tabs items={TABS} active={active} onSelect={setActive} />

      {active === "overview" && (
        <OverviewTab id={id} summary={summary ?? null} turns={turns} />
      )}
      {active === "turns" && <TurnsTab turns={turns} />}
      {active === "trace" && (
        <TraceTab
          latestTurn={latestTurn}
          spans={traceSpans}
          phases={tracePhases}
          sessionId={id}
          latestTurnId={latestTurnId}
        />
      )}
      {active === "logs" && <LogsTab logs={logs} />}
      {active === "metrics" && <MetricsTab id={id} />}

      <footer className="pb-8 pt-2">
        <p className="font-mono text-[10px] text-[var(--text-muted)]">
          apogee 0.0.0-dev — session detail
        </p>
      </footer>
    </div>
  );
}

function OverviewTab({
  id,
  summary,
  turns,
}: {
  id: string;
  summary: SessionSummary | null;
  turns: Turn[];
}) {
  return (
    <div className="flex flex-col gap-6">
      <section>
        <SectionHeader title="Summary" subtitle="Rolled up from every turn in the session." />
        <Card className="flex flex-wrap items-center gap-4 px-4 py-3 text-[12px]">
          <SummaryStat label="TURNS" value={summary?.turn_count ?? turns.length} />
          <SummaryStat label="RUNNING" value={summary?.running_count ?? 0} tone="info" />
          <SummaryStat label="COMPLETED" value={summary?.completed_count ?? 0} tone="success" />
          <SummaryStat label="ERRORED" value={summary?.errored_count ?? 0} tone="critical" />
          <SummaryStat label="MODEL" value={summary?.model || "—"} />
          <SummaryStat label="SOURCE" value={summary?.source_app || "—"} />
        </Card>
      </section>
      <section>
        <RollupPanel sessionId={id} />
      </section>
      <section>
        <SectionHeader title="Fleet KPIs (scoped)" subtitle="Sparklines scoped to this session." />
        <KpiStrip sessionId={id} />
      </section>
      <section>
        <SectionHeader title="Latest turns" subtitle="Click a row to drill into the swim lane." />
        <Card className="p-0">
          <RecentTurnsTable turns={turns.slice(0, 20)} />
        </Card>
      </section>
    </div>
  );
}

function SummaryStat({
  label,
  value,
  tone,
}: {
  label: string;
  value: number | string;
  tone?: "info" | "success" | "critical";
}) {
  const color =
    tone === "info"
      ? "text-[var(--status-info)]"
      : tone === "success"
        ? "text-[var(--status-success)]"
        : tone === "critical"
          ? "text-[var(--status-critical)]"
          : "text-white";
  return (
    <div className="flex flex-col gap-0.5">
      <span className="font-display text-[10px] uppercase tracking-[0.14em] text-[var(--artemis-space)]">
        {label}
      </span>
      <span className={`font-mono text-[14px] tabular-nums ${color}`}>{value}</span>
    </div>
  );
}

function TurnsTab({ turns }: { turns: Turn[] }) {
  return (
    <section>
      <SectionHeader title="All turns" subtitle="Attention-sorted. Click a row to drill in." />
      <Card className="p-0">
        <RecentTurnsTable turns={turns} />
      </Card>
    </section>
  );
}

function TraceTab({
  latestTurn,
  spans,
  phases,
  sessionId,
  latestTurnId,
}: {
  latestTurn: Turn | null;
  spans: TurnSpansResponse["spans"];
  phases: TurnSpansResponse["phases"];
  sessionId: string;
  latestTurnId: string | null;
}) {
  if (!latestTurn || !latestTurnId) {
    return (
      <Card>
        <p className="px-4 py-10 text-center text-[12px] text-[var(--text-muted)]">
          No turn to display. Start a Claude Code session to populate this view.
        </p>
      </Card>
    );
  }
  return (
    <div className="flex flex-col gap-4">
      <section>
        <SectionHeader
          title={`Swim lane — turn ${latestTurnId.slice(0, 8)}`}
          subtitle={
            <span>
              Showing the latest turn.{" "}
              <a
                href={`/turn/?sess=${sessionId}&turn=${latestTurnId}`}
                className="underline hover:text-[var(--accent)]"
              >
                Open full turn detail →
              </a>
            </span>
          }
        />
        <Card>
          <SwimLane turn={latestTurn} spans={spans} phases={phases} highlightedFilter="all" />
        </Card>
      </section>
      <section>
        <SectionHeader title="Span tree" subtitle="Read-only snapshot scoped to the latest turn." />
        <Card className="p-2">
          <SpanTree spans={spans} selectedSpanId={null} onSelect={() => {}} filter="all" />
        </Card>
      </section>
    </div>
  );
}

function LogsTab({ logs }: { logs: SessionLogsResponse["logs"] }) {
  return (
    <section>
      <RawLogsPanel logs={logs} title="Raw logs (last 200)" />
    </section>
  );
}

function MetricsTab({ id }: { id: string }) {
  return (
    <div className="flex flex-col gap-4">
      <section>
        <SectionHeader
          title="Scoped KPIs"
          subtitle="Sparklines filtered to this session's labeled metric rows."
        />
        <KpiStrip sessionId={id} />
      </section>
    </div>
  );
}
