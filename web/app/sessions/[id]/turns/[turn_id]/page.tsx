"use client";

import { use, useCallback, useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";

import AttentionDot from "../../../../components/AttentionDot";
import Breadcrumb from "../../../../components/Breadcrumb";
import Card from "../../../../components/Card";
import FilterChips, {
  useFilterState,
} from "../../../../components/FilterChips";
import RawLogsPanel from "../../../../components/RawLogsPanel";
import RecapPanels from "../../../../components/RecapPanels";
import SectionHeader from "../../../../components/SectionHeader";
import SpanTree from "../../../../components/SpanTree";
import StatusPill from "../../../../components/StatusPill";
import SwimLane from "../../../../components/SwimLane";
import type {
  ApogeeEvent,
  AttentionDetail,
  Span,
  SpanPayload,
  Turn,
  TurnLogsResponse,
  TurnPayload,
  TurnSpansResponse,
} from "../../../../lib/api-types";
import { SSE_EVENT_TYPES } from "../../../../lib/api-types";
import type { StatusKey } from "../../../../lib/design-tokens";
import { useEventStream } from "../../../../lib/sse";
import { useApi } from "../../../../lib/swr";
import { formatClock, timeAgo } from "../../../../lib/time";

/**
 * `/sessions/[id]/turns/[turn_id]` — the apogee turn detail page. Pulls the
 * turn row, span list (with phase segments), logs, and engine attention
 * detail in parallel, then renders them as a header + recap placeholders +
 * swim lane + filter chips + span tree + collapsible raw logs.
 *
 * Live updates: subscribes to the SSE stream filtered to this session and
 * patches the local turn / span state when matching events arrive. Refresh
 * intervals are tightened while the turn is still running, and frozen when
 * it has completed.
 */

function statusTone(status: string): StatusKey {
  switch (status) {
    case "running":
      return "info";
    case "completed":
      return "success";
    case "errored":
      return "critical";
    case "compacted":
      return "warning";
    default:
      return "muted";
  }
}

function shortId(id: string, len = 8): string {
  if (!id) return "—";
  if (id.length <= len) return id;
  return id.slice(0, len);
}

function durationLabel(turn: Turn): string {
  if (turn.duration_ms) {
    if (turn.duration_ms < 1000) return `${turn.duration_ms}ms`;
    const seconds = turn.duration_ms / 1000;
    if (seconds < 60) return `${seconds.toFixed(1)}s`;
    const minutes = Math.floor(seconds / 60);
    const remainder = Math.round(seconds % 60);
    return remainder ? `${minutes}m${remainder}s` : `${minutes}m`;
  }
  return "—";
}

export default function TurnDetailPage({
  params,
}: {
  params: Promise<{ id: string; turn_id: string }>;
}) {
  const { id: sessionId, turn_id: turnId } = use(params);
  const router = useRouter();
  const searchParams = useSearchParams();
  const selectedSpanId = searchParams.get("span");

  const setSelectedSpan = useCallback(
    (spanId: string | null) => {
      const url = new URL(window.location.href);
      if (!spanId) {
        url.searchParams.delete("span");
      } else {
        url.searchParams.set("span", spanId);
      }
      router.replace(url.pathname + url.search, { scroll: false });
    },
    [router],
  );

  const [filter, setFilter] = useFilterState();

  // While the turn is running we want fast refreshes; once it finishes we
  // freeze polling so the rendered state is stable for the operator.
  const turnQuery = useApi<Turn>(`/v1/turns/${turnId}`, {
    refreshInterval: 2_000,
  });
  const turn = turnQuery.data;
  const isRunning = turn?.status === "running";

  const spansQuery = useApi<TurnSpansResponse>(`/v1/turns/${turnId}/spans`, {
    refreshInterval: isRunning ? 2_000 : 0,
  });
  const logsQuery = useApi<TurnLogsResponse>(`/v1/turns/${turnId}/logs`, {
    refreshInterval: isRunning ? 5_000 : 0,
  });
  const attentionQuery = useApi<AttentionDetail>(
    `/v1/turns/${turnId}/attention`,
    { refreshInterval: isRunning ? 2_000 : 0 },
  );

  // SSE patches — only react to events that affect this turn.
  const [spanPatches, setSpanPatches] = useState<Span[]>([]);
  const [turnPatch, setTurnPatch] = useState<Turn | null>(null);

  const onEvent = useCallback(
    (event: ApogeeEvent) => {
      switch (event.type) {
        case SSE_EVENT_TYPES.SpanInserted:
        case SSE_EVENT_TYPES.SpanUpdated: {
          const payload = event.data as SpanPayload;
          if (payload?.span?.turn_id === turnId) {
            setSpanPatches((prev) => [...prev, payload.span]);
          }
          break;
        }
        case SSE_EVENT_TYPES.TurnUpdated:
        case SSE_EVENT_TYPES.TurnEnded: {
          const payload = event.data as TurnPayload;
          if (payload?.turn?.turn_id === turnId) {
            setTurnPatch(payload.turn);
          }
          break;
        }
        default:
          break;
      }
    },
    [turnId],
  );

  useEventStream<ApogeeEvent>(`/v1/events/stream?session_id=${sessionId}`, {
    onEvent,
    historyLimit: 64,
  });

  const liveTurn = turnPatch ?? turn ?? null;
  const spansData = spansQuery.data;
  const phases = spansData?.phases ?? [];

  const spans: Span[] = useMemo(() => {
    const baseSpans = spansData?.spans ?? [];
    if (spanPatches.length === 0) return baseSpans;
    const byId = new Map<string, Span>();
    for (const sp of baseSpans) byId.set(sp.span_id, sp);
    for (const sp of spanPatches) byId.set(sp.span_id, sp);
    return Array.from(byId.values()).sort((a, b) =>
      a.start_time.localeCompare(b.start_time),
    );
  }, [spansData, spanPatches]);

  const logs = logsQuery.data?.logs ?? [];
  const attention = attentionQuery.data ?? null;

  if (!liveTurn) {
    return (
      <div className="mx-auto flex max-w-6xl flex-col gap-6 pt-6">
        <Breadcrumb
          segments={[
            { label: "Sessions", href: "/sessions" },
            { label: shortId(sessionId), href: `/sessions/${sessionId}` },
            { label: "Turns" },
            { label: shortId(turnId) },
          ]}
        />
        <Card>
          <p className="text-[12px] text-[var(--text-muted)]">
            {turnQuery.error ? "Failed to load turn." : "Loading turn…"}
          </p>
        </Card>
      </div>
    );
  }

  const headline =
    liveTurn.headline ||
    liveTurn.prompt_text?.slice(0, 120) ||
    `Turn ${shortId(turnId)}`;

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <header className="flex flex-col gap-3 pt-6">
        <Breadcrumb
          segments={[
            { label: "Sessions", href: "/sessions" },
            { label: shortId(sessionId), href: `/sessions/${sessionId}` },
            { label: "Turns" },
            { label: shortId(turnId) },
          ]}
        />
        <div className="flex flex-col gap-2">
          <h1 className="text-xl font-medium text-white">{headline}</h1>
          <div className="flex flex-wrap items-center gap-3 text-[12px] text-[var(--text-muted)]">
            <AttentionDot
              state={liveTurn.attention_state}
              tone={liveTurn.attention_tone}
              reason={liveTurn.attention_reason}
            />
            {liveTurn.attention_reason && (
              <span className="text-[11px]">{liveTurn.attention_reason}</span>
            )}
            <span className="font-mono text-[11px]">
              started {formatClock(liveTurn.started_at)} · {timeAgo(liveTurn.started_at)} ago
            </span>
            <span className="font-mono text-[11px]">{liveTurn.model || "—"}</span>
            <StatusPill tone={statusTone(liveTurn.status)}>
              {liveTurn.status}
            </StatusPill>
            <span className="font-mono text-[11px]">{durationLabel(liveTurn)}</span>
          </div>
          <div className="flex flex-wrap items-center gap-3 font-mono text-[11px] text-[var(--text-muted)]">
            <span>tools {liveTurn.tool_call_count}</span>
            <span>·</span>
            <span>subagents {liveTurn.subagent_count}</span>
            <span>·</span>
            <span
              className={
                liveTurn.error_count > 0
                  ? "text-[var(--status-critical)]"
                  : undefined
              }
            >
              errors {liveTurn.error_count}
            </span>
          </div>
        </div>
      </header>

      <section>
        <SectionHeader title="Recap" subtitle="Populated by the summarizer (PR #6)." />
        <RecapPanels />
      </section>

      <section>
        <SectionHeader
          title="Swim lane"
          subtitle="Turn timeline. Tool bars are coloured by status; HITL markers in warning."
        />
        <Card>
          <SwimLane
            turn={liveTurn}
            spans={spans}
            phases={phases}
            highlightedFilter={filter}
          />
        </Card>
      </section>

      <section>
        <SectionHeader
          title="Filter"
          subtitle="Selects which spans the lane and tree highlight. Persisted in the URL."
        />
        <FilterChips active={filter} onChange={setFilter} />
      </section>

      <section>
        <SectionHeader title="Span tree" subtitle="Click a row to select a span." />
        <Card className="p-2">
          <SpanTree
            spans={spans}
            selectedSpanId={selectedSpanId}
            onSelect={setSelectedSpan}
            filter={filter}
          />
        </Card>
      </section>

      <section>
        <RawLogsPanel logs={logs} title="Raw logs" />
      </section>

      <footer className="pb-8 pt-2">
        <p className="font-mono text-[10px] text-[var(--text-muted)]">
          apogee 0.0.0-dev — turn detail
          {attention?.signals?.length ? (
            <span className="ml-2">
              · {attention.signals.length} attention signals recorded
            </span>
          ) : null}
        </p>
      </footer>
    </div>
  );
}
