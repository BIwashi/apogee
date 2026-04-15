"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import CountPills from "./components/CountPills";
import EventTicker from "./components/EventTicker";
import FocusCard from "./components/FocusCard";
import KpiStrip from "./components/KpiStrip";
import SectionHeader from "./components/SectionHeader";
import TriageRail from "./components/TriageRail";
import VersionTag from "./components/VersionTag";
import type {
  ApogeeEvent,
  AttentionCounts,
  InitialPayload,
  PhaseSegment,
  RecapResponse,
  RecentTurnsResponse,
  Span,
  SpanPayload,
  Turn,
  TurnPayload,
  TurnSpansResponse,
} from "./lib/api-types";
import { SSE_EVENT_TYPES } from "./lib/api-types";
import { useEventStream } from "./lib/sse";
import { useApi } from "./lib/swr";
import { buildQuery, useSelection } from "./lib/url-state";

/**
 * `/` — apogee Live. The focus-card driven landing page. PR #24 makes the
 * currently-running turn the hero of the view: a large FocusCard on the
 * right displays the flame graph, recap headline, phase, and CTA; a
 * vertical TriageRail on the left lists every running/recent turn sorted
 * by attention.
 *
 * Data flow:
 *   - `/v1/turns/active` (SWR, 2s) feeds the triage rail.
 *   - A cross-session SSE stream patches the rail and the ticker.
 *   - When a turn is focused, `/v1/turns/:id/spans` + `/v1/turns/:id/recap`
 *     feed the FocusCard, refreshing while the turn is running.
 *   - Focus selection persists as `?focus=<turn_id>` for deep linking.
 */

const ACTIVE_LIMIT = 40;
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

/**
 * pickFocusedTurn returns the turn the FocusCard should render. Preference
 * order: the `?focus=<id>` override, then the highest-priority running
 * turn, then the highest-priority non-running turn, then null.
 */
function pickFocusedTurn(
  turns: Turn[],
  explicitId: string | null,
): Turn | null {
  if (explicitId) {
    const match = turns.find((t) => t.turn_id === explicitId);
    if (match) return match;
  }
  const running = turns.filter((t) => t.status === "running");
  if (running.length > 0) return running[0];
  return turns[0] ?? null;
}

