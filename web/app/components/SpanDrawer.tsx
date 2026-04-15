"use client";

import { useMemo, useState } from "react";
import { GitBranch } from "lucide-react";

import type { Span } from "../lib/api-types";
import { useApi } from "../lib/swr";
import { useDrawerState } from "../lib/drawer";
import { formatClock } from "../lib/time";
import DrawerFooterAction from "./DrawerFooterAction";
import DrawerHeader, { DrawerTabBar } from "./DrawerHeader";
import DrawerKeyValue, { DrawerSection } from "./DrawerKeyValue";

/**
 * SpanDrawer — low-level inspector for a single span. Fed by the new
 * `/v1/spans/:trace_id/:span_id/detail` endpoint (PR #36) which bundles the
 * span row plus its parent + direct children so the Parent tab can render a
 * click-through without a second network round-trip.
 *
 * Four tabs: Details / Attributes / Events / Parent. Clicking the parent or
 * a child calls useDrawerState().open() to swap the drawer content in place.
 */

interface SpanDrawerProps {
  traceID: string;
  spanID: string;
}

interface SpanDetail {
  span: Span;
  parent: Span | null;
  children: Span[];
}

type TabKey = "details" | "attributes" | "events" | "parent";

const TABS: ReadonlyArray<{ key: TabKey; label: string }> = [
  { key: "details", label: "Details" },
  { key: "attributes", label: "Attributes" },
  { key: "events", label: "Events" },
  { key: "parent", label: "Parent" },
];

function shortId(id: string, len = 12): string {
  if (!id) return "—";
  return id.length <= len ? id : id.slice(0, len);
}

function humanDuration(ns: number | undefined | null): string {
  if (!ns || ns <= 0) return "—";
  const ms = ns / 1_000_000;
  if (ms < 1) return "<1ms";
  if (ms < 1000) return `${Math.round(ms)}ms`;
  const seconds = ms / 1000;
  if (seconds < 60) return `${seconds.toFixed(1)}s`;
  return `${Math.round(seconds)}s`;
}

