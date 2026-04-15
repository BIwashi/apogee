"use client";

import { useMemo, useState } from "react";
import { Activity } from "lucide-react";
import type {
  HITLListResponse,
  RecapResponse,
  Span,
  Turn,
  TurnSpansResponse,
} from "../lib/api-types";
import type { StatusKey } from "../lib/design-tokens";
import { useDrawerState } from "../lib/drawer";
import { useApi } from "../lib/swr";
import { formatClock, timeAgo } from "../lib/time";
import DrawerFooterAction from "./DrawerFooterAction";
import DrawerHeader, { DrawerTabBar } from "./DrawerHeader";
import DrawerKeyValue, { DrawerSection } from "./DrawerKeyValue";
import SessionLabel from "./SessionLabel";
import StatusPill from "./StatusPill";

/**
 * TurnDrawer — in-place detail for a single turn. Four tabs cover the
 * summary, recap narrative, flat span list, and HITL events without pulling
 * in the full swim-lane machinery. Every cross-link (session, parent span)
 * calls useDrawerState().open() so the drawer stays mounted and content
 * swaps in place.
 */

interface TurnDrawerProps {
  sessionID: string;
  turnID: string;
}

type TabKey = "overview" | "recap" | "spans" | "hitl";

const TABS: ReadonlyArray<{ key: TabKey; label: string }> = [
  { key: "overview", label: "Overview" },
  { key: "recap", label: "Recap" },
  { key: "spans", label: "Spans" },
  { key: "hitl", label: "HITL" },
];

function shortId(id: string, len = 8): string {
  if (!id) return "—";
  return id.length <= len ? id : id.slice(0, len);
}

