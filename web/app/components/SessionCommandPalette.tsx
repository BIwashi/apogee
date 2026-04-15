"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Check, Circle, Clock, Layers, Search, X } from "lucide-react";
import type {
  RecentTurnsResponse,
  SessionSearchHit,
  SessionSearchResponse,
  Turn,
} from "../lib/api-types";
import {
  type RecentSessionEntry,
  addRecentSession,
  getRecentSessions,
} from "../lib/recent-sessions";
import { useApi } from "../lib/swr";
import { timeAgo } from "../lib/time";
import { useSelection } from "../lib/url-state";
import AttentionDot from "./AttentionDot";

/**
 * SessionCommandPalette — the global ⌘K command palette. Shown as a native
 * <dialog> so focus trapping and backdrop handling come for free. The body
 * has three sections (RECENT / ACTIVE / ALL SESSIONS), each contributing to
 * a single flat keyboard-navigable list.
 *
 * Opens via:
 *   - ⌘K / Ctrl+K global binding registered in layout.tsx
 *   - The session button in TopRibbon
 *   - Any other call to the open() callback on the imperative handle
 */

interface SessionCommandPaletteProps {
  open: boolean;
  onClose: () => void;
}

interface Row {
  session_id: string;
  source_app: string;
  label: string;
  last_seen_at: string;
  turn_count?: number;
  attention_state?: string;
  section: "recent" | "active" | "all";
}

function shortId(id: string, len = 8): string {
  if (!id) return "—";
  return id.length <= len ? id : id.slice(0, len);
}

/** groupActiveByTurns collapses a list of active turns into one row per session. */
function groupActiveByTurns(turns: Turn[]): Row[] {
  const byId = new Map<string, Row>();
  for (const t of turns) {
    const existing = byId.get(t.session_id);
    if (existing) {
      existing.turn_count = (existing.turn_count ?? 0) + 1;
      // Prefer the most recent last_seen_at.
      if (t.started_at > existing.last_seen_at)
        existing.last_seen_at = t.started_at;
      continue;
    }
    byId.set(t.session_id, {
      session_id: t.session_id,
      source_app: t.source_app,
      label:
        t.headline ||
        t.prompt_text?.slice(0, 80) ||
        `Session ${shortId(t.session_id)}`,
      last_seen_at: t.started_at,
      turn_count: 1,
      attention_state: t.attention_state ?? undefined,
      section: "active",
    });
  }
  return Array.from(byId.values());
}

function hitToRow(h: SessionSearchHit): Row {
  return {
    session_id: h.session_id,
    source_app: h.source_app,
    label:
      h.latest_headline?.trim() ||
      h.latest_prompt_snippet?.trim() ||
      `Session ${shortId(h.session_id)}`,
    last_seen_at: h.last_seen_at,
    turn_count: h.turn_count,
    attention_state: h.attention_state,
    section: "all",
  };
}

function recentToRow(e: RecentSessionEntry): Row {
  return {
    session_id: e.session_id,
    source_app: e.source_app,
    label: e.label,
    last_seen_at: e.last_seen_at,
    section: "recent",
  };
}

