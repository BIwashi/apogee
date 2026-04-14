"use client";

import { useCallback, useMemo, useState } from "react";

import Card from "./components/Card";
import CountPills from "./components/CountPills";
import EventTicker from "./components/EventTicker";
import KpiStrip from "./components/KpiStrip";
import LiveIndicator from "./components/LiveIndicator";
import RecentTurnsTable from "./components/RecentTurnsTable";
import SectionHeader from "./components/SectionHeader";
import type {
  ApogeeEvent,
  AttentionCounts,
  AttentionState,
  InitialPayload,
  RecentTurnsResponse,
  Turn,
  TurnPayload,
} from "./lib/api-types";
import { SSE_EVENT_TYPES } from "./lib/api-types";
import { useEventStream } from "./lib/sse";
import { useApi } from "./lib/swr";

/**
 * `/` — the apogee live triage dashboard. Hydrates from
 * `GET /v1/turns/active` (attention-sorted), patches itself in real time
 * from the SSE stream, and polls `GET /v1/attention/counts` every 2 s for
 * the top-of-page count pills. The KPI strip pulls its own 5-second
 * sparklines from `/v1/metrics/series`.
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

export default function Page() {
  const { data: activeTurnsData } =
    useApi<RecentTurnsResponse>("/v1/turns/active");
  const { data: countsData } = useApi<AttentionCounts>(
    "/v1/attention/counts",
    { refreshInterval: 2_000 },
  );

  // SSE-derived state. Mirrors the PR #3 layout but drops the initial
  // fallback entirely — the dedicated /v1/turns/active endpoint gives us a
  // correctly-sorted list on first paint, and SSE patches from then on.
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

  const { status } = useEventStream<ApogeeEvent>("/v1/events/stream", {
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
    const merged = Array.from(byId.values());
    return sortTurns(merged).slice(0, RECENT_TURNS_LIMIT);
  }, [initialTurns, activeTurnsData, patches]);

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

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <header className="flex flex-wrap items-end justify-between gap-4 pt-6">
        <div>
          <h1 className="font-display text-4xl leading-none tracking-[0.16em] text-white md:text-5xl">
            APOGEE
          </h1>
          <div className="accent-gradient-bar mt-3 h-[3px] w-32 rounded-full" />
          <p className="mt-3 max-w-xl text-[13px] text-[var(--text-muted)]">
            Live triage for every Claude Code session reporting to this
            collector.
          </p>
        </div>
        <div className="flex flex-col items-end gap-1">
          <LiveIndicator status={status} />
          <p className="font-mono text-[11px] text-[var(--text-muted)]">
            {turns.length} turns · {runningCount} running
          </p>
        </div>
      </header>

      <section className="flex flex-col gap-3">
        <CountPills
          counts={countsData}
          activeFilter={filter}
          onSelect={setFilter}
        />
      </section>

      <section>
        <SectionHeader
          title="Active turns"
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
        <KpiStrip />
      </section>

      <footer className="pb-8 pt-2">
        <p className="font-mono text-[10px] text-[var(--text-muted)]">
          apogee 0.0.0-dev — live dashboard
        </p>
      </footer>
    </div>
  );
}
