"use client";

import { useCallback, useMemo, useState } from "react";

import Card from "./components/Card";
import EventTicker from "./components/EventTicker";
import LiveIndicator from "./components/LiveIndicator";
import RecentTurnsTable from "./components/RecentTurnsTable";
import SectionHeader from "./components/SectionHeader";
import type {
  ApogeeEvent,
  InitialPayload,
  RecentTurnsResponse,
  Turn,
  TurnPayload,
} from "./lib/api-types";
import { SSE_EVENT_TYPES } from "./lib/api-types";
import { useEventStream } from "./lib/sse";
import { useApi } from "./lib/swr";

/**
 * `/` — the apogee live dashboard. Hydrates from `GET /v1/turns/recent` and
 * patches itself in real time from the SSE stream at
 * `GET /v1/events/stream`. Keeps two windows on screen:
 *   1. The 20 most recent turns, replaced in place on `turn.updated` /
 *      `turn.ended`, prepended on `turn.started`.
 *   2. A 40-entry ring buffer of raw SSE events so operators can watch
 *      activity flow through the collector without a reload.
 */

const RECENT_TURNS_LIMIT = 20;
const TICKER_HISTORY = 40;

export default function Page() {
  const { data: recentTurnsData } =
    useApi<RecentTurnsResponse>("/v1/turns/recent");

  // SSE-derived state. `initialTurns` captures the `initial` payload so we
  // can use it as the base when SWR has not finished loading. `patches` is
  // an append-only log of turn updates that we fold onto the base list in
  // render order. `events` is the ticker ring buffer.
  const [initialTurns, setInitialTurns] = useState<Turn[] | null>(null);
  const [patches, setPatches] = useState<Turn[]>([]);
  const [events, setEvents] = useState<ApogeeEvent[]>([]);

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
    // Start from whichever hydration source has arrived first. `initial`
    // from SSE takes priority over SWR because it is always fresh relative
    // to the live stream.
    const base = initialTurns ?? recentTurnsData?.turns ?? [];
    const byId = new Map<string, Turn>();
    const order: string[] = [];
    for (const turn of base) {
      if (byId.has(turn.turn_id)) continue;
      byId.set(turn.turn_id, turn);
      order.push(turn.turn_id);
    }
    // Apply patches in arrival order. New turns get prepended; existing
    // turns are replaced in place.
    for (const turn of patches) {
      if (byId.has(turn.turn_id)) {
        byId.set(turn.turn_id, turn);
      } else {
        byId.set(turn.turn_id, turn);
        order.unshift(turn.turn_id);
      }
    }
    const result: Turn[] = [];
    for (const id of order) {
      const turn = byId.get(id);
      if (turn) result.push(turn);
      if (result.length >= RECENT_TURNS_LIMIT) break;
    }
    return result;
  }, [initialTurns, recentTurnsData, patches]);

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
            Live telemetry for every Claude Code session reporting to this
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

      <section>
        <SectionHeader
          title="Recent turns"
          subtitle="Latest 20 turns across every session. Live-patched from the collector."
        />
        <Card className="p-0">
          <RecentTurnsTable turns={turns} />
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

      <footer className="pb-8 pt-2">
        <p className="font-mono text-[10px] text-[var(--text-muted)]">
          apogee 0.0.0-dev — live dashboard
        </p>
      </footer>
    </div>
  );
}