export default function SessionCommandPalette({
  open,
  onClose,
}: SessionCommandPaletteProps) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const { setSelection, clear } = useSelection();

  const [query, setQuery] = useState("");
  const [debouncedQuery, setDebouncedQuery] = useState("");
  const [recent, setRecent] = useState<RecentSessionEntry[]>([]);
  // Cursor position across the flattened row list. We reset it during
  // render when the list "generation" changes (open toggle or debounced
  // query change) using React's recommended
  // "store the previous derivation in state" pattern.
  const [cursor, setCursor] = useState(0);
  const [prevOpen, setPrevOpen] = useState(open);
  const [prevQuery, setPrevQuery] = useState(debouncedQuery);
  if (open !== prevOpen) {
    setPrevOpen(open);
    if (open) {
      setRecent(getRecentSessions());
    }
    setCursor(0);
  }
  if (debouncedQuery !== prevQuery) {
    setPrevQuery(debouncedQuery);
    setCursor(0);
  }

  // Debounce the search input at 150ms.
  useEffect(() => {
    const handle = setTimeout(() => setDebouncedQuery(query), 150);
    return () => clearTimeout(handle);
  }, [query]);

  // Drive the native dialog's open state.
  useEffect(() => {
    const el = dialogRef.current;
    if (!el) return;
    if (open && !el.open) {
      el.showModal();
      // Native <dialog> won't focus the input unless we kick it.
      requestAnimationFrame(() => inputRef.current?.focus());
    } else if (!open && el.open) {
      el.close();
    }
  }, [open]);

  const handleCancel = useCallback(
    (ev: React.SyntheticEvent<HTMLDialogElement>) => {
      ev.preventDefault();
      onClose();
    },
    [onClose],
  );

  // Fetch active turns + search results. We issue the search every time the
  // debounced query changes; the backend caps at 50 hits by default.
  const activeRes = useApi<RecentTurnsResponse>(
    open ? "/v1/turns/active?limit=200" : null,
    {
      refreshInterval: 5_000,
    },
  );
  const searchKey = open
    ? `/v1/sessions/search?q=${encodeURIComponent(debouncedQuery)}&limit=50`
    : null;
  const searchRes = useApi<SessionSearchResponse>(searchKey);

  const recentRows = useMemo<Row[]>(() => recent.map(recentToRow), [recent]);
  const activeRows = useMemo<Row[]>(
    () => groupActiveByTurns(activeRes.data?.turns ?? []),
    [activeRes.data],
  );
  const allRows = useMemo<Row[]>(
    () => (searchRes.data?.sessions ?? []).map(hitToRow),
    [searchRes.data],
  );

  // Flatten all three sections into one navigable list so the arrow keys
  // move across them seamlessly.
  const flatRows = useMemo<Row[]>(
    () => [...recentRows, ...activeRows, ...allRows],
    [recentRows, activeRows, allRows],
  );

  const activate = useCallback(
    (row: Row) => {
      setSelection({ sess: row.session_id, env: row.source_app || null });
      addRecentSession({
        session_id: row.session_id,
        source_app: row.source_app,
        label: row.label,
        last_seen_at: row.last_seen_at,
      });
      onClose();
    },
    [setSelection, onClose],
  );

  const onKeyDown = useCallback(
    (ev: React.KeyboardEvent<HTMLDivElement>) => {
      if (ev.key === "ArrowDown") {
        ev.preventDefault();
        setCursor((c) => Math.min(flatRows.length - 1, c + 1));
      } else if (ev.key === "ArrowUp") {
        ev.preventDefault();
        setCursor((c) => Math.max(0, c - 1));
      } else if (ev.key === "Enter") {
        ev.preventDefault();
        const row = flatRows[cursor];
        if (row) activate(row);
      } else if (ev.key === "Escape") {
        ev.preventDefault();
        onClose();
      }
    },
    [flatRows, cursor, activate, onClose],
  );

  return (
    <dialog
      ref={dialogRef}
      onCancel={handleCancel}
      onClose={onClose}
      className="m-0 max-h-[80vh] w-full max-w-2xl rounded-lg border border-[var(--border-bright)] bg-[var(--bg-overlay)] p-0 text-[var(--artemis-white)] shadow-[var(--shadow-lg)] backdrop:bg-[var(--overlay-backdrop)]"
      style={{ top: "12vh", left: "50%", transform: "translateX(-50%)" }}
      aria-label="Session command palette"
    >
      <div onKeyDown={onKeyDown}>
        <div className="flex items-center gap-3 border-b border-[var(--border)] px-4 py-3">
          <Search
            size={16}
            strokeWidth={1.5}
            className="text-[var(--artemis-space)]"
          />
          <input
            ref={inputRef}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search sessions — id, source, prompt text…"
            className="flex-1 bg-transparent font-mono text-[13px] text-[var(--artemis-white)] outline-none placeholder:text-[var(--artemis-space)]"
            autoComplete="off"
            spellCheck={false}
          />
          <button
            type="button"
            onClick={onClose}
            className="rounded p-1 text-[var(--artemis-space)] hover:bg-[var(--bg-raised)] hover:text-[var(--artemis-white)]"
            aria-label="Close palette"
          >
            <X size={14} strokeWidth={1.5} />
          </button>
        </div>

        <div className="max-h-[60vh] overflow-y-auto">
          <PaletteSection
            title="Recent"
            icon={Clock}
            rows={recentRows}
            flatOffset={0}
            cursor={cursor}
            onActivate={activate}
          />
          <PaletteSection
            title="Active"
            icon={Circle}
            rows={activeRows}
            flatOffset={recentRows.length}
            cursor={cursor}
            onActivate={activate}
          />
          <PaletteSection
            title="All sessions"
            icon={Layers}
            rows={allRows}
            flatOffset={recentRows.length + activeRows.length}
            cursor={cursor}
            onActivate={activate}
          />
        </div>

        <div className="flex items-center justify-between border-t border-[var(--border)] px-4 py-2 text-[11px] text-[var(--artemis-space)]">
          <button
            type="button"
            onClick={() => {
              clear();
              onClose();
            }}
            className="rounded px-2 py-1 hover:bg-[var(--bg-raised)] hover:text-[var(--artemis-white)]"
          >
            Clear selection — show fleet view
          </button>
          <span className="font-mono">
            <kbd className="rounded border border-[var(--border-bright)] px-1">
              ↑↓
            </kbd>
            <span className="px-1">nav</span>
            <kbd className="rounded border border-[var(--border-bright)] px-1">
              ⏎
            </kbd>
            <span className="px-1">select</span>
            <kbd className="rounded border border-[var(--border-bright)] px-1">
              esc
            </kbd>
            <span className="pl-1">close</span>
          </span>
        </div>
      </div>
    </dialog>
  );
}

