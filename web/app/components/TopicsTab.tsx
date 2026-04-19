"use client";

import { useMemo } from "react";
import { GitBranch, Sparkles } from "lucide-react";
import type {
  SessionTopic,
  SessionTopicTransitionsResponse,
  SessionTopicsResponse,
  TopicTransition,
} from "../lib/api-types";
import { useApi } from "../lib/swr";
import { formatClock, timeAgo } from "../lib/time";
import Card from "./Card";
import SectionHeader from "./SectionHeader";

/**
 * TopicsTab — the dashboard counterpart to `apogee topics show`. Lists
 * the per-session topic forest plus the per-turn classifier-decision
 * audit trail for one session. Surfacing this inside the session detail
 * page (rather than a separate top-level page) gives operators a
 * single deep link they can share when investigating "why did Mission
 * draw this banner".
 *
 * Two stacked panels:
 *
 *   1. Topic forest — one card per topic, parent-child indented with
 *      a "↳" connector. Mirrors the chronological-by-opened-at output
 *      of `topics show`. Active topic (highest last_seen_at) gets the
 *      artemis-red accent that the Mission Goal banner uses.
 *
 *   2. Transition audit — every classifier decision in chronological
 *      order, including the "unknown" rows that did not earn a
 *      topic stamp. Confidence is rendered as a small inline bar.
 */
