"use client";

import { useMemo, useState } from "react";

import type {
  AnyApogeeEvent,
  Intervention,
  InterventionListResponse,
} from "../lib/api-types";
import { isInterventionEvent } from "../lib/api-types";
import { useEventStream } from "../lib/sse";
import { useApi } from "../lib/swr";
import { timeAgo } from "../lib/time";
import Card from "./Card";
import {
  Chip,
  MODE_ICON,
  SCOPE_ICON,
  STATUS_META,
} from "./interventionVisuals";

/**
 * InterventionTimeline — compact history card for a session's past
 * interventions (statuses `consumed`, `expired`, `cancelled`). Drawn
 * with the same chip/icon language as InterventionQueue so the
 * operator can scan either card with the same mental model.
 */

const TERMINAL_STATUSES = new Set(["consumed", "expired", "cancelled"]);

export interface InterventionTimelineProps {
  sessionId: string;
  turnId?: string | null;
  limit?: number;
}

export default function InterventionTimeline({
  sessionId,
  turnId,
  limit = 20,
}: InterventionTimelineProps) {
  const [showAll, setShowAll] = useState(false);

  const listQuery = useApi<InterventionListResponse>(
    sessionId
      ? `/v1/sessions/${sessionId}/interventions?limit=${limit * 2}`
      : null,
    { refreshInterval: 10_000 },
  );

  useEventStream<AnyApogeeEvent>(
    sessionId ? `/v1/events/stream?session_id=${sessionId}` : "",
    {
      enabled: !!sessionId,
      onEvent: (event) => {
        if (!isInterventionEvent(event)) return;
        if (event.data?.intervention?.session_id !== sessionId) return;
        if (TERMINAL_STATUSES.has(event.data.intervention.status)) {
          void listQuery.mutate();
        }
      },
    },
  );

  const interventions: Intervention[] = useMemo(() => {
    const raw = listQuery.data?.interventions ?? [];
    let filtered = raw.filter((iv) => TERMINAL_STATUSES.has(iv.status));
    if (turnId) {
      filtered = filtered.filter(
        (iv) => iv.scope === "this_session" || iv.turn_id === turnId,
      );
    }
    filtered.sort((a, b) => b.created_at.localeCompare(a.created_at));
    return filtered;
  }, [listQuery.data, turnId]);

  const visible = showAll ? interventions : interventions.slice(0, limit);
  const hasMore = interventions.length > visible.length;

  return (
    <Card className="flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <span
          className="font-display text-[10px] uppercase tracking-[0.16em]"
          style={{ color: "var(--artemis-earth)" }}
        >
          Past interventions
        </span>
        <span className="ml-auto font-mono text-[10px] text-[var(--text-muted)]">
          {interventions.length} total
        </span>
      </div>

      {interventions.length === 0 ? (
        <p className="py-4 text-center font-mono text-[11px] text-[var(--text-muted)]">
          No past interventions on this session.
        </p>
      ) : (
        <ul className="flex flex-col gap-2">
          {visible.map((iv) => (
            <TimelineRow key={iv.intervention_id} intervention={iv} />
          ))}
        </ul>
      )}

      {hasMore && (
        <button
          type="button"
          onClick={() => setShowAll(true)}
          className="mt-1 self-start rounded border px-2 py-[2px] font-mono text-[10px] text-[var(--artemis-earth)]"
          style={{
            borderColor: "var(--border-bright)",
            background: "var(--bg-overlay)",
          }}
        >
          show more ({interventions.length - visible.length})
        </button>
      )}
    </Card>
  );
}

function TimelineRow({ intervention }: { intervention: Intervention }) {
  const meta = STATUS_META[intervention.status];
  const StatusIcon = meta.icon;
  const ModeIcon = MODE_ICON[intervention.delivery_mode];
  const ScopeIcon = SCOPE_ICON[intervention.scope];

  const deliveredDetail =
    intervention.status === "consumed" && intervention.consumed_event_id
      ? `Delivered via ${intervention.delivered_via ?? "hook"} · consumed event #${intervention.consumed_event_id}`
      : intervention.delivered_via
        ? `Delivered via ${intervention.delivered_via}`
        : "";

  return (
    <li
      className="rounded border p-2"
      style={{
        background: "var(--bg-overlay)",
        borderColor: "var(--border-bright)",
      }}
    >
      <div className="flex flex-wrap items-center gap-2">
        <span
          aria-hidden
          className="inline-block h-[8px] w-[8px] rounded-full"
          style={{ background: meta.color }}
        />
        <StatusIcon size={16} strokeWidth={1.5} color={meta.color} />
        <span
          className="font-display text-[10px] uppercase tracking-[0.16em]"
          style={{ color: meta.color }}
        >
          {meta.label}
        </span>
        <span className="font-mono text-[10px] text-[var(--text-muted)]">
          {timeAgo(intervention.created_at)} ago
        </span>
        <Chip icon={<ModeIcon size={16} strokeWidth={1.5} />}>
          {intervention.delivery_mode}
        </Chip>
        <Chip icon={<ScopeIcon size={16} strokeWidth={1.5} />}>
          {intervention.scope.replace("_", " ")}
        </Chip>
      </div>
      <p
        className="mt-1 line-clamp-2 whitespace-pre-wrap font-mono text-[11px] text-gray-200"
        style={{ wordBreak: "break-word" }}
      >
        {intervention.message}
      </p>
      {deliveredDetail && (
        <p className="mt-1 font-mono text-[10px] text-[var(--text-muted)]">
          {deliveredDetail}
        </p>
      )}
    </li>
  );
}
