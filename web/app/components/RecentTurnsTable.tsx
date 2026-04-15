"use client";

import Link from "next/link";
import { ChevronRight, Orbit } from "lucide-react";
import type { AttentionState, Turn } from "../lib/api-types";
import type { StatusKey } from "../lib/design-tokens";
import { drawerLinkProps, useDrawerState } from "../lib/drawer";
import { formatClock } from "../lib/time";
import AttentionDot from "./AttentionDot";
import SessionLabel from "./SessionLabel";
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
  const { open } = useDrawerState();
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
        <p className="font-display text-[12px] text-[var(--artemis-white)]">
          No turns yet
        </p>
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
            <th className="border-b border-[var(--border)] px-3 py-2 font-medium">
              Attention
            </th>
            <th className="border-b border-[var(--border)] px-3 py-2 font-medium">
              Time
            </th>
            <th className="border-b border-[var(--border)] px-3 py-2 font-medium">
              Session
            </th>
            <th className="border-b border-[var(--border)] px-3 py-2 font-medium">
              Source App
            </th>
            <th className="border-b border-[var(--border)] px-3 py-2 font-medium">
              Phase
            </th>
            <th className="border-b border-[var(--border)] px-3 py-2 font-medium">
              Status
            </th>
            <th className="border-b border-[var(--border)] px-3 py-2 text-right font-medium">
              Tools
            </th>
            <th className="border-b border-[var(--border)] px-3 py-2 text-right font-medium">
              Subagents
            </th>
            <th className="border-b border-[var(--border)] px-3 py-2 text-right font-medium">
              Errors
            </th>
            <th className="border-b border-[var(--border)] px-3 py-2 font-medium">
              Model
            </th>
            <th
              className="border-b border-[var(--border)] px-3 py-2 font-medium"
              aria-label="Open"
            />
          </tr>
        </thead>
        <tbody>
          {turns.map((turn) => {
            const attention = normaliseAttention(turn);
            const turnHref = `/turn/?sess=${turn.session_id}&turn=${turn.turn_id}`;
            const rowProps = drawerLinkProps(turnHref, () =>
              open({
                kind: "turn",
                sess: turn.session_id,
                turn: turn.turn_id,
              }),
            );
            return (
              <tr
                key={turn.turn_id}
                onClick={(e) =>
                  rowProps.onClick(
                    e as unknown as React.MouseEvent<HTMLElement>,
                  )
                }
                className="group cursor-pointer border-b border-[var(--border)] transition-colors hover:bg-[var(--bg-raised)] focus-within:bg-[var(--bg-raised)] focus-within:ring-1 focus-within:ring-[var(--border-bright)]"
              >
                <td className="px-3 py-2">
                  <AttentionDot
                    state={attention}
                    tone={turn.attention_tone}
                    reason={turn.attention_reason}
                  />
                </td>
                <td className="px-3 py-2 font-mono text-[11px] text-[var(--text-muted)]">
                  {formatClock(turn.started_at)}
                </td>
                <td
                  className="px-3 py-2 font-mono text-[11px]"
                  onClick={(e) => e.stopPropagation()}
                >
                  <SessionLabel sessionID={turn.session_id} />
                </td>
                <td className="px-3 py-2 text-[var(--artemis-white)]">
                  {turn.source_app || "—"}
                </td>
                <td className="px-3 py-2 font-mono text-[11px] text-[var(--text-muted)]">
                  {turn.phase || "—"}
                </td>
                <td className="px-3 py-2">
                  <StatusPill tone={statusTone(turn.status)}>
                    {turn.status}
                  </StatusPill>
                </td>
                <td className="px-3 py-2 text-right font-mono tabular-nums text-[var(--artemis-white)]">
                  {turn.tool_call_count}
                </td>
                <td className="px-3 py-2 text-right font-mono tabular-nums text-[var(--artemis-white)]">
                  {turn.subagent_count}
                </td>
                <td
                  className={`px-3 py-2 text-right font-mono tabular-nums ${
                    turn.error_count > 0
                      ? "text-[var(--status-critical)]"
                      : "text-[var(--text-muted)]"
                  }`}
                >
                  {turn.error_count}
                </td>
                <td className="px-3 py-2 font-mono text-[11px] text-[var(--text-muted)]">
                  {turn.model || "—"}
                </td>
                <td className="px-2 py-2 text-right">
                  <Link
                    href={turnHref}
                    aria-label="Open turn detail"
                    className="block"
                    onClick={(e) => e.stopPropagation()}
                  >
                    <ChevronRight
                      size={14}
                      strokeWidth={1.5}
                      className="text-[var(--artemis-space)] transition-colors group-hover:text-[var(--artemis-white)]"
                    />
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