export default function TopicsTab({ sessionId }: { sessionId: string }) {
  const { data: topicsData, isLoading: topicsLoading } =
    useApi<SessionTopicsResponse>(
      sessionId ? `/v1/sessions/${sessionId}/topics` : null,
      { refreshInterval: 10_000 },
    );
  const { data: transitionsData, isLoading: transitionsLoading } =
    useApi<SessionTopicTransitionsResponse>(
      sessionId ? `/v1/sessions/${sessionId}/topic-transitions` : null,
      { refreshInterval: 10_000 },
    );

  const topics: SessionTopic[] = useMemo(
    () => topicsData?.topics ?? [],
    [topicsData],
  );
  const transitions: TopicTransition[] = useMemo(
    () => transitionsData?.transitions ?? [],
    [transitionsData],
  );

  const activeTopicID: string | null = useMemo(() => {
    if (topics.length === 0) return null;
    return topics.reduce(
      (acc, t) => (t.last_seen_at > acc.last_seen_at ? t : acc),
      topics[0],
    ).topic_id;
  }, [topics]);

  if (!topicsLoading && topics.length === 0) {
    return (
      <Card className="p-6">
        <SectionHeader
          title="No topics yet"
          subtitle="The per-turn topic classifier writes one row per closed turn into the topic_transitions / session_topics tables. This session has not produced any classifier output — either the session pre-dates the classifier (run `apogee topics backfill --session <id>` to recover) or no turn has finished yet."
        />
      </Card>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <section>
        <SectionHeader
          title="Topic forest"
          subtitle="Each row is one branch the classifier opened in this session. The active topic (most-recently-touched) carries the red accent the Mission Goal banner also uses."
        />
        <Card className="flex flex-col gap-2 p-4">
          {topics.map((t) => (
            <TopicForestRow
              key={t.topic_id}
              topic={t}
              isActive={t.topic_id === activeTopicID}
            />
          ))}
        </Card>
      </section>

      <section>
        <SectionHeader
          title={`Transition audit (${transitions.length})`}
          subtitle="Every classifier decision the worker has made for this session, oldest first. Rows tagged 'unknown' had confidence below the 0.6 threshold — they are kept for backfill / re-evaluation but did not stamp a turn."
        />
        <Card className="overflow-x-auto p-0">
          {!transitionsLoading && transitions.length === 0 ? (
            <p className="px-4 py-6 text-[12px] text-[var(--text-muted)]">
              No transitions recorded.
            </p>
          ) : (
            <table className="w-full border-collapse text-[12px]">
              <thead>
                <tr className="text-left text-[10px] uppercase tracking-[0.14em] text-[var(--text-muted)]">
                  <th className="border-b border-[var(--border)] px-3 py-2 font-medium">
                    #
                  </th>
                  <th className="border-b border-[var(--border)] px-3 py-2 font-medium">
                    Turn
                  </th>
                  <th className="border-b border-[var(--border)] px-3 py-2 font-medium">
                    Kind
                  </th>
                  <th className="border-b border-[var(--border)] px-3 py-2 font-medium">
                    Confidence
                  </th>
                  <th className="border-b border-[var(--border)] px-3 py-2 font-medium">
                    From
                  </th>
                  <th className="border-b border-[var(--border)] px-3 py-2 font-medium">
                    To
                  </th>
                  <th className="border-b border-[var(--border)] px-3 py-2 font-medium">
                    Model
                  </th>
                  <th className="border-b border-[var(--border)] px-3 py-2 font-medium">
                    When
                  </th>
                </tr>
              </thead>
              <tbody>
                {transitions.map((tr, i) => (
                  <TransitionRow key={tr.turn_id} index={i} t={tr} />
                ))}
              </tbody>
            </table>
          )}
        </Card>
      </section>
    </div>
  );
}

function TopicForestRow({
  topic,
  isActive,
}: {
  topic: SessionTopic;
  isActive: boolean;
}) {
  const branched = topic.parent_topic_id != null;
  const closed = topic.closed_at != null;
  return (
    <div
      className={`flex items-start gap-3 rounded border p-3 transition-colors ${
        isActive
          ? "border-[var(--artemis-red)]/60 bg-[var(--artemis-red)]/10"
          : "border-[var(--border)] bg-[var(--bg-raised)]"
      } ${branched ? "ml-5" : ""}`}
      style={closed ? { opacity: 0.7 } : undefined}
    >
      <div
        className={`flex h-7 w-7 flex-shrink-0 items-center justify-center rounded-full ${
          isActive
            ? "bg-[var(--artemis-red)]/30 text-[var(--artemis-red)]"
            : "bg-[var(--bg-overlay)] text-[var(--text-muted)]"
        }`}
        aria-hidden="true"
      >
        {branched ? (
          <GitBranch size={12} strokeWidth={1.75} />
        ) : (
          <Sparkles size={12} strokeWidth={1.75} />
        )}
      </div>
      <div className="flex-1 min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <p
            className={`font-display text-[10px] uppercase tracking-[0.14em] ${
              isActive
                ? "text-[var(--artemis-red)]"
                : "text-[var(--text-muted)]"
            }`}
          >
            {branched ? "↳ Branch goal" : "Mission goal"}
            {isActive ? " · active" : closed ? " · closed" : null}
          </p>
          <p className="font-mono text-[10px] text-[var(--text-faint)]">
            {topic.topic_id}
          </p>
        </div>
        <p className="mt-1 text-[13px] leading-snug text-[var(--artemis-white)]">
          {topic.goal || "Goal not set"}
        </p>
        <p className="mt-1 font-mono text-[10px] text-[var(--text-muted)]">
          opened {timeAgo(topic.opened_at)} · last seen{" "}
          {timeAgo(topic.last_seen_at)}
          {topic.parent_topic_id ? ` · parent ${topic.parent_topic_id}` : ""}
        </p>
      </div>
    </div>
  );
}

function TransitionRow({ index, t }: { index: number; t: TopicTransition }) {
  const conf = t.confidence ?? null;
  const kindTone =
    t.kind === "unknown"
      ? "text-[var(--text-muted)]"
      : t.kind === "resume"
        ? "text-[var(--artemis-red)]"
        : t.kind === "new"
          ? "text-[var(--artemis-earth)]"
          : "text-[var(--artemis-white)]";
  return (
    <tr className="border-b border-[var(--border)] last:border-b-0 hover:bg-[var(--bg-raised)]">
      <td className="px-3 py-2 font-mono text-[10px] text-[var(--text-muted)]">
        {index + 1}
      </td>
      <td className="px-3 py-2 font-mono text-[11px] text-[var(--artemis-white)]">
        {t.turn_id.length > 12 ? t.turn_id.slice(0, 12) : t.turn_id}
      </td>
      <td className={`px-3 py-2 font-mono text-[11px] ${kindTone}`}>
        {t.kind}
      </td>
      <td className="px-3 py-2">
        <ConfidenceBar value={conf} />
      </td>
      <td className="px-3 py-2 font-mono text-[10px] text-[var(--text-muted)]">
        {t.from_topic_id ?? "—"}
      </td>
      <td className="px-3 py-2 font-mono text-[10px] text-[var(--text-muted)]">
        {t.to_topic_id ?? "—"}
      </td>
      <td className="px-3 py-2 font-mono text-[10px] text-[var(--text-muted)]">
        {t.model}
      </td>
      <td className="px-3 py-2 font-mono text-[10px] text-[var(--text-muted)]">
        {formatClock(t.created_at)}
      </td>
    </tr>
  );
}

function ConfidenceBar({ value }: { value: number | null }) {
  if (value == null) {
    return <span className="text-[10px] text-[var(--text-muted)]">—</span>;
  }
  const pct = Math.round(Math.max(0, Math.min(1, value)) * 100);
  const tone =
    value < 0.6
      ? "var(--text-muted)"
      : value < 0.8
        ? "var(--status-warning)"
        : "var(--status-success)";
  return (
    <span className="inline-flex items-center gap-2">
      <span
        className="relative inline-block h-1.5 w-16 overflow-hidden rounded-sm bg-[var(--bg-overlay)]"
        aria-hidden="true"
      >
        <span
          className="absolute inset-y-0 left-0 rounded-sm"
          style={{ width: `${pct}%`, background: tone }}
        />
      </span>
      <span className="font-mono text-[10px] tabular-nums text-[var(--text-muted)]">
        {value.toFixed(2)}
      </span>
    </span>
  );
}
