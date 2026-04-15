"use client";

import { ChevronDown, ChevronRight } from "lucide-react";
import { useState } from "react";

import type { LogRow } from "../lib/api-types";
import { formatClock } from "../lib/time";

/**
 * RawLogsPanel — collapsible mono-typed table of raw log records. Default
 * collapsed (the section header acts as the toggle). When expanded, each row
 * shows timestamp, hook event, severity dot, and a 120-char body snippet;
 * clicking a row reveals the full body in-place.
 */

interface RawLogsPanelProps {
  logs: LogRow[];
  title?: string;
  defaultOpen?: boolean;
  /**
   * Optional CSS max-height for the expanded log list. PR #30 adds this so
   * the session-detail Logs tab can cap the panel at 60vh and let the user
   * scroll within it instead of pushing the rest of the page down. Defaults
   * to undefined, which preserves the original unbounded behaviour for the
   * turn-detail page where the panel is the last thing on the page.
   */
  maxHeight?: string;
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
  return SEVERITY_COLOR[text.toUpperCase()] ?? "var(--status-muted)";
}

function snippet(body: string, max = 120): string {
  if (body.length <= max) return body;
  return body.slice(0, max) + "…";
}

export default function RawLogsPanel({
  logs,
  title = "Raw logs",
  defaultOpen = false,
  maxHeight,
}: RawLogsPanelProps) {
  const [open, setOpen] = useState(defaultOpen);
  const [expandedId, setExpandedId] = useState<number | null>(null);
  return (
    <div className="surface-card p-0">
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center justify-between gap-3 px-4 py-3 text-left focus:outline-none focus-visible:ring-1 focus-visible:ring-[var(--border-bright)]"
      >
        <span className="flex items-center gap-2">
          {open ? (
            <ChevronDown size={14} strokeWidth={1.5} className="text-[var(--text-muted)]" />
          ) : (
            <ChevronRight size={14} strokeWidth={1.5} className="text-[var(--text-muted)]" />
          )}
          <span className="font-display text-[12px] uppercase tracking-[0.14em] text-white">
            {title}
          </span>
          <span className="font-mono text-[11px] text-[var(--text-muted)]">
            ({logs.length})
          </span>
        </span>
      </button>
      {open && (
        <div
          className={`border-t border-[var(--border)] ${maxHeight ? "overflow-y-auto" : ""}`}
          style={maxHeight ? { maxHeight } : undefined}
        >
          {logs.length === 0 ? (
            <p className="px-4 py-6 text-center text-[12px] text-[var(--text-muted)]">
              No log records.
            </p>
          ) : (
            <ul className="divide-y divide-[var(--border)]">
              {logs.map((log) => {
                const isExpanded = expandedId === log.id;
                return (
                  <li key={log.id}>
                    <button
                      type="button"
                      aria-expanded={isExpanded}
                      onClick={() =>
                        setExpandedId((cur) => (cur === log.id ? null : log.id))
                      }
                      className="flex w-full items-start gap-3 px-4 py-2 text-left transition-colors hover:bg-[var(--bg-raised)] focus:outline-none focus-visible:bg-[var(--bg-raised)]"
                    >
                      <span className="font-mono text-[10px] text-[var(--text-muted)]">
                        {formatClock(log.timestamp)}
                      </span>
                      <span className="font-mono text-[10px] uppercase tracking-[0.1em] text-[var(--text-muted)] min-w-[120px]">
                        {log.hook_event}
                      </span>
                      <span
                        aria-hidden
                        style={{ background: severityColor(log.severity_text) }}
                        className="mt-[5px] h-[6px] w-[6px] flex-shrink-0 rounded-full"
                      />
                      <span className="flex-1 break-words font-mono text-[11px] text-gray-200">
                        {isExpanded ? log.body : snippet(log.body)}
                      </span>
                    </button>
                  </li>
                );
              })}
            </ul>
          )}
        </div>
      )}
    </div>
  );
}
