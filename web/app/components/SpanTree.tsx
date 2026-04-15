"use client";

import { ChevronDown, ChevronRight } from "lucide-react";
import { useMemo, useState } from "react";

import type { FilterKey, HITLEvent, Span, SpanTreeNode } from "../lib/api-types";
import HITLTimelineItem from "./HITLTimelineItem";

/**
 * SpanTree — recursive waterfall view of a turn's spans. Builds a tree from
 * the flat span list using parent_span_id, then renders one row per span
 * with a triangle/collapse toggle, mono-typed name, duration pill, and a
 * coloured status dot. Click a row to select it; the parent component
 * persists the selection in URL state for deep linking.
 *
 * Filter integration: when a FilterKey other than "all" is active, rows
 * whose spans don't match are greyed out so the operator still sees the
 * full hierarchy.
 */

interface SpanTreeProps {
  spans: Span[];
  selectedSpanId: string | null;
  onSelect: (spanId: string | null) => void;
  filter?: FilterKey;
  hitlEvents?: HITLEvent[];
}

function buildTree(spans: Span[]): SpanTreeNode[] {
  const byId = new Map<string, SpanTreeNode>();
  for (const sp of spans) {
    byId.set(sp.span_id, { ...sp, children: [] });
  }
  const roots: SpanTreeNode[] = [];
  for (const node of byId.values()) {
    const parent = node.parent_span_id ? byId.get(node.parent_span_id) : null;
    if (parent) {
      parent.children.push(node);
    } else {
      roots.push(node);
    }
  }
  // Sort children by start_time ascending so the waterfall reads left-to-right.
  const sortRecursive = (list: SpanTreeNode[]) => {
    list.sort((a, b) => a.start_time.localeCompare(b.start_time));
    for (const child of list) sortRecursive(child.children);
  };
  sortRecursive(roots);
  return roots;
}

function statusColor(status: string): string {
  switch (status) {
    case "OK":
      return "var(--status-success)";
    case "ERROR":
      return "var(--status-critical)";
    default:
      return "var(--status-muted)";
  }
}

function durationLabel(span: Span): string {
  if (span.duration_ns) {
    const ms = span.duration_ns / 1_000_000;
    if (ms < 1) return "<1ms";
    if (ms < 1000) return `${Math.round(ms)}ms`;
    const seconds = ms / 1000;
    if (seconds < 60) return `${seconds.toFixed(1)}s`;
    return `${Math.round(seconds)}s`;
  }
  if (span.start_time && span.end_time) {
    const ms = Date.parse(span.end_time) - Date.parse(span.start_time);
    if (Number.isFinite(ms) && ms >= 0) {
      if (ms < 1000) return `${ms}ms`;
      return `${(ms / 1000).toFixed(1)}s`;
    }
  }
  return "—";
}

function matchesFilter(span: Span, filter: FilterKey | undefined): boolean {
  if (!filter || filter === "all") return true;
  switch (filter) {
    case "tools":
      return Boolean(span.tool_name) || span.name.startsWith("claude_code.tool");
    case "commands":
      return span.tool_name === "Bash" || span.tool_name === "KillShell";
    case "errors":
      return span.status_code === "ERROR";
    case "hitl":
      return span.name === "claude_code.hitl.permission";
    case "subagents":
      return span.name.startsWith("claude_code.subagent");
    case "messages":
      return span.name === "claude_code.turn";
    default:
      return true;
  }
}

interface RowProps {
  node: SpanTreeNode;
  depth: number;
  selectedSpanId: string | null;
  onSelect: (id: string | null) => void;
  collapsed: Record<string, boolean>;
  toggle: (id: string) => void;
  filter?: FilterKey;
  hitlBySpan?: Map<string, HITLEvent>;
}

function statusChip(event: HITLEvent): { bg: string; label: string } {
  if (event.status === "pending") {
    return { bg: "var(--status-warning)", label: "pending" };
  }
  if (event.status === "expired" || event.status === "timeout") {
    return { bg: "var(--status-muted)", label: event.status };
  }
  if (event.decision === "deny") {
    return { bg: "var(--status-critical)", label: "deny" };
  }
  if (event.decision === "allow") {
    return { bg: "var(--status-success)", label: "allow" };
  }
  return { bg: "var(--artemis-earth)", label: event.status };
}

