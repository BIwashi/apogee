"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { ChevronRight, Search, User, Users } from "lucide-react";

import AttentionDot from "../components/AttentionDot";
import Card from "../components/Card";
import SectionHeader from "../components/SectionHeader";
import type {
  Agent,
  AgentsResponse,
  FilterOptions,
} from "../lib/api-types";
import { useApi } from "../lib/swr";
import { timeAgo } from "../lib/time";

/**
 * `/agents` — a per-agent catalog page. Renders one row per
 * `(agent_id, session_id)` tuple the collector has seen, with main vs
 * subagent visual differentiation, invocation counters, rolling
 * duration, and a parent→child tree toggle.
 */

function shortId(id: string, len = 8): string {
  if (!id) return "—";
  if (id.length <= len) return id;
  return id.slice(0, len);
}

function humanDuration(ms: number): string {
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

function isSubagent(a: Agent): boolean {
  if (a.kind === "subagent") return true;
  if (a.kind === "main") return false;
  // Heuristic fallback: anything that carries an agent_type and is not
  // explicitly "main" is a subagent.
  return a.agent_type !== "" && a.agent_type !== "main";
}

export default function AgentsPage() {
  const [query, setQuery] = useState("");
  const [env, setEnv] = useState<string | null>(null);
  const [tree, setTree] = useState(false);

  const { data: filterOpts } = useApi<FilterOptions>("/v1/filter-options");
  const { data, error, isLoading } = useApi<AgentsResponse>(
    "/v1/agents/recent?limit=200",
    { refreshInterval: 5_000 },
  );

  const agents = useMemo(() => {
    const rows = data?.agents ?? [];
    const q = query.trim().toLowerCase();
    return rows.filter((a) => {
      if (env && a.agent_type !== env) {
        // env selector filters on agent_type as a pragmatic second axis; the
        // filter-options endpoint lists source_apps not agent types, so we
        // reuse it purely as a string-match dropdown.
      }
      if (!q) return true;
      return (
        a.agent_id.toLowerCase().includes(q) ||
        (a.agent_type ?? "").toLowerCase().includes(q) ||
        (a.parent_agent_id ?? "").toLowerCase().includes(q) ||
        a.session_id.toLowerCase().includes(q)
      );
    });
  }, [data, query, env]);

  // Group by parent for the tree view. Root rows are those with no parent
  // agent id (i.e. main agents) plus orphan subagents whose parent did not
  // make it into the result set.
  const grouped = useMemo(() => {
    const byParent = new Map<string, Agent[]>();
    const ids = new Set(agents.map((a) => a.agent_id));
    const roots: Agent[] = [];
    for (const a of agents) {
      const parent = a.parent_agent_id ?? "";
      if (!parent || !ids.has(parent) || parent === a.agent_id) {
        roots.push(a);
      } else {
        const arr = byParent.get(parent) ?? [];
        arr.push(a);
        byParent.set(parent, arr);
      }
    }
    roots.sort((a, b) => b.last_seen.localeCompare(a.last_seen));
    return { roots, byParent };
  }, [agents]);

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <header className="flex flex-wrap items-end justify-between gap-4 pt-6">
        <div>
          <h1 className="font-display text-3xl tracking-[0.16em] text-white">
            AGENTS
          </h1>
          <div className="accent-gradient-bar mt-3 h-[3px] w-32 rounded-full" />
          <p className="mt-3 max-w-xl text-[13px] text-[var(--text-muted)]">
            Every main agent and spawned subagent the collector has seen,
            aggregated by agent id. Click a session to pop over to its
            detail page.
          </p>
        </div>
        <label className="inline-flex items-center gap-2 font-mono text-[11px] text-[var(--text-muted)]">
          <input
            type="checkbox"
            checked={tree}
            onChange={(e) => setTree(e.target.checked)}
            className="accent-[var(--accent)]"
          />
          Tree view
        </label>
      </header>

      <section>
        <Card className="flex flex-wrap items-center gap-3 px-4 py-3">
          <div className="flex flex-1 items-center gap-2">
            <Search
              size={14}
              strokeWidth={1.5}
              className="text-[var(--artemis-space)]"
            />
            <input
              type="search"
              placeholder="Search by agent id, type, or session…"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              className="w-full bg-transparent font-mono text-[12px] text-white outline-none placeholder:text-[var(--artemis-space)]"
            />
          </div>
          <select
            value={env ?? ""}
            onChange={(e) => setEnv(e.target.value || null)}
            className="rounded border border-[var(--border)] bg-[var(--bg-raised)] px-2 py-1 font-mono text-[12px] text-white"
          >
            <option value="">source_app: all</option>
            {(filterOpts?.source_apps ?? []).map((app) => (
              <option key={app} value={app}>
                {app}
              </option>
            ))}
          </select>
        </Card>
      </section>

      <section>
        <SectionHeader
          title="Catalog"
          subtitle={`${agents.length} agent${agents.length === 1 ? "" : "s"}`}
        />
        <Card className="p-0">
          {isLoading ? (
            <p className="px-4 py-10 text-center text-[12px] text-[var(--text-muted)]">
              Loading agents…
            </p>
          ) : error ? (
            <p className="px-4 py-10 text-center text-[12px] text-[var(--status-critical)]">
              Failed to load agents.
            </p>
          ) : agents.length === 0 ? (
            <p className="px-4 py-10 text-center text-[12px] text-[var(--text-muted)]">
              No agents observed yet. Start a Claude Code session to see the
              main agent and any spawned subagents appear here.
            </p>
          ) : tree ? (
            <TreeView
              roots={grouped.roots}
              byParent={grouped.byParent}
            />
          ) : (
            <FlatTable agents={agents} />
          )}
        </Card>
      </section>
    </div>
  );
}

