"use client";

import { useCallback, useEffect, useRef } from "react";
import { Orbit } from "lucide-react";

import type { AttentionState, Turn } from "../lib/api-types";
import { timeAgo } from "../lib/time";
import AttentionDot from "./AttentionDot";
import Card from "./Card";

/**
 * TriageRail — vertical list of sessions with running or recent turns,
 * sorted by attention priority. Each row clicks-through to focus the turn
 * in the FocusCard. Supports keyboard navigation: Arrow keys move the
 * selection, Enter opens the full turn detail page.
 *
 * Pure presentation — the parent page owns SWR/SSE and the selected id.
 */

interface TriageRailProps {
  turns: Turn[];
  selectedTurnId: string | null;
  onSelect: (sessionId: string, turnId: string) => void;
  onOpen: (sessionId: string, turnId: string) => void;
}

function shortId(id: string, len = 8): string {
  if (!id) return "—";
  if (id.length <= len) return id;
  return id.slice(0, len);
}

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

export default function TriageRail({
  turns,
  selectedTurnId,
  onSelect,
  onOpen,
}: TriageRailProps) {
  const listRef = useRef<HTMLUListElement>(null);

  const focusRow = useCallback((turnId: string) => {
    const el = listRef.current?.querySelector<HTMLButtonElement>(
      `[data-turn-id="${turnId}"]`,
    );
    el?.focus();
  }, []);

  // When the selected id changes from outside, bring the matching row into
  // view so the keyboard-user sees which row is active.
  useEffect(() => {
    if (!selectedTurnId) return;
    const el = listRef.current?.querySelector<HTMLButtonElement>(
      `[data-turn-id="${selectedTurnId}"]`,
    );
    el?.scrollIntoView({ block: "nearest" });
  }, [selectedTurnId]);

  const onKeyDown = useCallback(
    (e: React.KeyboardEvent, idx: number) => {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        const next = turns[Math.min(idx + 1, turns.length - 1)];
        if (next) {
          onSelect(next.session_id, next.turn_id);
          focusRow(next.turn_id);
        }
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        const prev = turns[Math.max(idx - 1, 0)];
        if (prev) {
          onSelect(prev.session_id, prev.turn_id);
          focusRow(prev.turn_id);
        }
      } else if (e.key === "Enter") {
        e.preventDefault();
        const t = turns[idx];
        if (t) onOpen(t.session_id, t.turn_id);
      }
    },
    [turns, onSelect, onOpen, focusRow],
  );

  if (turns.length === 0) {
    return (
      <Card className="flex flex-col items-center gap-3 py-12 text-center">
        <div className="rounded-full border border-[var(--border-bright)] bg-[var(--bg-raised)] p-3">
          <Orbit
            size={20}
            strokeWidth={1.5}
            className="text-[var(--artemis-earth)]"
          />
        </div>
        <p className="font-display text-[11px] tracking-[0.14em] text-[var(--artemis-white)]">
          NO ACTIVE TURNS
        </p>
        <p className="max-w-[220px] text-[11px] text-[var(--text-muted)]">
          Every running turn shows up here, sorted by attention.
        </p>
      </Card>
    );
  }

  return (
    <Card className="p-0">
      <div className="border-b border-[var(--border)] px-4 py-2">
        <p className="font-display text-[10px] tracking-[0.14em] text-[var(--text-muted)]">
          TRIAGE · {turns.length}
        </p>
      </div>
      <ul ref={listRef} className="flex flex-col">
        {turns.map((turn, idx) => {
          const selected = turn.turn_id === selectedTurnId;
          const attention = normaliseAttention(turn);
          const headline =
            turn.headline ||
            turn.prompt_text ||
            `Turn ${shortId(turn.turn_id)}`;
          return (
            <li
              key={turn.turn_id}
              className={`relative border-b border-[var(--border)] last:border-b-0`}
            >
              <button
                type="button"
                data-turn-id={turn.turn_id}
                onClick={() => onSelect(turn.session_id, turn.turn_id)}
                onDoubleClick={() => onOpen(turn.session_id, turn.turn_id)}
                onKeyDown={(e) => onKeyDown(e, idx)}
                aria-pressed={selected}
                className={`group flex w-full flex-col gap-1 px-4 py-3 text-left transition-colors hover:bg-[var(--bg-raised)] focus:bg-[var(--bg-raised)] focus:outline-none focus-visible:ring-1 focus-visible:ring-[var(--border-bright)] ${
                  selected ? "bg-[var(--bg-raised)]" : ""
                }`}
              >
                {selected && (
                  <span
                    aria-hidden
                    className="absolute inset-y-0 left-0 w-[3px] rounded-r bg-[var(--accent)]"
                  />
                )}
                <div className="flex items-center justify-between gap-2">
                  <AttentionDot
                    state={attention}
                    tone={turn.attention_tone}
                    reason={turn.attention_reason}
                  />
                  <span className="font-mono text-[10px] text-[var(--text-muted)]">
                    {timeAgo(turn.started_at)}
                  </span>
                </div>
                <p className="line-clamp-2 text-[12px] leading-snug text-[var(--artemis-white)]">
                  {headline}
                </p>
                <div className="flex flex-wrap items-center gap-2 font-mono text-[10px] text-[var(--text-muted)]">
                  <span>{shortId(turn.session_id)}</span>
                  <span>·</span>
                  <span>tools {turn.tool_call_count}</span>
                  {turn.error_count > 0 && (
                    <>
                      <span>·</span>
                      <span className="text-[var(--status-critical)]">
                        errors {turn.error_count}
                      </span>
                    </>
                  )}
                  {turn.phase && (
                    <>
                      <span>·</span>
                      <span>{turn.phase}</span>
                    </>
                  )}
                </div>
              </button>
            </li>
          );
        })}
      </ul>
    </Card>
  );
}
