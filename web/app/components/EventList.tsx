"use client";

import type { LogRow } from "../lib/api-types";
import { formatClock, timeAgo } from "../lib/time";

/**
 * EventList — paginated table view of raw log records used by the `/events`
 * route. Each row renders timestamp, hook event, source app, session id, a
 * severity dot, and a one-line snippet of the body. Click handler is wired
 * by the parent so the page can pop a SideDrawer with the full payload.
 *
 * The component is purely presentational: pagination controls live in the
 * parent so the URL state stays the source of truth for the current page.
 */

interface EventListProps {
  logs: LogRow[];
  loading: boolean;
  error: string | null;
  onRowClick: (log: LogRow) => void;
  selectedId?: number | null;
}

const SEVERITY_COLOR: Record<string, string> = {
  TRACE: "var(--status-muted)",
  DEBUG: "var(--status-muted)",
  INFO: "var(--status-info)",
  WARN: "var(--status-warning)",
  ERROR: "var(--status-critical)",
  FATAL: "var(--status-critical)",
};

function severityColor(text: string): string {
  return SEVERITY_COLOR[(text || "").toUpperCase()] ?? "var(--status-muted)";
}

function shortId(id: string | null | undefined, len = 10): string {
  if (!id) return "—";
  return id.length <= len ? id : id.slice(0, len);
}

function snippet(body: string, max = 160): string {
  if (body.length <= max) return body;
  return body.slice(0, max) + "…";
}

export default function EventList({
  logs,
  loading,
  error,
  onRowClick,
  selectedId,
}: EventListProps) {
  if (loading && logs.length === 0) {
    return (
      <p className="px-4 py-10 text-center text-[12px] text-[var(--text-muted)]">
        Loading events…
      </p>
    );
  }
  if (error) {
    return (
      <p className="px-4 py-10 text-center text-[12px] text-[var(--status-critical)]">
        {error}
      </p>
    );
  }
  if (logs.length === 0) {
    return (
      <p className="px-4 py-10 text-center text-[12px] text-[var(--text-muted)]">
        No events match the current filter.
      </p>
    );
  }
  return (
    <table className="w-full border-collapse text-[12px]">
      <thead>
        <tr className="text-left text-[10px] uppercase tracking-[0.14em] text-[var(--text-muted)]">
          <th className="border-b border-[var(--border)] px-3 py-2 font-medium">
            Time
          </th>
          <th className="border-b border-[var(--border)] px-3 py-2 font-medium">
            Event
          </th>
          <th className="border-b border-[var(--border)] px-3 py-2 font-medium">
            Source
          </th>
          <th className="border-b border-[var(--border)] px-3 py-2 font-medium">
            Session
          </th>
          <th className="border-b border-[var(--border)] px-3 py-2 font-medium">
            Body
          </th>
          <th className="border-b border-[var(--border)] px-3 py-2 text-right font-medium">
            Ago
          </th>
        </tr>
      </thead>
      <tbody>
        {logs.map((log) => {
          const selected = selectedId === log.id;
          return (
            <tr
              key={log.id}
              onClick={() => onRowClick(log)}
              className={`group cursor-pointer border-b border-[var(--border)] transition-colors ${
                selected
                  ? "bg-[var(--bg-raised)]"
                  : "hover:bg-[var(--bg-raised)]"
              }`}
            >
              <td className="whitespace-nowrap px-3 py-2 font-mono text-[10px] text-[var(--text-muted)]">
                {formatClock(log.timestamp)}
              </td>
              <td className="px-3 py-2">
                <span className="inline-flex items-center gap-2">
                  <span
                    aria-hidden
                    style={{ background: severityColor(log.severity_text) }}
                    className="h-[6px] w-[6px] flex-shrink-0 rounded-full"
                  />
                  <span className="font-mono text-[11px] text-gray-200">
                    {log.hook_event || "—"}
                  </span>
                </span>
              </td>
              <td className="px-3 py-2 font-mono text-[11px] text-[var(--text-muted)]">
                {log.source_app || "—"}
              </td>
              <td className="px-3 py-2 font-mono text-[11px] text-[var(--text-muted)]">
                {shortId(log.session_id, 8)}
              </td>
              <td className="px-3 py-2">
                <span className="line-clamp-1 font-mono text-[11px] text-[var(--text-muted)]">
                  {snippet(log.body)}
                </span>
              </td>
              <td className="whitespace-nowrap px-3 py-2 text-right font-mono text-[10px] text-[var(--text-muted)]">
                {timeAgo(log.timestamp)}
              </td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}
