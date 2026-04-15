"use client";

import { AlertTriangle, X } from "lucide-react";
import {
  useCallback,
  useEffect,
  useMemo,
  useState,
} from "react";

import type {
  AnyApogeeEvent,
  Intervention,
  InterventionListResponse,
} from "../lib/api-types";
import { isInterventionEvent } from "../lib/api-types";
import { apiUrl } from "../lib/api";
import { useEventStream } from "../lib/sse";
import { useApi } from "../lib/swr";
import { timeAgo } from "../lib/time";
import Card from "./Card";
import {
  Chip,
  MODE_ICON,
  SCOPE_ICON,
  STATUS_META,
  computeStaleness,
  sortInterventions,
  urgencyAccent,
} from "./interventionVisuals";

/**
 * InterventionQueue — live card showing every non-terminal intervention
 * for a session. Combines SWR polling (2s) with an SSE subscription that
 * calls `mutate()` on any matching `intervention.*` event so the queue
 * updates instantly when a hook claims or delivers a row.
 *
 * Each row carries a staleness indicator: if the row has been `queued`
 * for more than 30s we show a warning pill; past 120s it escalates to
 * critical with "no hook activity" text, which is the UI half of the
 * idle-session safety net described in docs/interventions.md.
 */

export interface InterventionQueueProps {
  sessionId: string;
  turnId?: string | null;
  onCancel?: (intervention: Intervention) => void;
}

const PENDING_STATUSES = new Set(["queued", "claimed", "delivered"]);

export default function InterventionQueue({
  sessionId,
  turnId,
  onCancel,
}: InterventionQueueProps) {
  const pendingQuery = useApi<InterventionListResponse>(
    sessionId ? `/v1/sessions/${sessionId}/interventions/pending` : null,
    { refreshInterval: 2_000 },
  );

  // Optimistic cancel — rows added to this set disappear immediately and
  // are restored only if the POST fails.
  const [cancellingIds, setCancellingIds] = useState<Set<string>>(
    () => new Set(),
  );

  // Drive row-level staleness labels from a 1Hz tick so the timers update
  // smoothly without each row holding its own interval.
  const [nowMs, setNowMs] = useState(() => Date.now());
  useEffect(() => {
    const interval = setInterval(() => setNowMs(Date.now()), 1_000);
    return () => clearInterval(interval);
  }, []);

  const interventionFilter = useMemo(
    () => (sessionId ? { sessionId } : undefined),
    [sessionId],
  );
  const { subscribe: subscribeInterventions } = useEventStream(interventionFilter);
  useEffect(() => {
    if (!sessionId) return;
    return subscribeInterventions((event) => {
      const anyEvent = event as AnyApogeeEvent;
      if (!isInterventionEvent(anyEvent)) return;
      if (anyEvent.data?.intervention?.session_id !== sessionId) return;
      void pendingQuery.mutate();
    });
  }, [subscribeInterventions, sessionId, pendingQuery]);

  const interventions: Intervention[] = useMemo(() => {
    const raw = pendingQuery.data?.interventions ?? [];
    let filtered = raw.filter(
      (iv) => PENDING_STATUSES.has(iv.status) && !cancellingIds.has(iv.intervention_id),
    );
    // When scoped to a turn, still show session-scoped rows so the
    // operator can see messages that apply to multiple turns.
    if (turnId) {
      filtered = filtered.filter(
        (iv) =>
          iv.scope === "this_session" ||
          iv.turn_id === turnId ||
          !iv.turn_id,
      );
    }
    return sortInterventions(filtered);
  }, [pendingQuery.data, turnId, cancellingIds]);

  const onCancelClick = useCallback(
    async (iv: Intervention) => {
      setCancellingIds((prev) => {
        const next = new Set(prev);
        next.add(iv.intervention_id);
        return next;
      });
      try {
        const resp = await fetch(
          apiUrl(`/v1/interventions/${iv.intervention_id}/cancel`),
          { method: "POST" },
        );
        if (!resp.ok) {
          throw new Error(`${resp.status}: ${resp.statusText}`);
        }
        onCancel?.(iv);
        void pendingQuery.mutate();
      } catch {
        // Roll back the optimistic cancel so the row reappears.
        setCancellingIds((prev) => {
          const next = new Set(prev);
          next.delete(iv.intervention_id);
          return next;
        });
      }
    },
    [onCancel, pendingQuery],
  );

  return (
    <Card className="flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <span
          className="font-display text-[10px] uppercase tracking-[0.16em]"
          style={{ color: "var(--artemis-earth)" }}
        >
          Pending interventions
        </span>
        <span className="ml-auto font-mono text-[10px] text-[var(--text-muted)]">
          {interventions.length} in flight
        </span>
      </div>
      {interventions.length === 0 ? (
        <p className="py-4 text-center font-mono text-[11px] text-[var(--text-muted)]">
          No operator interventions pending.
        </p>
      ) : (
        <ul className="flex flex-col gap-2">
          {interventions.map((iv) => (
            <QueueRow
              key={iv.intervention_id}
              intervention={iv}
              nowMs={nowMs}
              onCancel={() => void onCancelClick(iv)}
            />
          ))}
        </ul>
      )}
    </Card>
  );
}