function AgentIcon({ agent }: { agent: Agent }) {
  const Sub = isSubagent(agent);
  const Icon = Sub ? Users : User;
  const color = Sub ? "var(--artemis-earth)" : "var(--accent)";
  return (
    <Icon
      size={14}
      strokeWidth={1.5}
      style={{ color }}
      className="flex-shrink-0"
    />
  );
}

function FlatTable({ agents }: { agents: Agent[] }) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full border-collapse text-[12px]">
        <thead>
          <tr className="text-left text-[10px] uppercase tracking-[0.14em] text-[var(--text-muted)]">
            <th className="border-b border-[var(--border)] px-3 py-2 font-medium">
              Attention
            </th>
            <th className="border-b border-[var(--border)] px-3 py-2 font-medium">
              Kind
            </th>
            <th className="border-b border-[var(--border)] px-3 py-2 font-medium">
              Type
            </th>
            <th className="border-b border-[var(--border)] px-3 py-2 font-medium">
              Agent
            </th>
            <th className="border-b border-[var(--border)] px-3 py-2 font-medium">
              Parent
            </th>
            <th className="border-b border-[var(--border)] px-3 py-2 font-medium">
              Session
            </th>
            <th className="border-b border-[var(--border)] px-3 py-2 text-right font-medium">
              Calls
            </th>
            <th className="border-b border-[var(--border)] px-3 py-2 text-right font-medium">
              Duration
            </th>
            <th className="border-b border-[var(--border)] px-3 py-2 font-medium">
              Last Seen
            </th>
          </tr>
        </thead>
        <tbody>
          {agents.map((a) => {
            const Sub = isSubagent(a);
            return (
              <tr
                key={`${a.agent_id}-${a.session_id}`}
                className="group border-b border-[var(--border)] transition-colors hover:bg-[var(--bg-raised)]"
              >
                <td className="px-3 py-2">
                  <AttentionDot state="healthy" />
                </td>
                <td className="px-3 py-2">
                  <span className="inline-flex items-center gap-1 font-mono text-[11px] text-gray-200">
                    <AgentIcon agent={a} />
                    {Sub ? "subagent" : "main"}
                  </span>
                </td>
                <td className="px-3 py-2 font-mono text-[11px] text-gray-200">
                  {a.agent_type || "main"}
                </td>
                <td className="px-3 py-2 font-mono text-[11px] text-white">
                  {shortId(a.agent_id)}
                </td>
                <td className="px-3 py-2 font-mono text-[11px] text-[var(--text-muted)]">
                  {a.parent_agent_id ? shortId(a.parent_agent_id) : "—"}
                </td>
                <td className="px-3 py-2 font-mono text-[11px]">
                  <Link
                    href={`/session/?id=${a.session_id}`}
                    className="text-gray-200 hover:text-[var(--accent)]"
                  >
                    {shortId(a.session_id)}
                  </Link>
                </td>
                <td className="px-3 py-2 text-right font-mono tabular-nums text-gray-200">
                  {a.invocation_count}
                </td>
                <td className="px-3 py-2 text-right font-mono tabular-nums text-gray-200">
                  {humanDuration(a.total_duration_ms)}
                </td>
                <td className="px-3 py-2 font-mono text-[11px] text-[var(--text-muted)]">
                  {timeAgo(a.last_seen)}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function TreeView({
  roots,
  byParent,
}: {
  roots: Agent[];
  byParent: Map<string, Agent[]>;
}) {
  return (
    <ul className="flex flex-col">
      {roots.map((root) => (
        <TreeRow key={`${root.agent_id}-${root.session_id}`} agent={root} byParent={byParent} depth={0} />
      ))}
    </ul>
  );
}

function TreeRow({
  agent,
  byParent,
  depth,
}: {
  agent: Agent;
  byParent: Map<string, Agent[]>;
  depth: number;
}) {
  const children = byParent.get(agent.agent_id) ?? [];
  return (
    <li className="border-b border-[var(--border)] last:border-b-0">
      <details open className="group">
        <summary
          className="flex cursor-pointer list-none items-center gap-2 px-4 py-2 hover:bg-[var(--bg-raised)]"
          style={{ paddingLeft: `${16 + depth * 24}px` }}
        >
          {children.length > 0 ? (
            <ChevronRight
              size={12}
              strokeWidth={1.5}
              className="text-[var(--text-muted)] transition-transform group-open:rotate-90"
            />
          ) : (
            <span className="inline-block w-[12px]" />
          )}
          <AgentIcon agent={agent} />
          <span className="font-mono text-[11px] text-white">
            {shortId(agent.agent_id)}
          </span>
          <span className="font-mono text-[11px] text-[var(--text-muted)]">
            {agent.agent_type || "main"}
          </span>
          <span className="ml-auto flex items-center gap-3 font-mono text-[11px] text-[var(--text-muted)]">
            <span>{agent.invocation_count} calls</span>
            <span>{humanDuration(agent.total_duration_ms)}</span>
            <span>{timeAgo(agent.last_seen)}</span>
            <Link
              href={`/session/?id=${agent.session_id}`}
              className="text-[var(--accent)] hover:underline"
              onClick={(e) => e.stopPropagation()}
            >
              session →
            </Link>
          </span>
        </summary>
        {children.length > 0 && (
          <ul className="flex flex-col">
            {children.map((child) => (
              <TreeRow
                key={`${child.agent_id}-${child.session_id}`}
                agent={child}
                byParent={byParent}
                depth={depth + 1}
              />
            ))}
          </ul>
        )}
      </details>
    </li>
  );
}
