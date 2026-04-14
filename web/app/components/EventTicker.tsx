"use client";

import { useEffect, useState } from "react";
import {
  Activity,
  ArrowRightCircle,
  Flag,
  MessageSquare,
  PlayCircle,
  StopCircle,
  Wrench,
  type LucideIcon,
} from "lucide-react";

import type { ApogeeEvent } from "../lib/api-types";
import { SSE_EVENT_TYPES } from "../lib/api-types";
import { formatClock, timeAgo } from "../lib/time";

/**
 * EventTicker — circular buffer of recent SSE events, newest on top. Each
 * row shows an icon, the event type, the truncated id it references, and a
 * time-ago badge that re-renders every second so "now" / "2s" / "1m" stay
 * current without reconnecting to the stream.
 */

interface EventTickerProps {
  events: ApogeeEvent[];
}

const ICON_BY_TYPE: Record<string, LucideIcon> = {
  [SSE_EVENT_TYPES.Initial]: Flag,
  [SSE_EVENT_TYPES.TurnStarted]: PlayCircle,
  [SSE_EVENT_TYPES.TurnUpdated]: ArrowRightCircle,
  [SSE_EVENT_TYPES.TurnEnded]: StopCircle,
  [SSE_EVENT_TYPES.SpanInserted]: Wrench,
  [SSE_EVENT_TYPES.SpanUpdated]: Wrench,
  [SSE_EVENT_TYPES.SessionUpdated]: MessageSquare,
};

const COLOR_BY_TYPE: Record<string, string> = {
  [SSE_EVENT_TYPES.Initial]: "var(--artemis-earth)",
  [SSE_EVENT_TYPES.TurnStarted]: "var(--status-info)",
  [SSE_EVENT_TYPES.TurnUpdated]: "var(--status-info)",
  [SSE_EVENT_TYPES.TurnEnded]: "var(--status-success)",
  [SSE_EVENT_TYPES.SpanInserted]: "var(--artemis-earth)",
  [SSE_EVENT_TYPES.SpanUpdated]: "var(--artemis-earth)",
  [SSE_EVENT_TYPES.SessionUpdated]: "var(--artemis-space)",
};

/** shortId truncates long ids for the mono column. */
function shortId(id: string | undefined, len = 8): string {
  if (!id) return "";
  return id.length <= len ? id : id.slice(0, len);
}

/**
 * summarise pulls a single-line label out of a typed payload without leaking
 * any `any` into app code. Returns `{ subject, detail }` where `subject` is
 * the id the event references and `detail` is a short human label.
 */
function summarise(event: ApogeeEvent): { subject: string; detail: string } {
  const data = event.data as Record<string, unknown> | null | undefined;
  if (!data) return { subject: "", detail: event.type };

  if ("turn" in data && data.turn && typeof data.turn === "object") {
    const turn = data.turn as {
      turn_id?: string;
      session_id?: string;
      status?: string;
      tool_call_count?: number;
    };
    const pieces: string[] = [];
    if (turn.status) pieces.push(turn.status);
    if (typeof turn.tool_call_count === "number") {
      pieces.push(`${turn.tool_call_count} tools`);
    }
    return {
      subject: shortId(turn.session_id ?? turn.turn_id ?? ""),
      detail: pieces.join(" · ") || "turn",
    };
  }
  if ("span" in data && data.span && typeof data.span === "object") {
    const span = data.span as {
      tool_name?: string;
      name?: string;
      session_id?: string;
      status_code?: string;
    };
    return {
      subject: shortId(span.session_id ?? ""),
      detail: span.tool_name || span.name || "span",
    };
  }
  if ("session" in data && data.session && typeof data.session === "object") {
    const session = data.session as {
      session_id?: string;
      source_app?: string;
      turn_count?: number;
    };
    return {
      subject: shortId(session.session_id ?? ""),
      detail: session.source_app || "session",
    };
  }
  if ("recent_turns" in data || "recent_sessions" in data) {
    const initial = data as {
      recent_turns?: unknown[];
      recent_sessions?: unknown[];
    };
    return {
      subject: "",
      detail: `hydrated · ${initial.recent_turns?.length ?? 0} turns / ${initial.recent_sessions?.length ?? 0} sessions`,
    };
  }
  return { subject: "", detail: event.type };
}

export default function EventTicker({ events }: EventTickerProps) {
  // Re-render once a second so the time-ago column stays fresh without
  // touching the event buffer.
  const [tick, setTick] = useState(0);
  useEffect(() => {
    const id = setInterval(() => setTick((t) => t + 1), 1000);
    return () => clearInterval(id);
  }, []);
  void tick;

  if (events.length === 0) {
    return (
      <div className="flex flex-col items-center gap-2 py-10 text-center">
        <Activity
          size={20}
          strokeWidth={1.5}
          className="text-[var(--artemis-earth)]"
        />
        <p className="font-display text-[11px] text-white">Awaiting events</p>
        <p className="max-w-sm text-[11px] text-[var(--text-muted)]">
          The ticker fills in as the collector broadcasts new hook events.
        </p>
      </div>
    );
  }

  return (
    <ul className="flex flex-col">
      {events.map((event, idx) => {
        const Icon = ICON_BY_TYPE[event.type] ?? Activity;
        const color = COLOR_BY_TYPE[event.type] ?? "var(--text-muted)";
        const { subject, detail } = summarise(event);
        return (
          <li
            key={`${event.at}-${idx}`}
            className="flex items-center gap-3 border-b border-[var(--border)] px-3 py-1.5 text-[12px] last:border-b-0"
          >
            <span className="font-mono text-[10px] text-[var(--text-muted)]">
              {formatClock(event.at)}
            </span>
            <Icon
              size={14}
              strokeWidth={1.5}
              style={{ color }}
              className="flex-shrink-0"
            />
            <span className="font-mono text-[11px] text-gray-200">
              {event.type}
            </span>
            {subject && (
              <span className="font-mono text-[11px] text-[var(--text-muted)]">
                {subject}
              </span>
            )}
            <span className="flex-1 truncate text-[11px] text-[var(--text-muted)]">
              {detail}
            </span>
            <span className="font-mono text-[10px] text-[var(--text-muted)]">
              {timeAgo(event.at)}
            </span>
          </li>
        );
      })}
    </ul>
  );
}