function QueueRow({
  intervention,
  nowMs,
  onCancel,
}: {
  intervention: Intervention;
  nowMs: number;
  onCancel: () => void;
}) {
  const [expanded, setExpanded] = useState(false);
  const meta = STATUS_META[intervention.status];
  const ModeIcon = MODE_ICON[intervention.delivery_mode];
  const ScopeIcon = SCOPE_ICON[intervention.scope];
  const StatusIcon = meta.icon;
  const stale = computeStaleness(intervention, nowMs);
  const accent = urgencyAccent(intervention.urgency);

  const canCancel =
    intervention.status === "queued" || intervention.status === "claimed";

  const lines = intervention.message.split("\n");
  const truncated = !expanded && lines.length > 3;
  const shownBody = truncated ? lines.slice(0, 3).join("\n") + "…" : intervention.message;

  return (
    <li
      className="rounded-md border-l-[3px] border p-3"
      style={{
        background: "var(--bg-raised)",
        borderColor: "var(--border-bright)",
        borderLeftColor: accent,
      }}
    >
      <div className="flex flex-wrap items-center gap-2">
        <span
          aria-hidden
          className="inline-block h-[8px] w-[8px] rounded-full"
          style={{ background: meta.color, boxShadow: `0 0 6px ${meta.color}` }}
        />
        <StatusIcon size={16} strokeWidth={1.5} color={meta.color} />
        <span
          className="font-display text-[10px] uppercase tracking-[0.16em]"
          style={{ color: meta.color }}
        >
          {meta.label}
        </span>
        <Chip icon={<ModeIcon size={16} strokeWidth={1.5} />}>
          {intervention.delivery_mode}
        </Chip>
        <Chip icon={<ScopeIcon size={16} strokeWidth={1.5} />}>
          {intervention.scope.replace("_", " ")}
        </Chip>
        <Chip
          tone={
            intervention.urgency === "high"
              ? "critical"
              : intervention.urgency === "normal"
                ? "warning"
                : "muted"
          }
        >
          {intervention.urgency}
        </Chip>
        {stale.tone && (
          <span
            className="ml-auto inline-flex items-center gap-1 rounded border px-2 py-[2px] font-mono text-[10px]"
            style={{
              background: "var(--bg-overlay)",
              borderColor:
                stale.tone === "critical"
                  ? "var(--status-critical)"
                  : "var(--status-warning)",
              color:
                stale.tone === "critical"
                  ? "var(--status-critical)"
                  : "var(--status-warning)",
            }}
          >
            <AlertTriangle size={16} strokeWidth={1.5} />
            {stale.label}
          </span>
        )}
      </div>

      <p
        className="mt-2 whitespace-pre-wrap font-mono text-[11px] leading-snug text-gray-200"
        style={{ wordBreak: "break-word" }}
      >
        {shownBody}
      </p>
      {lines.length > 3 && (
        <button
          type="button"
          onClick={() => setExpanded((v) => !v)}
          className="mt-1 font-mono text-[10px] text-[var(--artemis-earth)] hover:underline"
        >
          {expanded ? "show less" : "show more"}
        </button>
      )}

      <div className="mt-2 flex flex-wrap items-center gap-2 font-mono text-[10px] text-[var(--text-muted)]">
        <span>
          Submitted
          {intervention.operator_id ? ` by ${intervention.operator_id}` : ""}
        </span>
        <span aria-hidden>·</span>
        <span>{timeAgo(intervention.created_at)} ago</span>
        {intervention.status === "queued" && (
          <>
            <span aria-hidden>·</span>
            <span>waiting for next hook</span>
          </>
        )}
        {intervention.status === "claimed" && (
          <>
            <span aria-hidden>·</span>
            <span>claimed, awaiting delivery</span>
          </>
        )}
        {intervention.status === "delivered" && (
          <>
            <span aria-hidden>·</span>
            <span>delivered{intervention.delivered_via ? ` via ${intervention.delivered_via}` : ""}</span>
          </>
        )}
        {canCancel && (
          <button
            type="button"
            onClick={onCancel}
            className="ml-auto inline-flex items-center gap-1 rounded border px-2 py-[2px] text-[10px]"
            style={{
              borderColor: "var(--border-bright)",
              background: "var(--bg-overlay)",
              color: "var(--text-muted)",
            }}
          >
            <X size={16} strokeWidth={1.5} /> Cancel
          </button>
        )}
      </div>
    </li>
  );
}
