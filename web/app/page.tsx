"use client";

import Link from "next/link";
import { useCallback, useMemo, useState } from "react";
import { X } from "lucide-react";

import Card from "./components/Card";
import CountPills from "./components/CountPills";
import EventTicker from "./components/EventTicker";
import KpiStrip from "./components/KpiStrip";
import RecentTurnsTable from "./components/RecentTurnsTable";
import SectionHeader from "./components/SectionHeader";
import type {
  ApogeeEvent,
  AttentionCounts,
  AttentionState,
  InitialPayload,
  RecentTurnsResponse,
  SessionSummary,
  Turn,
  TurnPayload,
} from "./lib/api-types";
import { SSE_EVENT_TYPES } from "./lib/api-types";
import { useEventStream } from "./lib/sse";
import { useApi } from "./lib/swr";
import { formatClock, timeAgo } from "./lib/time";
import { buildQuery, useSelection } from "./lib/url-state";

/**
 * `/` — the apogee live triage dashboard. In fleet mode it hydrates from
 * `GET /v1/turns/active` and patches from SSE. When a session is selected
 * via the TopRibbon / command palette, every SWR hook picks up the
 * scope params (session_id / source_app / since / until) from the URL so
 * the dashboard rescopes itself in place.
 */

const RECENT_TURNS_LIMIT = 100;
const TICKER_HISTORY = 40;

function attentionPriority(turn: Turn): number {
  switch (turn.attention_state) {
    case "intervene_now":
      return 0;
    case "watch":
      return 1;
    case "watchlist":
      return 2;
    case "healthy":
    default:
      return 3;
  }
}

function sortTurns(turns: Turn[]): Turn[] {
  return [...turns].sort((a, b) => {
    const pa = attentionPriority(a);
    const pb = attentionPriority(b);
    if (pa !== pb) return pa - pb;
    return b.started_at.localeCompare(a.started_at);
  });
}

function shortId(id: string, len = 8): string {
  if (!id) return "—";
  return id.length <= len ? id : id.slice(0, len);
}

