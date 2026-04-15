"use client";

import { useMemo, useState } from "react";
import { Users } from "lucide-react";
import type { Agent, Turn } from "../lib/api-types";
import { useDrawerState } from "../lib/drawer";
import { useApi } from "../lib/swr";
import { formatClock, timeAgo } from "../lib/time";
import DrawerFooterAction from "./DrawerFooterAction";
import DrawerHeader, { DrawerTabBar } from "./DrawerHeader";
import DrawerKeyValue, { DrawerSection } from "./DrawerKeyValue";
import SessionLabel from "./SessionLabel";

/**
 * AgentDrawer — Datadog-style detail for a single main agent or subagent.
 *
 * Fed by the new `/v1/agents/:id/detail` endpoint (PR #36) which returns the
 * full Agent row along with the last 20 turns the agent participated in, a
 * tool-usage histogram, and its parent + children pointers so the Parent
 * tree tab can render a compact ancestor/descendant view without a second
 * round-trip.
 */

interface AgentDrawerProps {
  agentID: string;
}

export interface AgentDetail {
  agent: Agent;
  parent: Agent | null;
  children: Agent[];
  turns: Turn[];
  tool_counts: { name: string; count: number }[];
}

type TabKey = "details" | "turns" | "tools" | "tree";

const TABS: ReadonlyArray<{ key: TabKey; label: string }> = [
  { key: "details", label: "Details" },
  { key: "turns", label: "Turns" },
  { key: "tools", label: "Tools" },
  { key: "tree", label: "Parent tree" },
];