function renderScalar(value: unknown): string {
  if (value == null) return "—";
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

function AttributeTree({
  data,
  depth = 0,
}: {
  data: unknown;
  depth?: number;
}) {
  if (data === null || data === undefined) {
    return <span className="text-[var(--text-muted)]">—</span>;
  }
  if (Array.isArray(data)) {
    return (
      <ul className="flex flex-col gap-1">
        {data.map((item, idx) => (
          <li key={idx} className="flex gap-2">
            <span className="font-mono text-[10px] text-[var(--text-muted)]">
              [{idx}]
            </span>
            <AttributeTree data={item} depth={depth + 1} />
          </li>
        ))}
      </ul>
    );
  }
  if (typeof data === "object") {
    const entries = Object.entries(data as Record<string, unknown>);
    return (
      <ul className="flex flex-col gap-1" style={{ paddingLeft: depth * 8 }}>
        {entries.map(([key, value]) => (
          <li key={key} className="flex flex-col">
            <span className="font-display text-[10px] uppercase tracking-[0.12em] text-[var(--text-muted)]">
              {key}
            </span>
            <span className="font-mono text-[11px] text-[var(--artemis-white)]">
              {typeof value === "object" && value !== null ? (
                <AttributeTree data={value} depth={depth + 1} />
              ) : (
                renderScalar(value)
              )}
            </span>
          </li>
        ))}
      </ul>
    );
  }
  return (
    <span className="font-mono text-[11px] text-[var(--artemis-white)]">
      {renderScalar(data)}
    </span>
  );
}

export default function SpanDrawer({ traceID, spanID }: SpanDrawerProps) {
  const [tab, setTab] = useState<TabKey>("details");
  const { open } = useDrawerState();

  const detailQuery = useApi<SpanDetail>(
    traceID && spanID ? `/v1/spans/${traceID}/${spanID}/detail` : null,
  );
  const span = detailQuery.data?.span ?? null;
  const parent = detailQuery.data?.parent ?? null;
  const children = detailQuery.data?.children ?? [];

  const events = useMemo(() => {
    const raw = span?.events;
    if (!Array.isArray(raw)) return [] as Record<string, unknown>[];
    return raw.filter(
      (e): e is Record<string, unknown> =>
        typeof e === "object" && e !== null && !Array.isArray(e),
    );
  }, [span]);

  const onCopyJSON = () => {
    if (!span) return;
    if (typeof navigator === "undefined" || !navigator.clipboard) return;
    void navigator.clipboard.writeText(JSON.stringify(span, null, 2));
  };

  return (
    <div className="flex flex-col gap-4">
      <DrawerHeader
        icon={GitBranch}
        kind="Span"
        title={
          <span className="flex flex-col gap-1">
            <span className="truncate font-mono text-[13px] text-[var(--artemis-white)]">
              {span?.name || "—"}
            </span>
            <span className="font-mono text-[11px] text-[var(--text-muted)]">
              {shortId(spanID)}
            </span>
          </span>
        }
        subtitle={
          span?.status_code ? (
            <span>
              {span.status_code}
              {span.status_message ? ` · ${span.status_message}` : ""}
            </span>
          ) : null
        }
      />

      <DrawerTabBar<TabKey> tabs={TABS} active={tab} onChange={setTab} />

      {tab === "details" && (
        <div className="flex flex-col gap-4">
          <DrawerSection title="Identity">
            <DrawerKeyValue
              rows={[
                {
                  label: "trace_id",
                  value: traceID,
                  mono: true,
                  copyable: traceID,
                },
                {
                  label: "span_id",
                  value: spanID,
                  mono: true,
                  copyable: spanID,
                },
                {
                  label: "parent_span_id",
                  value: span?.parent_span_id || "—",
                  mono: true,
                  copyable: span?.parent_span_id || undefined,
                },
                { label: "kind", value: span?.kind || "—", mono: true },
                {
                  label: "status",
                  value: span?.status_code || "—",
                  mono: true,
                  tone:
                    span?.status_code === "ERROR" ? "critical" : "default",
                },
              ]}
            />
          </DrawerSection>

          <DrawerSection title="Timing">
            <DrawerKeyValue
              rows={[
                {
                  label: "start",
                  value: span?.start_time
                    ? formatClock(span.start_time)
                    : "—",
                  mono: true,
                },
                {
                  label: "end",
                  value: span?.end_time ? formatClock(span.end_time) : "—",
                  mono: true,
                },
                {
                  label: "duration",
                  value: humanDuration(span?.duration_ns),
                  mono: true,
                },
              ]}
            />
          </DrawerSection>

          {(span?.tool_name || span?.tool_use_id) && (
            <DrawerSection title="Tool">
              <DrawerKeyValue
                rows={[
                  {
                    label: "tool_name",
                    value: span?.tool_name || "—",
                    mono: true,
                  },
                  {
                    label: "tool_use_id",
                    value: span?.tool_use_id || "—",
                    mono: true,
                    copyable: span?.tool_use_id || undefined,
                  },
                ]}
              />
            </DrawerSection>
          )}
        </div>
      )}

      {tab === "attributes" && (
        <DrawerSection title="Attributes">
          {!span?.attributes || Object.keys(span.attributes).length === 0 ? (
            <p className="text-[11px] text-[var(--text-muted)]">
              No attributes recorded.
            </p>
          ) : (
            <AttributeTree data={span.attributes} />
          )}
        </DrawerSection>
      )}

      {tab === "events" && (
        <DrawerSection title="Events">
          {events.length === 0 ? (
            <p className="text-[11px] text-[var(--text-muted)]">
              No events recorded.
            </p>
          ) : (
            <ol className="flex flex-col gap-2">
              {events.map((ev, idx) => {
                const name = typeof ev.name === "string" ? ev.name : `event ${idx}`;
                const ts = typeof ev.timestamp === "string" ? ev.timestamp : null;
                return (
                  <li
                    key={idx}
                    className="flex flex-col gap-1 rounded border border-[var(--border)] px-2 py-1"
                  >
                    <span className="flex items-center justify-between gap-2">
                      <span className="font-mono text-[11px] text-[var(--artemis-white)]">
                        {name}
                      </span>
                      {ts ? (
                        <span className="font-mono text-[10px] text-[var(--text-muted)]">
                          {formatClock(ts)}
                        </span>
                      ) : null}
                    </span>
                    {ev.attributes && typeof ev.attributes === "object" ? (
                      <AttributeTree data={ev.attributes} />
                    ) : null}
                  </li>
                );
              })}
            </ol>
          )}
        </DrawerSection>
      )}

      {tab === "parent" && (
        <div className="flex flex-col gap-3">
          <DrawerSection title="Parent">
            {parent ? (
              <button
                type="button"
                onClick={() =>
                  open({
                    kind: "span",
                    traceID: parent.trace_id,
                    spanID: parent.span_id,
                  })
                }
                className="flex w-full flex-col gap-1 rounded border border-transparent px-2 py-1 text-left transition hover:border-[var(--border)] hover:bg-[var(--bg-raised)]"
              >
                <span className="truncate font-mono text-[11px] text-[var(--artemis-white)]">
                  {parent.name}
                </span>
                <span className="font-mono text-[10px] text-[var(--text-muted)]">
                  {shortId(parent.span_id)}
                </span>
              </button>
            ) : (
              <p className="text-[11px] text-[var(--text-muted)]">
                This span has no parent — it is the root of its trace.
              </p>
            )}
          </DrawerSection>

          <DrawerSection title={`Children (${children.length})`}>
            {children.length === 0 ? (
              <p className="text-[11px] text-[var(--text-muted)]">
                No direct children.
              </p>
            ) : (
              <ul className="flex flex-col gap-1">
                {children.map((child) => (
                  <li key={child.span_id}>
                    <button
                      type="button"
                      onClick={() =>
                        open({
                          kind: "span",
                          traceID: child.trace_id,
                          spanID: child.span_id,
                        })
                      }
                      className="flex w-full flex-col gap-0.5 rounded border border-transparent px-2 py-1 text-left transition hover:border-[var(--border)] hover:bg-[var(--bg-raised)]"
                    >
                      <span className="truncate font-mono text-[11px] text-[var(--artemis-white)]">
                        {child.name}
                      </span>
                      <span className="font-mono text-[10px] text-[var(--text-muted)]">
                        {shortId(child.span_id)} · {humanDuration(child.duration_ns)}
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </DrawerSection>
        </div>
      )}

      <DrawerFooterAction onClick={onCopyJSON} label="Copy span JSON" tone="muted" />
    </div>
  );
}