export default function LivePage() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const focusParam = searchParams.get("focus");

  // Read the TopRibbon selection so the TimeRangePicker's "Last 5m",
  // "Last 1h" etc. presets actually scope the Live fetches. The
  // apiParams bundle already carries since/until/session_id/source_app
  // in the shape the collector /v1/turns/active and /v1/attention/counts
  // filters accept (parseTurnFilter in internal/collector/server.go).
  const { apiParams } = useSelection();

  const { data: activeTurnsData, mutate: mutateActive } =
    useApi<RecentTurnsResponse>("/v1/turns/active" + buildQuery(apiParams), {
      refreshInterval: 2_000,
    });
  const { data: countsData } = useApi<AttentionCounts>(
    "/v1/attention/counts" + buildQuery(apiParams, { include: "ended" }),
    { refreshInterval: 2_000 },
  );

  // SSE-derived state — patches the triage rail between SWR polls and
  // fills the ambient event ticker.
  const [patches, setPatches] = useState<Turn[]>([]);
  const [events, setEvents] = useState<ApogeeEvent[]>([]);
  const [initialTurns, setInitialTurns] = useState<Turn[] | null>(null);

  const { subscribe: subscribeAll } = useEventStream<ApogeeEvent>();

  const onEvent = useCallback((event: ApogeeEvent) => {
    setEvents((prev) => {
      const next = [event, ...prev];
      if (next.length > TICKER_HISTORY) next.length = TICKER_HISTORY;
      return next;
    });
    switch (event.type) {
      case SSE_EVENT_TYPES.Initial: {
        const payload = event.data as InitialPayload;
        setInitialTurns(payload?.recent_turns?.slice(0, ACTIVE_LIMIT) ?? []);
        break;
      }
      case SSE_EVENT_TYPES.TurnStarted:
      case SSE_EVENT_TYPES.TurnUpdated:
      case SSE_EVENT_TYPES.TurnEnded: {
        const payload = event.data as TurnPayload;
        if (payload?.turn) {
          setPatches((prev) => [...prev, payload.turn]);
        }
        break;
      }
      default:
        break;
    }
  }, []);

  useEffect(() => subscribeAll(onEvent), [subscribeAll, onEvent]);

  const turns = useMemo(() => {
    const base = initialTurns ?? activeTurnsData?.turns ?? [];
    const byId = new Map<string, Turn>();
    for (const turn of base) byId.set(turn.turn_id, turn);
    for (const turn of patches) byId.set(turn.turn_id, turn);
    return sortTurns(Array.from(byId.values())).slice(0, ACTIVE_LIMIT);
  }, [initialTurns, activeTurnsData, patches]);

  const focusedTurn = useMemo(
    () => pickFocusedTurn(turns, focusParam),
    [turns, focusParam],
  );

  const setFocus = useCallback(
    (turnId: string | null) => {
      const url = new URL(window.location.href);
      if (turnId) {
        url.searchParams.set("focus", turnId);
      } else {
        url.searchParams.delete("focus");
      }
      router.replace(url.pathname + (url.search || ""), { scroll: false });
    },
    [router],
  );

  const onTriageSelect = useCallback(
    (_sessionId: string, turnId: string) => {
      setFocus(turnId);
    },
    [setFocus],
  );

  const onTriageOpen = useCallback(
    (sessionId: string, turnId: string) => {
      router.push(`/turn/?sess=${sessionId}&turn=${turnId}`);
    },
    [router],
  );

  // Focused-turn detail data. While running, poll at 2s; when the turn ends
  // SWR freezes so the rendered state is stable for the operator.
  const focusedTurnId = focusedTurn?.turn_id ?? null;
  const isFocusedRunning = focusedTurn?.status === "running";
  const spansQuery = useApi<TurnSpansResponse>(
    focusedTurnId ? `/v1/turns/${focusedTurnId}/spans` : null,
    { refreshInterval: isFocusedRunning ? 2_000 : 0 },
  );
  const recapQuery = useApi<RecapResponse>(
    focusedTurnId ? `/v1/turns/${focusedTurnId}/recap` : null,
    { refreshInterval: isFocusedRunning ? 5_000 : 0 },
  );

  // SSE span patching for the focused turn. Keeps the flame graph warm
  // between SWR polls. Reset-on-focus-change is done during render via a
  // tracking ref so React doesn't have to chain effects.
  const [focusedSpanPatches, setFocusedSpanPatches] = useState<Span[]>([]);
  const lastFocusedTurnIdRef = useRef<string | null>(focusedTurnId);
  if (lastFocusedTurnIdRef.current !== focusedTurnId) {
    lastFocusedTurnIdRef.current = focusedTurnId;
    if (focusedSpanPatches.length > 0) {
      setFocusedSpanPatches([]);
    }
  }

  const focusedSessionId = focusedTurn?.session_id ?? "";
  const focusedFilter = useMemo(
    () => (focusedSessionId ? { sessionId: focusedSessionId } : undefined),
    [focusedSessionId],
  );
  const { subscribe: subscribeFocused } =
    useEventStream<ApogeeEvent>(focusedFilter);
  const onFocusedSSE = useCallback(
    (event: ApogeeEvent) => {
      switch (event.type) {
        case SSE_EVENT_TYPES.SpanInserted:
        case SSE_EVENT_TYPES.SpanUpdated: {
          const payload = event.data as SpanPayload;
          if (payload?.span?.turn_id === focusedTurnId) {
            setFocusedSpanPatches((prev) => [...prev, payload.span]);
          }
          break;
        }
        case SSE_EVENT_TYPES.TurnEnded: {
          void mutateActive();
          break;
        }
        default:
          break;
      }
    },
    [focusedTurnId, mutateActive],
  );
  useEffect(() => {
    if (!focusedSessionId) return;
    return subscribeFocused(onFocusedSSE);
  }, [subscribeFocused, onFocusedSSE, focusedSessionId]);

  const focusedSpans: Span[] = useMemo(() => {
    const base = spansQuery.data?.spans ?? [];
    if (focusedSpanPatches.length === 0) return base;
    const byId = new Map<string, Span>();
    for (const sp of base) byId.set(sp.span_id, sp);
    for (const sp of focusedSpanPatches) byId.set(sp.span_id, sp);
    return Array.from(byId.values()).sort((a, b) =>
      a.start_time.localeCompare(b.start_time),
    );
  }, [spansQuery.data, focusedSpanPatches]);
  const focusedPhases: PhaseSegment[] = spansQuery.data?.phases ?? [];

  // Cheapest "current tool" signal: the most recent tool span.
  const currentTool = useMemo(() => {
    for (let i = focusedSpans.length - 1; i >= 0; i--) {
      const span = focusedSpans[i];
      if (span.tool_name) return span.tool_name;
    }
    return undefined;
  }, [focusedSpans]);

  const runningCount = useMemo(
    () => turns.filter((t) => t.status === "running").length,
    [turns],
  );

  return (
    <div className="mx-auto flex max-w-7xl flex-col gap-6">
      <header className="flex flex-wrap items-end justify-between gap-4 pt-6">
        <div>
          <h1 className="font-display-accent text-4xl leading-none tracking-[0.16em] text-[var(--artemis-white)] md:text-5xl">
            LIVE
          </h1>
          <div className="accent-gradient-bar mt-3 h-[3px] w-32 rounded-full" />
          <p className="mt-3 max-w-xl text-[13px] text-[var(--text-muted)]">
            Focus card driven triage. The hero is the turn you&apos;re watching
            right now — pick a different one from the rail on the left.
          </p>
        </div>
        <div className="flex flex-col items-end gap-1">
          <p className="font-mono text-[11px] text-[var(--text-muted)]">
            {turns.length} turns · {runningCount} running
          </p>
        </div>
      </header>

      <section>
        <CountPills
          counts={countsData}
          activeFilter={null}
          onSelect={() => {
            /* filter handled inside /sessions; here the pills are read-only */
          }}
        />
      </section>

      {/*
       * EventTicker — pinned directly below the count pills with a fixed
       * 180px max-height and internal scroll. PR #30 made this the stable
       * anchor of the live dashboard so new events no longer push the
       * triage / focus grid offscreen. The ring buffer is capped at 40
       * events upstream in onEvent, so the DOM never grows past that.
       */}
      <section>
        <EventTicker events={events} maxHeightPx={180} />
      </section>

      {/*
       * Datadog-style two-column layout for the live view: the
       * TriageRail on the left grows as the number of running turns
       * climbs (potentially much taller than the focus card on the
       * right), so we pin the FocusCard with `sticky` and let the
       * rail scroll underneath it. `items-start` keeps both grid
       * tracks top-aligned, which is what `sticky` needs to work —
       * otherwise the default `stretch` alignment forces every cell
       * to the grid row's max height and there is nothing for the
       * sticky element to anchor against.
       *
       * The top offset (`lg:top-4`) matches the page gutter, and
       * the max-height caps the sticky card at one viewport minus
       * that gutter so its internal scrollbar takes over if the
       * focused turn's span tree is also long. On narrow
       * breakpoints the grid collapses to a single column and
       * `lg:sticky` reverts to the default flow.
       */}
      <section className="grid items-start gap-4 lg:grid-cols-12">
        <div className="lg:col-span-4">
          <TriageRail
            turns={turns}
            selectedTurnId={focusedTurn?.turn_id ?? null}
            onSelect={onTriageSelect}
            onOpen={onTriageOpen}
          />
        </div>
        <div className="lg:sticky lg:top-4 lg:col-span-8 lg:max-h-[calc(100vh-2rem)] lg:self-start lg:overflow-y-auto">
          <FocusCard
            turn={focusedTurn}
            spans={focusedSpans}
            phases={focusedPhases}
            recap={recapQuery.data ?? null}
            currentTool={currentTool}
          />
        </div>
      </section>

      <section>
        <SectionHeader
          title="Fleet KPIs"
          subtitle="Rolling 5-minute windows from the collector metric sampler."
        />
        <KpiStrip />
      </section>

      <footer className="pb-8 pt-2">
        <VersionTag suffix="Live" />
      </footer>
    </div>
  );
}
