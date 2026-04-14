"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { Search } from "lucide-react";

import AttentionDot from "../components/AttentionDot";
import Card from "../components/Card";
import SectionHeader from "../components/SectionHeader";
import type {
  FilterOptions,
  SessionSearchHit,
  SessionSearchResponse,
} from "../lib/api-types";
import { useApi } from "../lib/swr";
import { timeAgo } from "../lib/time";
import { useSelection } from "../lib/url-state";

/**
 * `/sessions` — the Datadog "Service Catalog" equivalent. A searchable,
 * filterable table of every session the collector has seen. Clicking a row
 * promotes the session into the global selection (via the URL state) and
 * navigates into the tabbed session detail.
 */

function shortId(id: string, len = 12): string {
  if (!id) return "—";
  if (id.length <= len) return id;
  return id.slice(0, len);
}

export default function SessionsPage() {
  const { setSelection } = useSelection();
  const [query, setQuery] = useState("");
  const [env, setEnv] = useState<string | null>(null);

  const { data: filterOpts } = useApi<FilterOptions>("/v1/filter-options");
  const params = new URLSearchParams();
  if (query) params.set("q", query);
  params.set("limit", "200");
  const { data, error, isLoading } = useApi<SessionSearchResponse>(
    `/v1/sessions/search?${params.toString()}`,
    { refreshInterval: 5_000 },
  );

  const sessions: SessionSearchHit[] = useMemo(() => {
    const rows = data?.sessions ?? [];
    if (!env) return rows;
    return rows.filter((r) => r.source_app === env);
  }, [data, env]);

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <header className="flex flex-wrap items-end justify-between gap-4 pt-6">
        <div>
          <h1 className="font-display text-3xl tracking-[0.16em] text-white">SESSIONS</h1>
          <div className="accent-gradient-bar mt-3 h-[3px] w-32 rounded-full" />
          <p className="mt-3 max-w-xl text-[13px] text-[var(--text-muted)]">
            Every Claude Code session reporting to this collector. Click a row to drill
            into its tabbed detail page.
          </p>
        </div>
      </header>

      <section>
        <Card className="flex flex-wrap items-center gap-3 px-4 py-3">
          <div className="flex flex-1 items-center gap-2">
            <Search size={14} strokeWidth={1.5} className="text-[var(--artemis-space)]" />
            <input
              type="search"
              placeholder="Search by id, source_app, or prompt…"
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
            <option value="">env: all</option>
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
          subtitle={`${sessions.length} match${sessions.length === 1 ? "" : "es"}`}
        />
        <Card className="p-0">
          {isLoading ? (
            <p className="px-4 py-10 text-center text-[12px] text-[var(--text-muted)]">
              Loading sessions…
            </p>
          ) : error ? (
            <p className="px-4 py-10 text-center text-[12px] text-[var(--status-critical)]">
              Failed to load sessions.
            </p>
          ) : sessions.length === 0 ? (
            <p className="px-4 py-10 text-center text-[12px] text-[var(--text-muted)]">
              No matching sessions.
            </p>
          ) : (
            <table className="w-full border-collapse text-[12px]">
              <thead>
                <tr className="text-left text-[10px] uppercase tracking-[0.14em] text-[var(--text-muted)]">
                  <th className="border-b border-[var(--border)] px-3 py-2 font-medium">Attention</th>
                  <th className="border-b border-[var(--border)] px-3 py-2 font-medium">Session</th>
                  <th className="border-b border-[var(--border)] px-3 py-2 font-medium">Source App</th>
                  <th className="border-b border-[var(--border)] px-3 py-2 font-medium">Headline</th>
                  <th className="border-b border-[var(--border)] px-3 py-2 font-medium">Last Seen</th>
                  <th className="border-b border-[var(--border)] px-3 py-2 text-right font-medium">Turns</th>
                  <th className="border-b border-[var(--border)] px-3 py-2 font-medium"></th>
                </tr>
              </thead>
              <tbody>
                {sessions.map((hit) => (
                  <tr
                    key={hit.session_id}
                    className="group cursor-pointer border-b border-[var(--border)] transition-colors hover:bg-[var(--bg-raised)]"
                    onClick={() => {
                      setSelection({
                        sess: hit.session_id,
                        env: hit.source_app || null,
                      });
                    }}
                  >
                    <td className="px-3 py-2">
                      <AttentionDot state={hit.attention_state} />
                    </td>
                    <td className="px-3 py-2 font-mono text-[11px] text-gray-200">
                      <Link
                        href={`/session/?id=${hit.session_id}&tab=overview`}
                        className="hover:text-[var(--accent)]"
                      >
                        {shortId(hit.session_id)}
                      </Link>
                    </td>
                    <td className="px-3 py-2 text-gray-200">
                      {hit.source_app || "—"}
                    </td>
                    <td className="px-3 py-2 text-[11px] text-[var(--text-muted)]">
                      <span className="line-clamp-1">
                        {hit.latest_headline || hit.latest_prompt_snippet || "—"}
                      </span>
                    </td>
                    <td className="px-3 py-2 font-mono text-[11px] text-[var(--text-muted)]">
                      {timeAgo(hit.last_seen_at)}
                    </td>
                    <td className="px-3 py-2 text-right font-mono tabular-nums text-gray-200">
                      {hit.turn_count}
                    </td>
                    <td className="px-3 py-2 text-right">
                      <Link
                        href={`/session/?id=${hit.session_id}&tab=overview`}
                        className="font-mono text-[11px] text-[var(--accent)] hover:underline"
                      >
                        detail →
                      </Link>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </Card>
      </section>
    </div>
  );
}