function SpanRow({
  node,
  depth,
  selectedSpanId,
  onSelect,
  collapsed,
  toggle,
  filter,
  hitlBySpan,
}: RowProps) {
  const isCollapsed = collapsed[node.span_id] === true;
  const hasChildren = node.children.length > 0;
  const matches = matchesFilter(node, filter);
  const isSelected = selectedSpanId === node.span_id;
  const dimmed = !matches;
  const hitlEvent = hitlBySpan?.get(node.span_id);
  const isHITLRow = node.name === "claude_code.hitl.permission";
  return (
    <li>
      <div
        role="treeitem"
        aria-selected={isSelected}
        tabIndex={0}
        onClick={() => onSelect(isSelected ? null : node.span_id)}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            onSelect(isSelected ? null : node.span_id);
          }
        }}
        className={`flex items-center gap-2 px-2 py-1 rounded transition-colors cursor-pointer ${
          isSelected
            ? "bg-[var(--accent)]/15 ring-1 ring-[var(--accent)]/40"
            : "hover:bg-[var(--bg-raised)]"
        } ${dimmed ? "opacity-30" : "opacity-100"}`}
        style={{ paddingLeft: 8 + depth * 14 }}
      >
        {hasChildren ? (
          <button
            type="button"
            aria-label={isCollapsed ? "Expand" : "Collapse"}
            onClick={(e) => {
              e.stopPropagation();
              toggle(node.span_id);
            }}
            className="text-[var(--text-muted)] hover:text-[var(--artemis-white)]"
          >
            {isCollapsed ? (
              <ChevronRight size={12} strokeWidth={1.5} />
            ) : (
              <ChevronDown size={12} strokeWidth={1.5} />
            )}
          </button>
        ) : (
          <span className="inline-block h-3 w-3" aria-hidden />
        )}
        <span
          aria-hidden
          style={{ background: statusColor(node.status_code) }}
          className="h-[6px] w-[6px] flex-shrink-0 rounded-full"
        />
        <span className="flex-1 truncate font-mono text-[11px] text-[var(--artemis-white)]">
          {node.name}
        </span>
        {hitlEvent && (
          <HITLStatusChip event={hitlEvent} />
        )}
        <span className="font-mono text-[10px] text-[var(--text-muted)]">
          {durationLabel(node)}
        </span>
      </div>
      {isHITLRow && hitlEvent && hitlEvent.status !== "pending" && (
        <HITLTimelineItem event={hitlEvent} />
      )}
      {hasChildren && !isCollapsed && (
        <ul role="group">
          {node.children.map((child) => (
            <SpanRow
              key={child.span_id}
              node={child}
              depth={depth + 1}
              selectedSpanId={selectedSpanId}
              onSelect={onSelect}
              collapsed={collapsed}
              toggle={toggle}
              filter={filter}
              hitlBySpan={hitlBySpan}
            />
          ))}
        </ul>
      )}
    </li>
  );
}

function HITLStatusChip({ event }: { event: HITLEvent }) {
  const tone = statusChip(event);
  return (
    <span
      className="rounded px-1.5 py-[1px] font-mono text-[9px] uppercase"
      style={{ background: tone.bg, color: "var(--chip-on-accent)" }}
    >
      {tone.label}
    </span>
  );
}

export default function SpanTree({
  spans,
  selectedSpanId,
  onSelect,
  filter,
  hitlEvents,
}: SpanTreeProps) {
  const tree = useMemo(() => buildTree(spans), [spans]);
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});
  const toggle = (id: string) =>
    setCollapsed((cur) => ({ ...cur, [id]: !cur[id] }));

  const hitlBySpan = useMemo(() => {
    const map = new Map<string, HITLEvent>();
    for (const ev of hitlEvents ?? []) {
      if (ev.span_id) map.set(ev.span_id, ev);
    }
    return map;
  }, [hitlEvents]);

  if (spans.length === 0) {
    return (
      <p className="px-3 py-6 text-center text-[12px] text-[var(--text-muted)]">
        No spans recorded for this turn.
      </p>
    );
  }

  return (
    <ul role="tree" className="py-1">
      {tree.map((root) => (
        <SpanRow
          key={root.span_id}
          node={root}
          depth={0}
          selectedSpanId={selectedSpanId}
          onSelect={onSelect}
          collapsed={collapsed}
          toggle={toggle}
          filter={filter}
          hitlBySpan={hitlBySpan}
        />
      ))}
    </ul>
  );
}
