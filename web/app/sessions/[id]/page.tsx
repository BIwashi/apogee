"use client";

import { use, useCallback, useMemo, useState } from "react";

import Breadcrumb from "../../components/Breadcrumb";
import Card from "../../components/Card";
import RawLogsPanel from "../../components/RawLogsPanel";
import RecentTurnsTable from "../../components/RecentTurnsTable";
import SectionHeader from "../../components/SectionHeader";
import type {
  ApogeeEvent,
  Session,
  SessionLogsResponse,
  SessionTurnsResponse,
  Turn,
  TurnPayload,
} from "../../lib/api-types";
import { SSE_EVENT_TYPES } from "../../lib/api-types";
import { useEventStream } from "../../lib/sse";
import { useApi } from "../../lib/swr";
import { formatClock, timeAgo } from "../../lib/time";

/**
 * `/sessions/[id]` — session detail. Shows the session header, the
 * attention-sorted list of turns belonging to this session (with click-
 * through to the per-turn detail page), and a collapsed raw-log feed.
 *
 * Live updates piggy-back on the SSE stream filtered server-side via
 * `?session_id=` so only events for this session walk the wire.
 */

function attentionPriority(turn: Turn): number {
  switch (turn.attention_state) {
    case "intervene_now":
      return 0;
    case "watch":
      return 1;
    case "watchlist":
      return 2;
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
  if (id.length <= len) return id;
  return id.slice(0, len);
}

export default function SessionDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);

  const { data: session } = useApi<Session>(`/v1/sessions/${id}`, {
    refreshInterval: 5_000,
  });
  const { data: turnsData } = useApi<SessionTurnsResponse>(
    `/v1/sessions/${id}/turns`,
    { refreshInterval: 3_000 },
  );
  const { data: logsData } = useApi<SessionLogsResponse>(
    `/v1/sessions/${id}/logs?limit=50`,
    { refreshInterval: 5_000 },
  );

  const [patches, setPatches] = useState<Turn[]>([]);

  const onEvent = useCallback((event: ApogeeEvent) => {
    switch (event.type) {
      case SSE_EVENT_TYPES.TurnStarted:
      case SSE_EVENT_TYPES.TurnUpdated:
      case SSE_EVENT_TYPES.TurnEnded: {
        const payload = event.data as TurnPayload;
        if (payload?.turn?.session_id === id) {
          setPatches((prev) => {
            const next = [...prev, payload.turn];
            if (next.length > 200) return next.slice(next.length - 200);
            return next;
          });
        }
        break;
      }
      default:
        break;
    }
  }, [id]);

  useEventStream<ApogeeEvent>(`/v1/events/stream?session_id=${id}`, {
    onEvent,
    historyLimit: 64,
  });

  const turns = useMemo(() => {
    const base = turnsData?.turns ?? [];
    const byId = new Map<string, Turn>();
    for (const t of base) byId.set(t.turn_id, t);
    for (const t of patches) byId.set(t.turn_id, t);
    return sortTurns(Array.from(byId.values()));
  }, [turnsData, patches]);

  const logs = logsData?.logs ?? [];

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <header className="flex flex-col gap-3 pt-6">
        <Breadcrumb
          segments={[
            { label: "Sessions", href: "/sessions" },
            { label: shortId(id) },
          ]}
        />
        <div className="flex flex-wrap items-end justify-between gap-4">
          <div>
            <h1 className="font-display text-3xl tracking-[0.16em] text-white">
              SESSION <span className="text-[var(--accent)]">{shortId(id)}</span>
            </h1>
            <div className="accent-gradient-bar mt-3 h-[3px] w-32 rounded-full" />
          </div>
          {session && (
            <div className="flex flex-col items-end gap-1 font-mono text-[11px] text-[var(--text-muted)]">
              <span>{session.source_app || "—"}</span>
              <span>started {formatClock(session.started_at)}</span>
              <span>
                last seen {timeAgo(session.last_seen_at)} · {session.turn_count} turns
              </span>
            </div>
          )}
        </div>
      </header>

      <section>
        <SectionHeader
          title="Turns in this session"
          subtitle="Attention-sorted. Click a row to drill in."
        />
        <Card className="p-0">
          <RecentTurnsTable turns={turns} />
        </Card>
      </section>

      <section>
        <RawLogsPanel logs={logs} title="Raw logs (last 50)" />
      </section>

      <footer className="pb-8 pt-2">
        <p className="font-mono text-[10px] text-[var(--text-muted)]">
          apogee 0.0.0-dev — session detail
        </p>
      </footer>
    </div>
  );
}
