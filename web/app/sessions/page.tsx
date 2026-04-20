"use client";

import { useMemo, useState } from "react";
import { Search } from "lucide-react";
import AttentionDot from "../components/AttentionDot";
import Card from "../components/Card";
import SectionHeader from "../components/SectionHeader";
import SessionLabel from "../components/SessionLabel";
import type {
  FilterOptions,
  SessionSearchHit,
  SessionSearchResponse,
} from "../lib/api-types";
import { drawerLinkProps, useDrawerState } from "../lib/drawer";
import { useApi } from "../lib/swr";
import { timeAgo } from "../lib/time";
import { buildQuery, useSelection } from "../lib/url-state";

/**
 * `/sessions` — the Datadog "Service Catalog" equivalent. A searchable,
 * filterable table of every session the collector has seen. Clicking a row
 * promotes the session into the global selection (via the URL state) and
 * navigates into the tabbed session detail.
 */

export default function SessionsPage() {
  const { setSelection, apiParams } = useSelection();
  const { open } = useDrawerState();
  const [query, setQuery] = useState("");
  const [env, setEnv] = useState<string | null>(null);

  const { data: filterOpts } = useApi<FilterOptions>("/v1/filter-options");
  // Thread the top-ribbon scope (source_app + since/until) into the
  // search query so "Last 15m" + env filter actually constrain the
  // catalog. The local env dropdown takes precedence over the ribbon
  // env when set, so users can browse a different source_app without
  // clearing the global filter.
  const queryString = useMemo(() => {
    const extras: Record<string, string | number | null | undefined> = {
      limit: 200,
    };
    if (query) extras.q = query;
    if (env) extras.source_app = env;
    return buildQuery(apiParams, extras);
  }, [apiParams, query, env]);
  const { data, error, isLoading } = useApi<SessionSearchResponse>(
    `/v1/sessions/search${queryString}`,
    { refreshInterval: 5_000 },
  );

  const sessions: SessionSearchHit[] = useMemo(
    () => data?.sessions ?? [],
    [data],
  );

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <header className="flex flex-wrap items-end justify-between gap-4 pt-6">
        <div>
          <h1 className="font-display-accent text-3xl tracking-[0.16em] text-[var(--artemis-white)]">
            SESSIONS
          </h1>
          <div className="accent-gradient-bar mt-3 h-[3px] w-32 rounded-full" />
          <p className="mt-3 max-w-xl text-[13px] text-[var(--text-muted)]">
            Every Claude Code session reporting to this collector. Click a row
            to drill into its tabbed detail page.
          </p>
        </div>
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
              placeholder="Search by id, source_app, or prompt…"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              className="w-full bg-transparent font-mono text-[12px] text-[var(--artemis-white)] outline-none placeholder:text-[var(--artemis-space)]"
            />
          </div>
          <select
            value={env ?? ""}
            onChange={(e) => setEnv(e.target.value || null)}
            className="rounded border border-[var(--border)] bg-[var(--bg-raised)] px-2 py-1 font-mono text-[12px] text-[var(--artemis-white)]"
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
            <table className="w-full table-fixed border-collapse text-[12px]">
              <thead>
                <tr className="text-left text-[10px] uppercase tracking-[0.14em] text-[var(--text-muted)]">
                  <th className="w-[140px] border-b border-[var(--border)] px-3 py-2 font-medium">
                    Attention
                  </th>
                  <th className="w-[20%] border-b border-[var(--border)] px-3 py-2 font-medium">
                    Session
                  </th>
                  <th className="w-[11%] border-b border-[var(--border)] px-3 py-2 font-medium">
                    Source App
                  </th>
                  <th className="border-b border-[var(--border)] px-3 py-2 font-medium">
                    Headline
                  </th>
                  <th className="w-[90px] border-b border-[var(--border)] px-3 py-2 font-medium">
                    Last Seen
                  </th>
                  <th className="w-[64px] border-b border-[var(--border)] px-3 py-2 text-right font-medium">
                    Turns
                  </th>
                  <th className="w-[72px] border-b border-[var(--border)] px-3 py-2 font-medium"></th>
                </tr>
              </thead>
              <tbody>
                {sessions.map((hit) => {
                  const rowProps = drawerLinkProps(
                    `/session/?id=${hit.session_id}&tab=overview`,
                    () => {
                      setSelection({
                        sess: hit.session_id,
                        env: hit.source_app || null,
                      });
                      open({ kind: "session", id: hit.session_id });
                    },
                  );
                  return (
                    <tr
                      key={hit.session_id}
                      onClick={(e) =>
                        rowProps.onClick(
                          e as unknown as React.MouseEvent<HTMLElement>,
                        )
                      }
                      className="group cursor-pointer border-b border-[var(--border)] transition-colors hover:bg-[var(--bg-raised)]"
                    >
                      <td className="overflow-hidden px-3 py-2">
                        <AttentionDot state={hit.attention_state} />
                      </td>
                      <td
                        className="max-w-0 overflow-hidden px-3 py-2"
                        onClick={(e) => e.stopPropagation()}
                      >
                        <SessionLabel
                          sessionID={hit.session_id}
                          sourceApp={hit.source_app || null}
                          headline={
                            hit.latest_headline ||
                            hit.latest_prompt_snippet ||
                            null
                          }
                          size="md"
                        />
                      </td>
                      <td className="max-w-0 overflow-hidden truncate px-3 py-2 text-[var(--artemis-white)]">
                        {hit.source_app || "—"}
                      </td>
                      <td className="max-w-0 overflow-hidden truncate px-3 py-2 text-[11px] text-[var(--text-muted)]">
                        {hit.latest_headline ||
                          hit.latest_prompt_snippet ||
                          "—"}
                      </td>
                      <td className="px-3 py-2 font-mono text-[11px] text-[var(--text-muted)]">
                        {timeAgo(hit.last_seen_at)}
                      </td>
                      <td className="px-3 py-2 text-right font-mono tabular-nums text-[var(--artemis-white)]">
                        {hit.turn_count}
                      </td>
                      <td className="px-3 py-2 text-right">
                        <a
                          href={`/session/?id=${hit.session_id}&tab=overview`}
                          className="font-mono text-[11px] text-[var(--accent)] hover:underline"
                          onClick={(e) => e.stopPropagation()}
                        >
                          detail →
                        </a>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
        </Card>
      </section>
    </div>
  );
}