function shortId(id: string, len = 8): string {
  if (!id) return "—";
  return id.length <= len ? id : id.slice(0, len);
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

export default function AgentDrawer({ agentID }: AgentDrawerProps) {
  const [tab, setTab] = useState<TabKey>("details");
  const { open } = useDrawerState();

  const detailQuery = useApi<AgentDetail>(
    agentID ? `/v1/agents/${encodeURIComponent(agentID)}/detail` : null,
  );

  const detail = detailQuery.data ?? null;
  const agent = detail?.agent ?? null;
  const turns = useMemo(() => detail?.turns ?? [], [detail]);
  const toolCounts = useMemo(() => detail?.tool_counts ?? [], [detail]);
  const parent = detail?.parent ?? null;
  const children = useMemo(() => detail?.children ?? [], [detail]);
  const maxToolCount = useMemo(
    () => toolCounts.reduce((acc, row) => Math.max(acc, row.count), 0),
    [toolCounts],
  );

  const kindLabel = agent?.kind === "main" ? "Main agent" : "Subagent";
  const title = agent?.agent_type || "main";

  return (
    <div className="flex flex-col gap-4">
      <DrawerHeader
        icon={Users}
        kind={kindLabel}
        title={
          <span className="flex flex-col gap-1">
            <span className="font-display text-[16px] text-[var(--artemis-white)]">
              {title}
            </span>
            <span className="font-mono text-[11px] text-[var(--text-muted)]">
              {shortId(agentID)}
            </span>
          </span>
        }
        subtitle={
          agent?.last_seen ? (
            <>
              last seen {formatClock(agent.last_seen)} ·{" "}
              {timeAgo(agent.last_seen)}
            </>
          ) : null
        }
      />

      <DrawerTabBar<TabKey> tabs={TABS} active={tab} onChange={setTab} />

      {tab === "details" && (
        <div className="flex flex-col gap-4">
          {detailQuery.isLoading && !detail && (
            <p className="text-[11px] text-[var(--text-muted)]">
              Loading agent…
            </p>
          )}
          {agent && (
            <>
              <DrawerSection title="Identity">
                <DrawerKeyValue
                  rows={[
                    {
                      label: "agent_id",
                      value: agentID,
                      mono: true,
                      copyable: agentID,
                    },
                    {
                      label: "kind",
                      value: agent.kind || "—",
                      mono: true,
                    },
                    {
                      label: "agent_type",
                      value: agent.agent_type || "main",
                      mono: true,
                    },
                    {
                      label: "parent_agent_id",
                      value: parent ? (
                        <button
                          type="button"
                          onClick={() =>
                            open({ kind: "agent", id: parent.agent_id })
                          }
                          className="font-mono text-[11px] text-[var(--accent)] hover:underline"
                        >
                          {shortId(parent.agent_id)}
                        </button>
                      ) : (
                        agent.parent_agent_id || "—"
                      ),
                      mono: !parent,
                    },
                  ]}
                />
              </DrawerSection>

              <DrawerSection title="Session">
                <SessionLabel sessionID={agent.session_id} />
              </DrawerSection>

              <DrawerSection title="Activity">
                <DrawerKeyValue
                  rows={[
                    {
                      label: "invocations",
                      value: agent.invocation_count,
                      mono: true,
                    },
                    {
                      label: "total duration",
                      value: humanDuration(agent.total_duration_ms),
                      mono: true,
                    },
                    {
                      label: "last seen",
                      value: agent.last_seen
                        ? `${formatClock(agent.last_seen)} · ${timeAgo(agent.last_seen)}`
                        : "—",
                      mono: true,
                    },
                  ]}
                />
              </DrawerSection>
            </>
          )}
        </div>
      )}

      {tab === "turns" && (
        <DrawerSection title={`Turns (${turns.length})`}>
          {turns.length === 0 ? (
            <p className="text-[11px] text-[var(--text-muted)]">
              No turns recorded for this agent yet.
            </p>
          ) : (
            <ul className="flex flex-col gap-1">
              {turns.slice(0, 20).map((t) => (
                <li key={t.turn_id}>
                  <button
                    type="button"
                    onClick={() =>
                      open({
                        kind: "turn",
                        sess: t.session_id,
                        turn: t.turn_id,
                      })
                    }
                    className="flex w-full items-start justify-between gap-3 rounded border border-transparent px-2 py-1 text-left transition hover:border-[var(--border)] hover:bg-[var(--bg-raised)]"
                  >
                    <span className="flex min-w-0 flex-col gap-0.5">
                      <span className="font-mono text-[11px] text-[var(--artemis-white)]">
                        {shortId(t.turn_id)} · {t.status}
                      </span>
                      <span className="truncate text-[11px] text-[var(--text-muted)]">
                        {t.headline || t.prompt_text?.slice(0, 80) || "—"}
                      </span>
                    </span>
                    <span className="shrink-0 font-mono text-[10px] text-[var(--text-muted)]">
                      {formatClock(t.started_at)}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </DrawerSection>
      )}

      {tab === "tools" && (
        <DrawerSection title="Tool usage">
          {toolCounts.length === 0 ? (
            <p className="text-[11px] text-[var(--text-muted)]">
              No tool invocations recorded for this agent.
            </p>
          ) : (
            <ul className="flex flex-col gap-1">
              {toolCounts.slice(0, 20).map((row) => {
                const pct =
                  maxToolCount > 0
                    ? Math.round((row.count / maxToolCount) * 100)
                    : 0;
                return (
                  <li
                    key={row.name}
                    className="flex items-center gap-2 text-[11px]"
                  >
                    <span className="w-24 shrink-0 truncate font-mono text-[var(--artemis-white)]">
                      {row.name}
                    </span>
                    <span className="relative h-2 flex-1 rounded bg-[var(--bg-raised)]">
                      <span
                        className="absolute inset-y-0 left-0 rounded bg-[var(--accent)]"
                        style={{ width: `${pct}%` }}
                      />
                    </span>
                    <span className="w-8 shrink-0 text-right font-mono text-[10px] text-[var(--text-muted)] tabular-nums">
                      {row.count}
                    </span>
                  </li>
                );
              })}
            </ul>
          )}
        </DrawerSection>
      )}

      {tab === "tree" && (
        <div className="flex flex-col gap-3">
          <DrawerSection title="Parent">
            {parent ? (
              <button
                type="button"
                onClick={() => open({ kind: "agent", id: parent.agent_id })}
                className="flex w-full flex-col gap-0.5 rounded border border-transparent px-2 py-1 text-left transition hover:border-[var(--border)] hover:bg-[var(--bg-raised)]"
              >
                <span className="font-mono text-[11px] text-[var(--artemis-white)]">
                  {parent.agent_type || "main"}
                </span>
                <span className="font-mono text-[10px] text-[var(--text-muted)]">
                  {shortId(parent.agent_id)}
                </span>
              </button>
            ) : (
              <p className="text-[11px] text-[var(--text-muted)]">
                This agent has no parent.
              </p>
            )}
          </DrawerSection>

          <DrawerSection title={`Children (${children.length})`}>
            {children.length === 0 ? (
              <p className="text-[11px] text-[var(--text-muted)]">
                No subagents spawned from this agent.
              </p>
            ) : (
              <ul className="flex flex-col gap-1">
                {children.map((child) => (
                  <li key={`${child.agent_id}-${child.session_id}`}>
                    <button
                      type="button"
                      onClick={() =>
                        open({ kind: "agent", id: child.agent_id })
                      }
                      className="flex w-full items-center justify-between gap-2 rounded border border-transparent px-2 py-1 text-left transition hover:border-[var(--border)] hover:bg-[var(--bg-raised)]"
                    >
                      <span className="flex flex-col gap-0.5">
                        <span className="font-mono text-[11px] text-[var(--artemis-white)]">
                          {child.agent_type || "main"}
                        </span>
                        <span className="font-mono text-[10px] text-[var(--text-muted)]">
                          {shortId(child.agent_id)}
                        </span>
                      </span>
                      <span className="font-mono text-[10px] text-[var(--text-muted)]">
                        {child.invocation_count} calls
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </DrawerSection>
        </div>
      )}

      <DrawerFooterAction
        href={`/agents?id=${encodeURIComponent(agentID)}`}
        label="Open agents page"
        tone="muted"
      />
    </div>
  );
}