export default function Page() {
  const { selection, apiParams, clear } = useSelection();
  const scopedQuery = buildQuery(apiParams);
  const scopedQueryWithEnded = buildQuery(apiParams, { include: "ended" });

  const { data: activeTurnsData } = useApi<RecentTurnsResponse>(
    `/v1/turns/active${scopedQuery}`,
  );
  const { data: countsData } = useApi<AttentionCounts>(
    `/v1/attention/counts${scopedQueryWithEnded}`,
    { refreshInterval: 2_000 },
  );
  const summaryPath = selection.sess
    ? `/v1/sessions/${selection.sess}/summary`
    : null;
  const { data: summary } = useApi<SessionSummary>(summaryPath, {
    refreshInterval: 5_000,
  });

  // SSE-derived state. In scoped mode we pass session_id on the stream so
  // the server fans out only matching events.
  const [initialTurns, setInitialTurns] = useState<Turn[] | null>(null);
  const [patches, setPatches] = useState<Turn[]>([]);
  const [events, setEvents] = useState<ApogeeEvent[]>([]);
  const [filter, setFilter] = useState<AttentionState | null>(null);

  const onEvent = useCallback((event: ApogeeEvent) => {
    setEvents((prev) => {
      const next = [event, ...prev];
      if (next.length > TICKER_HISTORY) next.length = TICKER_HISTORY;
      return next;
    });

    switch (event.type) {
      case SSE_EVENT_TYPES.Initial: {
        const payload = event.data as InitialPayload;
        if (payload?.recent_turns?.length) {
          setInitialTurns(payload.recent_turns.slice(0, RECENT_TURNS_LIMIT));
        } else {
          setInitialTurns([]);
        }
        break;
      }
      case SSE_EVENT_TYPES.TurnStarted:
      case SSE_EVENT_TYPES.TurnUpdated:
      case SSE_EVENT_TYPES.TurnEnded: {
        const payload = event.data as TurnPayload;
        if (payload?.turn) {
          setPatches((prev) => {
            const next = [...prev, payload.turn];
            if (next.length > RECENT_TURNS_LIMIT * 4) {
              return next.slice(next.length - RECENT_TURNS_LIMIT * 4);
            }
            return next;
          });
        }
        break;
      }
      default:
        break;
    }
  }, []);

  const streamPath = `/v1/events/stream${scopedQuery}`;
  useEventStream<ApogeeEvent>(streamPath, {
    historyLimit: TICKER_HISTORY,
    onEvent,
  });

  const turns = useMemo(() => {
    const base = initialTurns ?? activeTurnsData?.turns ?? [];
    const byId = new Map<string, Turn>();
    for (const turn of base) {
      byId.set(turn.turn_id, turn);
    }
    for (const turn of patches) {
      byId.set(turn.turn_id, turn);
    }
    // When scoped, drop any patches that leaked from non-matching sessions.
    let merged = Array.from(byId.values());
    if (selection.sess) {
      merged = merged.filter((t) => t.session_id === selection.sess);
    }
    return sortTurns(merged).slice(0, RECENT_TURNS_LIMIT);
  }, [initialTurns, activeTurnsData, patches, selection.sess]);

  const filteredTurns = useMemo(() => {
    if (!filter) return turns;
    return turns.filter((t) => {
      const state = t.attention_state ?? "healthy";
      return state === filter;
    });
  }, [turns, filter]);

  const runningCount = useMemo(
    () => turns.filter((t) => t.status === "running").length,
    [turns],
  );

  const isScoped = !!selection.sess;

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <header className="flex flex-wrap items-end justify-between gap-4 pt-6">
        <div>
          {isScoped ? (
            <>
              <p className="font-display text-[11px] uppercase tracking-[0.2em] text-[var(--artemis-space)]">
                Session
              </p>
              <h1 className="font-display text-3xl leading-none tracking-[0.14em] text-white md:text-4xl">
                {shortId(selection.sess ?? "")}
              </h1>
              <div className="accent-gradient-bar mt-3 h-[3px] w-32 rounded-full" />
              <p className="mt-3 max-w-xl text-[13px] text-[var(--text-muted)]">
                {summary?.latest_headline ||
                  "Scoped view — every chart below reflects just this session."}
              </p>
            </>
          ) : (
            <>
              <h1 className="font-display text-4xl leading-none tracking-[0.16em] text-white md:text-5xl">
                APOGEE
              </h1>
              <div className="accent-gradient-bar mt-3 h-[3px] w-32 rounded-full" />
              <p className="mt-3 max-w-xl text-[13px] text-[var(--text-muted)]">
                Live triage for every Claude Code session reporting to this
                collector.
              </p>
            </>
          )}
        </div>
        <div className="flex flex-col items-end gap-1">
          <p className="font-mono text-[11px] text-[var(--text-muted)]">
            {turns.length} turns · {runningCount} running
          </p>
          <p className="font-mono text-[10px] text-[var(--text-muted)]">
            window: {selection.time.label}
          </p>
        </div>
      </header>

      {isScoped && summary && (
        <section>
          <Card className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
            <div className="flex flex-col gap-1">
              <div className="flex items-center gap-3 font-mono text-[11px] text-[var(--text-muted)]">
                <span className="text-white">{summary.source_app || "—"}</span>
                <span>·</span>
                <span>started {formatClock(summary.started_at)}</span>
                <span>·</span>
                <span>last seen {timeAgo(summary.last_seen_at)}</span>
              </div>
              <div className="flex items-center gap-3 font-mono text-[11px] text-[var(--text-muted)]">
                <span>{summary.turn_count} turns</span>
                <span>·</span>
                <span className="text-[var(--status-info)]">
                  {summary.running_count} running
                </span>
                <span>·</span>
                <span className="text-[var(--status-success)]">
                  {summary.completed_count} completed
                </span>
                <span>·</span>
                <span
                  className={
                    summary.errored_count > 0
                      ? "text-[var(--status-critical)]"
                      : undefined
                  }
                >
                  {summary.errored_count} errored
                </span>
                {summary.model && (
                  <>
                    <span>·</span>
                    <span>{summary.model}</span>
                  </>
                )}
              </div>
            </div>
            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={clear}
                className="inline-flex items-center gap-1 rounded border border-[var(--border)] bg-[var(--bg-raised)] px-2 py-1 font-mono text-[11px] text-[var(--artemis-space)] hover:bg-[var(--bg-overlay)] hover:text-white"
              >
                <X size={12} strokeWidth={1.5} /> clear scope
              </button>
              <Link
                href={`/session/?id=${selection.sess}&tab=overview`}
                className="inline-flex items-center gap-1 rounded border border-[var(--border-bright)] bg-[var(--bg-raised)] px-2 py-1 font-mono text-[11px] text-white hover:bg-[var(--bg-overlay)]"
              >
                View tabbed detail →
              </Link>
            </div>
          </Card>
        </section>
      )}

      <section className="flex flex-col gap-3">
        <CountPills
          counts={countsData}
          activeFilter={filter}
          onSelect={setFilter}
        />
      </section>

      <section>
        <SectionHeader
          title={isScoped ? "Turns in scope" : "Active turns"}
          subtitle="Pre-ranked by the attention engine. Click a pill above to filter."
        />
        <Card className="p-0">
          <RecentTurnsTable turns={filteredTurns} />
        </Card>
      </section>

      <section>
        <SectionHeader
          title="Event ticker"
          subtitle="Last 40 hook events, newest first."
        />
        <Card className="p-0">
          <EventTicker events={events} />
        </Card>
      </section>

      <section>
        <SectionHeader
          title="Fleet KPIs"
          subtitle="Rolling 5-minute windows from the collector metric sampler."
        />
        <KpiStrip
          sessionId={selection.sess}
          sourceApp={selection.env}
        />
      </section>

      <footer className="pb-8 pt-2">
        <p className="font-mono text-[10px] text-[var(--text-muted)]">
          apogee 0.0.0-dev — live dashboard
        </p>
      </footer>
    </div>
  );
}
