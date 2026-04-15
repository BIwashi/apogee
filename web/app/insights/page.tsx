"use client";

import {
  Bar,
  BarChart,
  Cell,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import Card from "../components/Card";
import SectionHeader from "../components/SectionHeader";
import type { InsightsOverview, SessionSearchResponse } from "../lib/api-types";
import { useApi } from "../lib/swr";
import { timeAgo } from "../lib/time";

/**
 * `/insights` — aggregate analytics across all sessions the collector has
 * seen in the last 24 hours. Reads from `/v1/insights/overview`, with a
 * small list of recent sessions pulled in parallel so the page has
 * narrative content alongside the numeric tiles.
 */

function formatMs(ms: number): string {
  if (!ms || ms <= 0) return "—";
  if (ms < 1000) return `${Math.round(ms)}ms`;
  const seconds = ms / 1000;
  if (seconds < 60) return `${seconds.toFixed(1)}s`;
  const minutes = Math.floor(seconds / 60);
  const rem = Math.round(seconds % 60);
  return rem ? `${minutes}m${rem}s` : `${minutes}m`;
}

function formatPercent(r: number): string {
  if (!Number.isFinite(r)) return "—";
  return `${(r * 100).toFixed(1)}%`;
}

function shortId(id: string, len = 10): string {
  if (!id) return "—";
  if (id.length <= len) return id;
  return id.slice(0, len);
}

interface StatTileProps {
  label: string;
  value: string;
  sub?: string;
  tone?: "info" | "success" | "warning" | "critical";
}

function StatTile({ label, value, sub, tone }: StatTileProps) {
  const color = tone ? `var(--status-${tone})` : "var(--artemis-earth)";
  return (
    <Card>
      <p className="font-display text-[10px] tracking-[0.14em] text-[var(--text-muted)]">
        {label}
      </p>
      <p
        className="mt-2 font-display text-2xl tracking-[0.08em]"
        style={{ color }}
      >
        {value}
      </p>
      {sub && (
        <p className="mt-1 font-mono text-[10px] text-[var(--text-muted)]">
          {sub}
        </p>
      )}
    </Card>
  );
}

export default function InsightsPage() {
  const { data, error, isLoading } = useApi<InsightsOverview>(
    "/v1/insights/overview",
    { refreshInterval: 10_000 },
  );
  const recentSessions = useApi<SessionSearchResponse>(
    "/v1/sessions/search?q=&limit=20",
    { refreshInterval: 30_000 },
  );

  if (isLoading) {
    return (
      <div className="mx-auto flex max-w-6xl flex-col gap-6 pt-6">
        <h1 className="font-display-accent text-3xl tracking-[0.16em] text-[var(--artemis-white)]">
          INSIGHTS
        </h1>
        <Card>
          <p className="px-4 py-10 text-center text-[12px] text-[var(--text-muted)]">
            Loading insights…
          </p>
        </Card>
      </div>
    );
  }

  if (error || !data) {
    return (
      <div className="mx-auto flex max-w-6xl flex-col gap-6 pt-6">
        <h1 className="font-display-accent text-3xl tracking-[0.16em] text-[var(--artemis-white)]">
          INSIGHTS
        </h1>
        <Card>
          <p className="px-4 py-10 text-center text-[12px] text-[var(--text-muted)]">
            No data yet. Events will flow in once Claude Code starts reporting.
          </p>
        </Card>
      </div>
    );
  }

  const topTools = data.top_tools ?? [];
  const topPhases = data.top_phases ?? [];

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <header className="flex flex-wrap items-end justify-between gap-4 pt-6">
        <div>
          <h1 className="font-display-accent text-3xl tracking-[0.16em] text-[var(--artemis-white)]">
            INSIGHTS
          </h1>
          <div className="accent-gradient-bar mt-3 h-[3px] w-32 rounded-full" />
          <p className="mt-3 max-w-xl text-[13px] text-[var(--text-muted)]">
            Aggregate analytics across every session the collector has ingested.
            Window: last 24 hours.
          </p>
        </div>
      </header>

      <section className="grid gap-3 md:grid-cols-4">
        <StatTile label="SESSIONS" value={String(data.total_sessions ?? 0)} />
        <StatTile label="TURNS" value={String(data.total_turns ?? 0)} />
        <StatTile label="EVENTS" value={String(data.total_events ?? 0)} />
        <StatTile
          label="ERROR RATE · 24H"
          value={formatPercent(data.error_rate_last_24h ?? 0)}
          tone={
            data.error_rate_last_24h > 0.2
              ? "critical"
              : data.error_rate_last_24h > 0.05
                ? "warning"
                : "success"
          }
        />
      </section>

      <section className="grid gap-3 md:grid-cols-3">
        <StatTile
          label="P50 TURN DURATION"
          value={formatMs(data.p50_turn_duration_ms ?? 0)}
          tone="info"
        />
        <StatTile
          label="P95 TURN DURATION"
          value={formatMs(data.p95_turn_duration_ms ?? 0)}
          tone="warning"
        />
        <StatTile
          label="WATCHLIST SESSIONS"
          value={String(data.watchlist_sessions ?? 0)}
          tone={(data.watchlist_sessions ?? 0) > 0 ? "warning" : "success"}
        />
      </section>

      <section className="grid gap-4 md:grid-cols-2">
        <div>
          <SectionHeader
            title="Top tools"
            subtitle="Most frequently invoked tools (last 24h)."
          />
          <Card>
            {topTools.length === 0 ? (
              <p className="py-6 text-center text-[12px] text-[var(--text-muted)]">
                No tool calls recorded.
              </p>
            ) : (
              <div style={{ width: "100%", height: 260 }}>
                <ResponsiveContainer>
                  <BarChart
                    data={topTools}
                    layout="vertical"
                    margin={{ left: 12, right: 12, top: 4, bottom: 4 }}
                  >
                    <XAxis
                      type="number"
                      stroke="var(--text-muted)"
                      fontSize={10}
                    />
                    <YAxis
                      type="category"
                      dataKey="name"
                      stroke="var(--text-muted)"
                      fontSize={10}
                      width={90}
                    />
                    <Tooltip
                      cursor={{ fill: "var(--bg-raised)" }}
                      contentStyle={{
                        background: "var(--bg-surface)",
                        border: "1px solid var(--border)",
                        fontSize: 11,
                      }}
                    />
                    <Bar dataKey="count">
                      {topTools.map((_, idx) => (
                        <Cell key={`tool-${idx}`} fill="var(--artemis-earth)" />
                      ))}
                    </Bar>
                  </BarChart>
                </ResponsiveContainer>
              </div>
            )}
          </Card>
        </div>
        <div>
          <SectionHeader
            title="Top phases"
            subtitle="Phase bucket distribution across closed turns."
          />
          <Card>
            {topPhases.length === 0 ? (
              <p className="py-6 text-center text-[12px] text-[var(--text-muted)]">
                No phases recorded.
              </p>
            ) : (
              <div style={{ width: "100%", height: 260 }}>
                <ResponsiveContainer>
                  <BarChart
                    data={topPhases}
                    layout="vertical"
                    margin={{ left: 12, right: 12, top: 4, bottom: 4 }}
                  >
                    <XAxis
                      type="number"
                      stroke="var(--text-muted)"
                      fontSize={10}
                    />
                    <YAxis
                      type="category"
                      dataKey="name"
                      stroke="var(--text-muted)"
                      fontSize={10}
                      width={90}
                    />
                    <Tooltip
                      cursor={{ fill: "var(--bg-raised)" }}
                      contentStyle={{
                        background: "var(--bg-surface)",
                        border: "1px solid var(--border)",
                        fontSize: 11,
                      }}
                    />
                    <Bar dataKey="count">
                      {topPhases.map((_, idx) => (
                        <Cell key={`phase-${idx}`} fill="var(--accent)" />
                      ))}
                    </Bar>
                  </BarChart>
                </ResponsiveContainer>
              </div>
            )}
          </Card>
        </div>
      </section>

      <section>
        <SectionHeader
          title="Recent sessions"
          subtitle="The 20 most recently seen sessions."
        />
        <Card className="p-0">
          {(recentSessions.data?.sessions ?? []).length === 0 ? (
            <p className="px-4 py-10 text-center text-[12px] text-[var(--text-muted)]">
              No sessions yet.
            </p>
          ) : (
            <ul className="flex flex-col">
              {(recentSessions.data?.sessions ?? []).map((hit) => (
                <li
                  key={hit.session_id}
                  className="flex items-center justify-between gap-3 border-b border-[var(--border)] px-4 py-2 text-[12px] last:border-b-0"
                >
                  <div className="flex flex-1 flex-col gap-0.5">
                    <p className="font-mono text-[11px] text-[var(--artemis-white)]">
                      {shortId(hit.session_id, 12)}
                    </p>
                    <p className="line-clamp-1 text-[11px] text-[var(--text-muted)]">
                      {hit.latest_headline || hit.latest_prompt_snippet || "—"}
                    </p>
                  </div>
                  <span className="font-mono text-[10px] text-[var(--text-muted)]">
                    {hit.turn_count} turns · {timeAgo(hit.last_seen_at)}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </Card>
      </section>
    </div>
  );
}
