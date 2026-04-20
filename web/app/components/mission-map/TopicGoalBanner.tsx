"use client";

import { Sparkles } from "lucide-react";
import type { SessionTopic } from "../../lib/api-types";
import { timeAgo } from "../../lib/time";

/**
 * TopicGoalBanner — one row in the per-session topic-goal stack on
 * the Mission map. The active topic (most-recent last_seen_at) gets a
 * brighter accent so the operator can tell which topic Claude is
 * currently inside; the rest fade slightly to read as past or paused
 * branches. Branched topics (parent_topic_id non-null) are inset and
 * prefixed with a "↳" connector so the parent / child relationship
 * is obvious without dragging in a full tree renderer (that lives in
 * Phase 3b).
 */
export default function TopicGoalBanner({
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
      style={closed ? { opacity: 0.65 } : undefined}
    >
      <div
        className={`flex h-7 w-7 flex-shrink-0 items-center justify-center rounded-full ${
          isActive
            ? "bg-[var(--artemis-red)]/30 text-[var(--artemis-red)]"
            : "bg-[var(--bg-overlay)] text-[var(--text-muted)]"
        }`}
        aria-hidden="true"
      >
        <Sparkles size={12} strokeWidth={1.75} />
      </div>
      <div className="flex-1 min-w-0">
        <p
          className={`font-display text-[9px] uppercase tracking-[0.16em] ${
            isActive ? "text-[var(--artemis-red)]" : "text-[var(--text-muted)]"
          }`}
        >
          {branched ? "↳ Branch goal" : "Mission goal"}
          {isActive ? " · active" : closed ? " · closed" : null}
        </p>
        <p className="mt-1 text-[13px] leading-snug text-[var(--artemis-white)]">
          {topic.goal || "Goal not set"}
        </p>
        <p className="mt-1 font-mono text-[9px] text-[var(--text-muted)]">
          opened {timeAgo(topic.opened_at)} · last seen{" "}
          {timeAgo(topic.last_seen_at)}
        </p>
      </div>
    </div>
  );
}
