import Link from "next/link";
import { Orbit } from "lucide-react";

import type { AttentionState, Turn } from "../lib/api-types";
import type { StatusKey } from "../lib/design-tokens";
import { formatClock } from "../lib/time";
import AttentionDot from "./AttentionDot";
import StatusPill from "./StatusPill";

/**
 * RecentTurnsTable — dense, triage-ordered table of the most recent turns.
 * PR #4 reshuffles the column order: attention → time → session → source →
 * phase → status → counts → model. Rows come pre-sorted from the backend
 * (attention priority asc, started_at desc) so the table does no sorting of
 * its own.
 */

interface RecentTurnsTableProps {
  turns: Turn[];
}

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
    case "stopped":
    default:
      return "muted";
  }
}

function shortId(id: string, len = 8): string {
  if (!id) return "—";
  if (id.length <= len) return id;
  return id.slice(0, len);
}

/**
 * normaliseAttention degrades a possibly-null attention state to a typed
 * default. Pre-engine rows (older turns written before PR #4 landed) fall
 * through to `healthy` for display purposes.
 */
function normaliseAttention(t: Turn): AttentionState {
  switch (t.attention_state) {
    case "intervene_now":
    case "watch":
    case "watchlist":
    case "healthy":
      return t.attention_state;
    default:
      return "healthy";
  }
}

export default function RecentTurnsTable({ turns }: RecentTurnsTableProps) {
  if (turns.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center gap-3 py-16 text-center">
        <div className="rounded-full border border-[var(--border-bright)] bg-[var(--bg-raised)] p-3">
          <Orbit
            size={22}
            strokeWidth={1.5}
            className="text-[var(--artemis-earth)]"
          />
        </div>
        <p className="font-display text-[12px] text-white">No turns yet</p>
        <p className="max-w-sm text-[12px] text-[var(--text-muted)]">
          Start a Claude Code session to see activity appear here in real time.
        </p>
      </div>
    );
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full border-collapse text-[12px]">
        <thead>
          <tr className="text-left text-[10px] uppercase tracking-[0.14em] text-[var(--text-muted)]">
            <th className="border-b border-[var(--border)] px-3 py-2 font-medium">Attention</th>
            <th className="border-b border-[var(--border)] px-3 py-2 font-medium">Time</th>
            <th className="border-b border-[var(--border)] px-3 py-2 font-medium">Session</th>
            <th className="border-b border-[var(--border)] px-3 py-2 font-medium">Source App</th>
            <th className="border-b border-[var(--border)] px-3 py-2 font-medium">Phase</th>
            <th className="border-b border-[var(--border)] px-3 py-2 font-medium">Status</th>
            <th className="border-b border-[var(--border)] px-3 py-2 text-right font-medium">Tools</th>
            <th className="border-b border-[var(--border)] px-3 py-2 text-right font-medium">Subagents</th>
            <th className="border-b border-[var(--border)] px-3 py-2 text-right font-medium">Errors</th>
            <th className="border-b border-[var(--border)] px-3 py-2 font-medium">Model</th>
          </tr>
        </thead>
        <tbody>
          {turns.map((turn) => {
            const attention = normaliseAttention(turn);
            const turnHref = `/turn/?sess=${turn.session_id}&turn=${turn.turn_id}`;
            const sessionHref = `/session/?id=${turn.session_id}`;
            return (
              <tr
                key={turn.turn_id}
                className="group border-b border-[var(--border)] transition-colors hover:bg-[var(--bg-raised)] focus-within:bg-[var(--bg-raised)]"
              >
                <td className="px-3 py-2">
                  <Link
                    href={turnHref}
                    className="block focus:outline-none focus-visible:ring-1 focus-visible:ring-[var(--border-bright)]"
                  >
                    <AttentionDot
                      state={attention}
                      tone={turn.attention_tone}
                      reason={turn.attention_reason}
                    />
                  </Link>
                </td>
                <td className="px-3 py-2 font-mono text-[11px] text-[var(--text-muted)]">
                  <Link href={turnHref} className="block">
                    {formatClock(turn.started_at)}
                  </Link>
                </td>
                <td className="px-3 py-2 font-mono text-[11px] text-gray-200">
                  <Link
                    href={sessionHref}
                    className="hover:text-[var(--accent)] focus:outline-none focus-visible:underline"
                  >
                    {shortId(turn.session_id)}
                  </Link>
                </td>
                <td className="px-3 py-2 text-gray-200">
                  <Link href={turnHref} className="block">
                    {turn.source_app || "—"}
                  </Link>
                </td>
                <td className="px-3 py-2 font-mono text-[11px] text-[var(--text-muted)]">
                  <Link href={turnHref} className="block">
                    {turn.phase || "—"}
                  </Link>
                </td>
                <td className="px-3 py-2">
                  <Link href={turnHref} className="block">
                    <StatusPill tone={statusTone(turn.status)}>{turn.status}</StatusPill>
                  </Link>
                </td>
                <td className="px-3 py-2 text-right font-mono tabular-nums text-gray-200">
                  <Link href={turnHref} className="block">
                    {turn.tool_call_count}
                  </Link>
                </td>
                <td className="px-3 py-2 text-right font-mono tabular-nums text-gray-200">
                  <Link href={turnHref} className="block">
                    {turn.subagent_count}
                  </Link>
                </td>
                <td
                  className={`px-3 py-2 text-right font-mono tabular-nums ${
                    turn.error_count > 0 ? "text-[var(--status-critical)]" : "text-gray-200"
                  }`}
                >
                  <Link href={turnHref} className="block">
                    {turn.error_count}
                  </Link>
                </td>
                <td className="px-3 py-2 font-mono text-[11px] text-[var(--text-muted)]">
                  <Link href={turnHref} className="block">
                    {turn.model || "—"}
                  </Link>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
