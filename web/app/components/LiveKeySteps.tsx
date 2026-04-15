"use client";

import { useMemo } from "react";
import { Activity } from "lucide-react";

import type { Span } from "../lib/api-types";
import Card from "./Card";
import SectionHeader from "./SectionHeader";

/**
 * LiveKeySteps — the "what is the agent doing right now" panel
 * that appears on the turn detail page when the turn is still
 * running and the tier-1 Haiku recap has not yet landed.
 *
 * The summarizer only writes a recap at turn close, which means
 * the existing `RecapPanels` component shows an "Awaiting recap"
 * empty state for the full duration of a long turn. The operator
 * wants "key steps" to update live, not wait until the end. The
 * summary / failure cause / notable events are fine to populate
 * only at close — they are genuinely dependent on the full tool
 * trail — but key steps can be derived in real time from the
 * streaming tool spans.
 *
 * This component renders a best-effort list of 1-8 live "steps":
 * one per distinct tool invocation in the current span list. Each
 * step is labelled with the tool name and a short one-line body
 * lifted from the span's attribute bag (file path, bash command,
 * query string, …). Steps are ordered newest-first so the latest
 * activity is always at the top.
 *
 * No new endpoint. The caller passes the already-fetched spans
 * for the turn (`/v1/turns/:id/spans` is already polled at 2 s
 * cadence by the turn detail page). When the turn closes and the
 * real recap arrives, the parent swaps this panel out for
 * `RecapPanels` which shows the LLM-written key_steps instead.
 */

interface LiveKeyStepsProps {
  spans: Span[];
  status: string;
}

interface LiveStep {
  id: string;
  label: string;
  body: string;
  startedAt: string;
  running: boolean;
}

// truncate shortens a long arg down to a readable preview.
// We split on whitespace/path separators so we favour keeping
// complete tokens over a blind substring.
function truncate(input: string, max: number): string {
  const s = input.trim();
  if (s.length <= max) return s;
  return s.slice(0, max - 1).trimEnd() + "…";
}

// preferredBody picks the most human-readable one-line description
// of what a span is doing. Different tool names expose different
// attribute keys — this function reads them in priority order and
// falls back to the raw span name when nothing specific matches.
function preferredBody(span: Span): string {
  const attrs = (span.attributes ?? {}) as Record<string, unknown>;
  const pick = (key: string): string | null => {
    const v = attrs[key];
    if (typeof v === "string" && v.trim().length > 0) return v.trim();
    return null;
  };
  // Tool-specific first — these are the fields the apogee
  // reconstructor populates on PreToolUse/PostToolUse spans.
  const candidates = [
    "claude_code.tool.file_path",
    "claude_code.tool.command",
    "claude_code.tool.query",
    "claude_code.tool.pattern",
    "claude_code.tool.url",
    "claude_code.tool.path",
    "claude_code.tool.description",
    "claude_code.tool.input",
    "claude_code.prompt",
    "file_path",
    "command",
    "query",
    "pattern",
    "url",
    "path",
    "prompt",
  ];
  for (const key of candidates) {
    const v = pick(key);
    if (v) return truncate(v, 120);
  }
  // Fall back to the span name. For tool spans the reconstructor
  // names them "tool:<tool_name>" or similar — strip the prefix
  // so the label is cleaner.
  const name = span.name ?? "";
  return truncate(name.replace(/^tool:/, ""), 120);
}

// deriveLiveSteps collapses the span list into a chronologically
// ordered list of steps. We:
//
//   - Skip spans without a tool_name (metadata / container spans)
//   - Dedupe consecutive identical (tool_name, body) pairs so a
//     noisy retry loop doesn't spam the list
//   - Keep at most 8 steps so the card stays glanceable
//   - Mark the most recent span that has no end_time as "running"
function deriveLiveSteps(spans: Span[]): LiveStep[] {
  const out: LiveStep[] = [];
  // Sort by start_time DESC so newest is first.
  const sorted = [...spans].sort((a, b) =>
    b.start_time.localeCompare(a.start_time),
  );
  for (const span of sorted) {
    const tool = span.tool_name?.trim();
    if (!tool) continue;
    const body = preferredBody(span);
    const last = out[0];
    if (last && last.label === tool && last.body === body) {
      continue;
    }
    out.unshift({
      id: span.span_id,
      label: tool,
      body,
      startedAt: span.start_time,
      running: !span.end_time,
    });
    if (out.length >= 8) {
      // Keep the last 8 (newest-first) — drop the tail as we push
      // the oldest out of the window.
      out.splice(8);
    }
  }
  // We unshifted so the resulting order is oldest-first; reverse
  // to get newest-first for display.
  return out.reverse();
}

export default function LiveKeySteps({ spans, status }: LiveKeyStepsProps) {
  const steps = useMemo(() => deriveLiveSteps(spans), [spans]);
  const isRunning = status === "running";

  return (
    <Card className="flex flex-col gap-2 p-4">
      <div className="flex items-center justify-between">
        <SectionHeader title="Live key steps" />
        {isRunning ? (
          <span className="inline-flex items-center gap-1 font-mono text-[10px] text-[var(--status-success)]">
            <Activity size={10} strokeWidth={1.75} className="animate-pulse" />
            streaming
          </span>
        ) : null}
      </div>
      <p className="font-mono text-[10px] text-[var(--text-muted)]">
        Derived from streaming tool spans — updates every ~2 s while the turn
        runs. The Haiku recap will replace this card once the turn closes.
      </p>
      {steps.length === 0 ? (
        <p className="pt-2 text-[12px] text-[var(--text-muted)]">
          No tool calls yet.
        </p>
      ) : (
        <ol className="flex flex-col gap-1 pt-1 text-[12px]">
          {steps.map((step, idx) => (
            <li
              key={step.id}
              className="flex gap-2 leading-snug text-[var(--artemis-white)]"
            >
              <span className="w-6 flex-shrink-0 font-mono text-[10px] text-[var(--text-muted)]">
                {String(idx + 1).padStart(2, "0")}
              </span>
              <span className="font-display text-[10px] uppercase tracking-[0.12em] text-[var(--artemis-earth)] flex-shrink-0 pt-[2px]">
                {step.label}
              </span>
              <span className="flex-1 break-words">{step.body}</span>
              {step.running ? (
                <span className="inline-flex items-center gap-1 text-[10px] text-[var(--status-success)]">
                  <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-[var(--status-success)]" />
                </span>
              ) : null}
            </li>
          ))}
        </ol>
      )}
    </Card>
  );
}
