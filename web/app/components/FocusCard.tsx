"use client";

import Link from "next/link";
import { ArrowRight, Wrench } from "lucide-react";

import type {
  PhaseSegment,
  RecapResponse,
  Span,
  Turn,
} from "../lib/api-types";
import { formatClock, timeAgo } from "../lib/time";
import AttentionDot from "./AttentionDot";
import Card from "./Card";
import FocusCardEmpty from "./FocusCardEmpty";
import RecapPanels from "./RecapPanels";
import StatusPill from "./StatusPill";
import SwimLane from "./SwimLane";
import type { StatusKey } from "../lib/design-tokens";

/**
 * FocusCard — the hero of the `/` Live page. Renders the currently-focused
 * running turn as a single dense card: header (session · attention ·
 * duration), a live-updating SwimLane flame graph preview, a compact
 * 3-column RecapPanels row, and a prominent CTA that deep-links into the
 * full turn detail page.
 *
 * Pure presentation. Data plumbing (SWR + SSE) lives in the parent page.
 */

interface FocusCardProps {
  turn: Turn | null;
  spans: Span[];
  phases: PhaseSegment[];
  recap: RecapResponse | null;
  currentTool?: string;
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
  // Live elapsed clock when running.
  const start = Date.parse(turn.started_at);
  if (!Number.isFinite(start)) return "—";
  const elapsed = Math.max(0, Date.now() - start);
  const seconds = Math.floor(elapsed / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const remainder = seconds % 60;
  return remainder ? `${minutes}m${remainder}s` : `${minutes}m`;
}

export default function FocusCard({
  turn,
  spans,
  phases,
  recap,
  currentTool,
}: FocusCardProps) {
  if (!turn) {
    return <FocusCardEmpty />;
  }

  const headline =
    recap?.recap?.headline ||
    turn.headline ||
    turn.prompt_text?.slice(0, 140) ||
    `Turn ${shortId(turn.turn_id)}`;

  const turnHref = `/turn/?sess=${turn.session_id}&turn=${turn.turn_id}`;

  return (
    <Card className="flex flex-col gap-5">
      <div className="flex flex-col gap-3">
        <div className="flex flex-wrap items-center gap-3">
          <AttentionDot
            state={turn.attention_state}
            tone={turn.attention_tone}
            reason={turn.attention_reason}
          />
          <span className="font-mono text-[11px] text-[var(--text-muted)]">
            session {shortId(turn.session_id)}
          </span>
          <span className="font-mono text-[11px] text-[var(--text-muted)]">
            {turn.source_app || "—"}
          </span>
          <StatusPill tone={statusTone(turn.status)}>{turn.status}</StatusPill>
          <span className="font-mono text-[11px] text-[var(--text-muted)]">
            {durationLabel(turn)}
          </span>
          <span className="font-mono text-[11px] text-[var(--text-muted)]">
            started {formatClock(turn.started_at)} · {timeAgo(turn.started_at)}{" "}
            ago
          </span>
        </div>

        <h2 className="text-xl font-semibold leading-snug text-white md:text-2xl">
          {headline}
        </h2>

        <div className="flex flex-wrap items-center gap-3 font-mono text-[11px] text-[var(--text-muted)]">
          <span>
            phase{" "}
            <span className="text-white">
              {turn.phase || "—"}
            </span>
          </span>
          <span>·</span>
          <span className="inline-flex items-center gap-1">
            <Wrench size={12} strokeWidth={1.5} />
            tools{" "}
            <span className="text-white">{turn.tool_call_count}</span>
          </span>
          {currentTool && (
            <>
              <span>·</span>
              <span>
                last <span className="text-white">{currentTool}</span>
              </span>
            </>
          )}
          <span>·</span>
          <span>
            subagents{" "}
            <span className="text-white">{turn.subagent_count}</span>
          </span>
          <span>·</span>
          <span
            className={
              turn.error_count > 0 ? "text-[var(--status-critical)]" : undefined
            }
          >
            errors <span className="text-white">{turn.error_count}</span>
          </span>
        </div>
      </div>

      <div className="rounded border border-[var(--border)] bg-[var(--bg-raised)] p-3">
        <SwimLane turn={turn} spans={spans} phases={phases} />
      </div>

      <div className="flex items-center justify-between gap-3">
        <p className="font-mono text-[10px] text-[var(--text-muted)]">
          flame graph updates every 2s via SSE · {spans.length} spans
        </p>
        <Link
          href={turnHref}
          className="inline-flex items-center gap-2 rounded border border-[var(--border-bright)] bg-[var(--bg-raised)] px-4 py-2 font-display text-[12px] tracking-[0.14em] text-white transition-colors hover:bg-[var(--bg-overlay)]"
        >
          OPEN TURN DETAIL
          <ArrowRight size={14} strokeWidth={1.5} />
        </Link>
      </div>

      <RecapPanels recap={recap?.recap ?? null} />
    </Card>
  );
}