interface PaletteSectionProps {
  title: string;
  icon: React.ComponentType<{
    size?: number;
    strokeWidth?: number;
    className?: string;
  }>;
  rows: Row[];
  flatOffset: number;
  cursor: number;
  onActivate: (row: Row) => void;
}

function PaletteSection({
  title,
  icon: Icon,
  rows,
  flatOffset,
  cursor,
  onActivate,
}: PaletteSectionProps) {
  if (rows.length === 0) return null;
  return (
    <div className="py-1">
      <div className="flex items-center gap-2 px-4 pb-1 pt-2 font-display text-[10px] uppercase tracking-[0.14em] text-[var(--artemis-space)]">
        <Icon size={12} strokeWidth={1.5} />
        <span>{title}</span>
      </div>
      <ul>
        {rows.map((row, i) => {
          const flatIdx = flatOffset + i;
          const isSelected = cursor === flatIdx;
          return (
            <li key={`${row.section}-${row.session_id}`}>
              <button
                type="button"
                onClick={() => onActivate(row)}
                className={`flex w-full items-center gap-3 border-l-2 px-4 py-1.5 text-left text-[12px] transition-colors ${
                  isSelected
                    ? "border-[var(--artemis-earth)] bg-[var(--bg-raised)]"
                    : "border-transparent hover:bg-[var(--bg-raised)]"
                }`}
              >
                <AttentionDot state={row.attention_state} />
                <span className="w-20 font-mono text-[11px] text-[var(--artemis-white)]">
                  {shortId(row.session_id)}
                </span>
                <span className="flex-1 truncate text-[12px] text-[var(--artemis-white)]">
                  {row.label}
                </span>
                <span className="w-28 truncate font-mono text-[11px] text-[var(--artemis-space)]">
                  {row.source_app || "—"}
                </span>
                <span className="w-16 font-mono text-[10px] text-[var(--artemis-space)]">
                  {timeAgo(row.last_seen_at)}
                </span>
                <span className="w-8 text-right font-mono text-[11px] text-[var(--artemis-space)]">
                  {row.turn_count ?? ""}
                </span>
                {isSelected && (
                  <Check
                    size={12}
                    strokeWidth={1.5}
                    className="text-[var(--artemis-earth)]"
                  />
                )}
              </button>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
