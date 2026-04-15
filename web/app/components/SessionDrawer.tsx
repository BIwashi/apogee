"use client";

import { useMemo, useState } from "react";
import { Layers } from "lucide-react";

import type {
  RollupResponse,
  SessionSummary,
  SessionTurnsResponse,
  Turn,
} from "../lib/api-types";
import { useApi } from "../lib/swr";
import { useDrawerState } from "../lib/drawer";
import { formatClock, timeAgo } from "../lib/time";
import DrawerFooterAction from "./DrawerFooterAction";
import DrawerHeader, { DrawerTabBar } from "./DrawerHeader";
import DrawerKeyValue, { DrawerSection } from "./DrawerKeyValue";

/**
 * SessionDrawer — the cross-cutting side drawer for a single session. PR #36.
 *
 * Fed by three existing endpoints:
 *   - `/v1/sessions/:id/summary`  → SessionSummary
 *   - `/v1/sessions/:id/turns`    → the last N turns
 *   - `/v1/sessions/:id/rollup`   → tier-2 narrative (optional)
 *
 * Three tabs (Overview / Turns / Rollup) mirror the full `/session` page's
 * hero sections without dragging in the heavy swim-lane / span tree. Turn
 * rows inside the Turns tab are wired to re-open the drawer as a TurnDrawer
 * (`open({ kind: "turn", ... })`) so operators can navigate inside the
 * drawer without ever leaving the current route.
 */

interface SessionDrawerProps {
  sessionID: string;
}

type TabKey = "overview" | "turns" | "rollup";

const TABS: ReadonlyArray<{ key: TabKey; label: string }> = [
  { key: "overview", label: "Overview" },
  { key: "turns", label: "Turns" },
  { key: "rollup", label: "Rollup" },
];

function shortId(id: string, len = 8): string {
  if (!id) return "—";
  return id.length <= len ? id : id.slice(0, len);
}

