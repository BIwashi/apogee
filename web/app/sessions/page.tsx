"use client";

import Link from "next/link";

import Card from "../components/Card";
import SectionHeader from "../components/SectionHeader";
import type { RecentSessionsResponse, Session } from "../lib/api-types";
import { formatClock, timeAgo } from "../lib/time";
import { useApi } from "../lib/swr";

/**
 * `/sessions` — flat listing of every session the collector has seen
 * recently. Used as the entry point from the sidebar; click-through opens
 * the per-session detail page. Intentionally minimal in PR #5 — PR #11 will
 * add filtering and aggregations.
 */

function shortId(id: string, len = 12): string {
  if (!id) return "—";
  if (id.length <= len) return id;
  return id.slice(0, len);
}

export default function SessionsPage() {
  const { data, error, isLoading } = useApi<RecentSessionsResponse>(
    "/v1/sessions/recent",
    { refreshInterval: 5_000 },
  );
  const sessions: Session[] = data?.sessions ?? [];

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <header className="flex flex-wrap items-end justify-between gap-4 pt-6">
        <div>
          <h1 className="font-display text-3xl tracking-[0.16em] text-white">
            SESSIONS
          </h1>
          <div className="accent-gradient-bar mt-3 h-[3px] w-32 rounded-full" />
          <p className="mt-3 max-w-xl text-[13px] text-[var(--text-muted)]">
            Every Claude Code session reporting to this collector. Click a row to
            drill into its turns.
          </p>
        </div>
      </header>

      <section>
        <SectionHeader
          title="Recent sessions"
          subtitle="Sorted by last activity."
        />
        <Card className="p-0">
          {isLoading ? (
            <p className="px-4 py-10 text-center text-[12px] text-[var(--text-muted)]">
              Loading sessions…
            </p>
          ) : error ? (
            <p className="px-4 py-10 text-center text-[12px] text-[var(--status-critical)]">
              Failed to load sessions.
            </p>
          ) : sessions.length === 0 ? (
            <p className="px-4 py-10 text-center text-[12px] text-[var(--text-muted)]">
              No sessions yet.
            </p>
          ) : (
            <table className="w-full border-collapse text-[12px]">
              <thead>
                <tr className="text-left text-[10px] uppercase tracking-[0.14em] text-[var(--text-muted)]">
                  <th className="border-b border-[var(--border)] px-3 py-2 font-medium">Session</th>
                  <th className="border-b border-[var(--border)] px-3 py-2 font-medium">Source App</th>
                  <th className="border-b border-[var(--border)] px-3 py-2 font-medium">Started</th>
                  <th className="border-b border-[var(--border)] px-3 py-2 font-medium">Last Seen</th>
                  <th className="border-b border-[var(--border)] px-3 py-2 text-right font-medium">Turns</th>
                  <th className="border-b border-[var(--border)] px-3 py-2 font-medium">Model</th>
                </tr>
              </thead>
              <tbody>
                {sessions.map((session) => (
                  <tr
                    key={session.session_id}
                    className="border-b border-[var(--border)] transition-colors hover:bg-[var(--bg-raised)]"
                  >
                    <td className="px-3 py-2 font-mono text-[11px] text-gray-200">
                      <Link
                        href={`/sessions/${session.session_id}`}
                        className="hover:text-[var(--accent)]"
                      >
                        {shortId(session.session_id)}
                      </Link>
                    </td>
                    <td className="px-3 py-2 text-gray-200">
                      {session.source_app || "—"}
                    </td>
                    <td className="px-3 py-2 font-mono text-[11px] text-[var(--text-muted)]">
                      {formatClock(session.started_at)}
                    </td>
                    <td className="px-3 py-2 font-mono text-[11px] text-[var(--text-muted)]">
                      {timeAgo(session.last_seen_at)}
                    </td>
                    <td className="px-3 py-2 text-right font-mono tabular-nums text-gray-200">
                      {session.turn_count}
                    </td>
                    <td className="px-3 py-2 font-mono text-[11px] text-[var(--text-muted)]">
                      {session.model || "—"}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </Card>
      </section>
    </div>
  );
}