function statusTone(status: string | undefined): StatusKey {
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

function humanDuration(ms: number | undefined | null): string {
  if (!ms || ms <= 0) return "—";
  if (ms < 1000) return `${Math.round(ms)}ms`;
  const seconds = ms / 1000;
  if (seconds < 60) return `${seconds.toFixed(1)}s`;
  const minutes = Math.floor(seconds / 60);
  const rem = Math.round(seconds % 60);
  if (minutes < 60) return rem ? `${minutes}m${rem}s` : `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  const remMin = minutes % 60;
  return remMin ? `${hours}h${remMin}m` : `${hours}h`;
}

function spanDurationMs(span: Span): number {
  if (span.duration_ns) return span.duration_ns / 1_000_000;
  if (span.start_time && span.end_time) {
    const ms = Date.parse(span.end_time) - Date.parse(span.start_time);
    if (Number.isFinite(ms) && ms >= 0) return ms;
  }
  return 0;
}

export default function TurnDrawer({ sessionID, turnID }: TurnDrawerProps) {
  const [tab, setTab] = useState<TabKey>("overview");
  const { open } = useDrawerState();

  const turnQuery = useApi<Turn>(turnID ? `/v1/turns/${turnID}` : null, {
    refreshInterval: 3_000,
  });
  const spansQuery = useApi<TurnSpansResponse>(
    turnID && tab === "spans" ? `/v1/turns/${turnID}/spans` : null,
  );
  const recapQuery = useApi<RecapResponse>(
    turnID && tab === "recap" ? `/v1/turns/${turnID}/recap` : null,
  );
  const hitlQuery = useApi<HITLListResponse>(
    turnID && tab === "hitl" ? `/v1/turns/${turnID}/hitl` : null,
  );

  const turn = turnQuery.data ?? null;
  const spans: Span[] = useMemo(
    () =>
      (spansQuery.data?.spans ?? [])
        .slice()
        .sort((a, b) => a.start_time.localeCompare(b.start_time)),
    [spansQuery.data],
  );
  const maxSpanMs = useMemo(
    () => spans.reduce((acc, sp) => Math.max(acc, spanDurationMs(sp)), 0),
    [spans],
  );
  const headline =
    turn?.headline ||
    turn?.prompt_text?.slice(0, 80) ||
    `Turn ${shortId(turnID)}`;
  const recap = recapQuery.data?.recap ?? null;
  const hitlRows = hitlQuery.data?.hitl ?? [];

  return (
    <div className="flex flex-col gap-4">
      <DrawerHeader
        icon={Activity}
        kind="Turn"
        title={
          <span className="flex flex-col gap-1">
            <span className="font-mono text-[13px] text-[var(--artemis-white)]">
              {shortId(turnID)}
            </span>
            <span className="text-[13px] font-normal text-[var(--text-muted)]">
              {headline}
            </span>
          </span>
        }
        trailing={
          turn ? (
            <StatusPill tone={statusTone(turn.status)}>
              {turn.status}
            </StatusPill>
          ) : null
        }
        subtitle={
          turn?.started_at ? (
            <>
              started {formatClock(turn.started_at)} ·{" "}
              {timeAgo(turn.started_at)}
            </>
          ) : null
        }
      />

      <DrawerTabBar<TabKey> tabs={TABS} active={tab} onChange={setTab} />

      {tab === "overview" && (
        <div className="flex flex-col gap-4">
          <DrawerSection title="Session">
            <SessionLabel sessionID={sessionID} />
          </DrawerSection>

          <DrawerSection title="Identity">
            <DrawerKeyValue
              rows={[
                {
                  label: "turn_id",
                  value: turnID,
                  mono: true,
                  copyable: turnID,
                },
                {
                  label: "trace_id",
                  value: turn?.trace_id || "—",
                  mono: true,
                  copyable: turn?.trace_id || undefined,
                },
                {
                  label: "model",
                  value: turn?.model || "—",
                  mono: true,
                },
                {
                  label: "phase",
                  value: turn?.phase || "—",
                  mono: true,
                },
              ]}
            />
          </DrawerSection>

          <DrawerSection title="Counters">
            <DrawerKeyValue
              rows={[
                {
                  label: "duration",
                  value: humanDuration(turn?.duration_ms),
                  mono: true,
                },
                {
                  label: "tools",
                  value: turn?.tool_call_count ?? 0,
                  mono: true,
                },
                {
                  label: "subagents",
                  value: turn?.subagent_count ?? 0,
                  mono: true,
                },
                {
                  label: "errors",
                  value: turn?.error_count ?? 0,
                  mono: true,
                  tone: (turn?.error_count ?? 0) > 0 ? "critical" : "default",
                },
                {
                  label: "attention",
                  value: turn?.attention_state || "—",
                  mono: true,
                },
              ]}
            />
          </DrawerSection>
        </div>
      )}

      {tab === "recap" && (
        <div className="flex flex-col gap-3">
          {recapQuery.isLoading && !recap && (
            <p className="text-[11px] text-[var(--text-muted)]">
              Loading recap…
            </p>
          )}
          {!recap && !recapQuery.isLoading && (
            <p className="text-[11px] text-[var(--text-muted)]">
              No recap has been generated for this turn yet.
            </p>
          )}
          {recap && (
            <>
              <DrawerSection title="Headline">
                <p className="text-[13px] text-[var(--artemis-white)]">
                  {recap.headline || "—"}
                </p>
                {recap.outcome && (
                  <span
                    className="mt-1 inline-flex rounded border px-2 py-[1px] font-mono text-[10px] uppercase"
                    style={{
                      borderColor: "var(--border)",
                      color: "var(--text-muted)",
                    }}
                  >
                    {recap.outcome}
                  </span>
                )}
              </DrawerSection>
              {recap.key_steps?.length > 0 && (
                <DrawerSection title="Key steps">
                  <ul className="flex flex-col gap-1 text-[12px] text-[var(--artemis-white)]">
                    {recap.key_steps.map((step, idx) => (
                      <li key={idx} className="flex gap-2">
                        <span className="text-[var(--text-muted)]">·</span>
                        <span>{step}</span>
                      </li>
                    ))}
                  </ul>
                </DrawerSection>
              )}
              {recap.notable_events?.length > 0 && (
                <DrawerSection title="Notable events">
                  <ul className="flex flex-col gap-1 text-[12px] text-[var(--artemis-white)]">
                    {recap.notable_events.map((ev, idx) => (
                      <li key={idx} className="flex gap-2">
                        <span className="text-[var(--text-muted)]">·</span>
                        <span>{ev}</span>
                      </li>
                    ))}
                  </ul>
                </DrawerSection>
              )}
              {recap.failure_cause && (
                <DrawerSection title="Failure cause">
                  <p className="text-[12px] text-[var(--status-critical)]">
                    {recap.failure_cause}
                  </p>
                </DrawerSection>
              )}
            </>
          )}
        </div>
      )}

      {tab === "spans" && (
        <DrawerSection
          title={`Spans (${spans.length === 0 ? 0 : Math.min(spans.length, 20)}/${spans.length})`}
        >
          {spansQuery.isLoading && spans.length === 0 && (
            <p className="text-[11px] text-[var(--text-muted)]">
              Loading spans…
            </p>
          )}
          {spans.length === 0 && !spansQuery.isLoading && (
            <p className="text-[11px] text-[var(--text-muted)]">
              No spans recorded for this turn.
            </p>
          )}
          {spans.length > 0 && (
            <ul className="flex flex-col gap-1">
              {spans.slice(0, 20).map((span) => {
                const ms = spanDurationMs(span);
                const pct =
                  maxSpanMs > 0 ? Math.round((ms / maxSpanMs) * 100) : 0;
                return (
                  <li key={span.span_id}>
                    <button
                      type="button"
                      onClick={() =>
                        open({
                          kind: "span",
                          traceID: span.trace_id,
                          spanID: span.span_id,
                        })
                      }
                      className="flex w-full flex-col gap-1 rounded border border-transparent px-2 py-1 text-left transition hover:border-[var(--border)] hover:bg-[var(--bg-raised)]"
                    >
                      <span className="flex items-center justify-between gap-2">
                        <span className="truncate font-mono text-[11px] text-[var(--artemis-white)]">
                          {span.name}
                        </span>
                        <span className="shrink-0 font-mono text-[10px] text-[var(--text-muted)]">
                          {ms > 0 ? humanDuration(Math.round(ms)) : "—"}
                        </span>
                      </span>
                      <span className="relative block h-1 w-full rounded bg-[var(--bg-raised)]">
                        <span
                          className="absolute inset-y-0 left-0 rounded bg-[var(--accent)]"
                          style={{ width: `${pct}%` }}
                        />
                      </span>
                    </button>
                  </li>
                );
              })}
            </ul>
          )}
        </DrawerSection>
      )}

      {tab === "hitl" && (
        <DrawerSection title="HITL events">
          {hitlQuery.isLoading && hitlRows.length === 0 && (
            <p className="text-[11px] text-[var(--text-muted)]">
              Loading HITL events…
            </p>
          )}
          {hitlRows.length === 0 && !hitlQuery.isLoading && (
            <p className="text-[11px] text-[var(--text-muted)]">
              No HITL events recorded for this turn.
            </p>
          )}
          {hitlRows.length > 0 && (
            <ul className="flex flex-col gap-2">
              {hitlRows.map((h) => (
                <li
                  key={h.hitl_id}
                  className="flex flex-col gap-1 rounded border border-[var(--border)] px-2 py-1"
                >
                  <span className="font-mono text-[11px] text-[var(--artemis-white)]">
                    {h.kind} · {h.status}
                  </span>
                  <span className="text-[11px] text-[var(--text-muted)]">
                    {h.question || "—"}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </DrawerSection>
      )}

      <DrawerFooterAction
        href={`/turn/?sess=${sessionID}&turn=${turnID}`}
        label="Open full turn page"
      />
    </div>
  );
}