export default function SessionDrawer({ sessionID }: SessionDrawerProps) {
  const [tab, setTab] = useState<TabKey>("overview");
  const { open } = useDrawerState();

  const summaryQuery = useApi<SessionSummary>(
    sessionID ? `/v1/sessions/${sessionID}/summary` : null,
    { refreshInterval: 5_000 },
  );
  const turnsQuery = useApi<SessionTurnsResponse>(
    sessionID ? `/v1/sessions/${sessionID}/turns` : null,
    { refreshInterval: 5_000 },
  );
  const rollupQuery = useApi<RollupResponse>(
    sessionID && tab === "rollup" ? `/v1/sessions/${sessionID}/rollup` : null,
  );

  const summary = summaryQuery.data ?? null;
  const turns: Turn[] = useMemo(
    () =>
      (turnsQuery.data?.turns ?? []).slice().sort((a, b) =>
        b.started_at.localeCompare(a.started_at),
      ),
    [turnsQuery.data],
  );

  const headline = summary?.latest_headline || turns[0]?.headline || "";
  const sourcePill = summary?.source_app || "—";

  return (
    <div className="flex flex-col gap-4">
      <DrawerHeader
        icon={Layers}
        kind="Session"
        title={
          <span className="flex flex-col gap-1">
            <span className="font-mono text-[13px] text-[var(--artemis-white)]">
              {shortId(sessionID, 12)}
            </span>
            {headline ? (
              <span className="text-[13px] font-normal text-[var(--text-muted)]">
                {headline}
              </span>
            ) : null}
          </span>
        }
        subtitle={
          <span className="flex items-center gap-2">
            <span>{sourcePill}</span>
            {summary?.last_seen_at ? (
              <>
                <span aria-hidden>·</span>
                <span>last seen {timeAgo(summary.last_seen_at)}</span>
              </>
            ) : null}
          </span>
        }
      />

      <DrawerTabBar<TabKey> tabs={TABS} active={tab} onChange={setTab} />

      {tab === "overview" && (
        <div className="flex flex-col gap-4">
          <DrawerSection title="Identity">
            <DrawerKeyValue
              rows={[
                {
                  label: "session_id",
                  value: sessionID,
                  mono: true,
                  copyable: sessionID,
                },
                {
                  label: "source_app",
                  value: summary?.source_app || "—",
                  mono: true,
                },
                {
                  label: "model",
                  value: summary?.model || "—",
                  mono: true,
                },
                {
                  label: "machine_id",
                  value: summary?.machine_id || "—",
                  mono: true,
                },
              ]}
            />
          </DrawerSection>

          <DrawerSection title="Timeline">
            <DrawerKeyValue
              rows={[
                {
                  label: "started",
                  value: summary?.started_at
                    ? `${formatClock(summary.started_at)} · ${timeAgo(summary.started_at)}`
                    : "—",
                  mono: true,
                },
                {
                  label: "last seen",
                  value: summary?.last_seen_at
                    ? `${formatClock(summary.last_seen_at)} · ${timeAgo(summary.last_seen_at)}`
                    : "—",
                  mono: true,
                },
                {
                  label: "ended",
                  value: summary?.ended_at
                    ? formatClock(summary.ended_at)
                    : "—",
                  mono: true,
                },
              ]}
            />
          </DrawerSection>

          <DrawerSection title="Turn breakdown">
            <DrawerKeyValue
              rows={[
                {
                  label: "total",
                  value: summary?.turn_count ?? turns.length,
                  mono: true,
                },
                {
                  label: "running",
                  value: summary?.running_count ?? 0,
                  mono: true,
                  tone: "warning",
                },
                {
                  label: "completed",
                  value: summary?.completed_count ?? 0,
                  mono: true,
                  tone: "success",
                },
                {
                  label: "errored",
                  value: summary?.errored_count ?? 0,
                  mono: true,
                  tone: "critical",
                },
              ]}
            />
          </DrawerSection>

          <DrawerSection title="Latest turn">
            {turns[0] ? (
              <button
                type="button"
                onClick={() =>
                  open({
                    kind: "turn",
                    sess: sessionID,
                    turn: turns[0].turn_id,
                  })
                }
                className="flex w-full flex-col gap-1 rounded border border-transparent px-2 py-2 text-left transition hover:border-[var(--border)] hover:bg-[var(--bg-raised)]"
              >
                <span className="font-mono text-[11px] text-[var(--artemis-white)]">
                  {shortId(turns[0].turn_id)} ·{" "}
                  {turns[0].status}
                </span>
                <span className="truncate text-[11px] text-[var(--text-muted)]">
                  {turns[0].headline ||
                    turns[0].prompt_text?.slice(0, 80) ||
                    "—"}
                </span>
              </button>
            ) : (
              <p className="text-[11px] text-[var(--text-muted)]">
                No turns recorded.
              </p>
            )}
          </DrawerSection>
        </div>
      )}

      {tab === "turns" && (
        <DrawerSection title={`Turns (${Math.min(turns.length, 20)})`}>
          {turns.length === 0 ? (
            <p className="text-[11px] text-[var(--text-muted)]">
              No turns recorded.
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
                        sess: sessionID,
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
                        {t.headline ||
                          t.prompt_text?.slice(0, 80) ||
                          "—"}
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

      {tab === "rollup" && (
        <div className="flex flex-col gap-3">
          {rollupQuery.isLoading && (
            <p className="text-[11px] text-[var(--text-muted)]">
              Loading rollup…
            </p>
          )}
          {!rollupQuery.isLoading && !rollupQuery.data?.rollup && (
            <p className="text-[11px] text-[var(--text-muted)]">
              No rollup has been generated yet for this session.
            </p>
          )}
          {rollupQuery.data?.rollup && (
            <>
              <DrawerSection title="Headline">
                <p className="text-[13px] text-[var(--artemis-white)]">
                  {rollupQuery.data.rollup.headline || "—"}
                </p>
              </DrawerSection>
              {rollupQuery.data.rollup.narrative && (
                <DrawerSection title="Narrative">
                  <p className="whitespace-pre-line text-[12px] leading-relaxed text-[var(--artemis-white)]">
                    {rollupQuery.data.rollup.narrative}
                  </p>
                </DrawerSection>
              )}
              {rollupQuery.data.rollup.highlights?.length ? (
                <DrawerSection title="Highlights">
                  <ul className="flex flex-col gap-1 text-[12px] text-[var(--artemis-white)]">
                    {rollupQuery.data.rollup.highlights.slice(0, 3).map((h, i) => (
                      <li key={i} className="flex gap-2">
                        <span className="text-[var(--text-muted)]">·</span>
                        <span>{h}</span>
                      </li>
                    ))}
                  </ul>
                </DrawerSection>
              ) : null}
            </>
          )}
        </div>
      )}

      <DrawerFooterAction
        href={`/session/?id=${sessionID}&tab=overview`}
        label="Open full session page"
      />
    </div>
  );
}
