"use client";

import { useState } from "react";
import { RefreshCw } from "lucide-react";

import { apiUrl } from "../lib/api";
import type { Rollup, RollupResponse } from "../lib/api-types";
import { useApi } from "../lib/swr";
import { timeAgo } from "../lib/time";
import Card from "./Card";
import SectionHeader from "./SectionHeader";

/**
 * RollupPanel — the per-session digest card. Renders the Sonnet-powered
 * narrative produced by the rollup worker, with empty-state copy when no
 * digest has landed yet and a "Regenerate" button that POSTs a manual job
 * to /v1/sessions/:id/rollup. The card is rendered inside the session
 * detail OverviewTab.
 */

interface Props {
  sessionId: string;
}

const EMPTY_HINT =
  "Rollup will appear once this session has multiple closed turns.";

export default function RollupPanel({ sessionId }: Props) {
  const { data, mutate } = useApi<RollupResponse>(
    sessionId ? `/v1/sessions/${sessionId}/rollup` : null,
    { refreshInterval: 15_000 },
  );
  const [pending, setPending] = useState(false);

  const onRegenerate = async () => {
    if (!sessionId || pending) return;
    setPending(true);
    try {
      await fetch(apiUrl(`/v1/sessions/${sessionId}/rollup`), {
        method: "POST",
      });
      // Optimistic: revalidate after a short delay so the worker has a
      // chance to land. The SSE session.updated event will also bump us.
      setTimeout(() => {
        void mutate();
      }, 1500);
    } finally {
      setPending(false);
    }
  };

  const rollup = data?.rollup ?? null;

  if (!rollup) {
    return (
      <Card className="p-4">
        <SectionHeader
          title="Session rollup"
          subtitle="Long-range narrative across every closed turn."
        />
        <p className="text-[12px] text-[var(--text-muted)]">{EMPTY_HINT}</p>
        <div className="mt-3">
          <RegenerateButton pending={pending} onClick={onRegenerate} />
        </div>
      </Card>
    );
  }

  return (
    <Card className="p-4">
      <SectionHeader
        title="Session rollup"
        subtitle="Long-range narrative across every closed turn."
      />
      <div className="flex flex-col gap-4">
        <p className="text-[14px] font-medium leading-snug text-[var(--artemis-white)]">
          {rollup.headline}
        </p>
        {rollup.narrative && (
          <p className="whitespace-pre-line text-[12px] leading-relaxed text-[var(--text-muted)]">
            {rollup.narrative}
          </p>
        )}
        <RollupSection title="Highlights" items={rollup.highlights} />
        <RollupSection title="Patterns" items={rollup.patterns} />
        <RollupSection
          title="Open threads"
          items={rollup.open_threads}
          tone="critical"
        />
        <div className="flex flex-wrap items-center justify-between gap-3 font-mono text-[10px] text-[var(--text-muted)]">
          <span>
            {rollup.model} · {rollup.turn_count} turns · generated{" "}
            {timeAgo(rollup.generated_at)} ago
          </span>
          <RegenerateButton pending={pending} onClick={onRegenerate} />
        </div>
      </div>
    </Card>
  );
}

function RollupSection({
  title,
  items,
  tone,
}: {
  title: string;
  items: string[];
  tone?: "critical";
}) {
  if (!items || items.length === 0) return null;
  const color =
    tone === "critical" ? "text-[var(--status-critical)]" : "text-[var(--artemis-white)]";
  return (
    <div className="flex flex-col gap-1">
      <span className="font-display text-[10px] uppercase tracking-[0.14em] text-[var(--artemis-space)]">
        {title}
      </span>
      <ul className={`flex flex-col gap-1 text-[12px] ${color}`}>
        {items.map((item, idx) => (
          <li key={`${title}-${idx}`} className="flex gap-2 leading-snug">
            <span className="font-mono text-[10px] text-[var(--text-muted)]">
              ·
            </span>
            <span>{item}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

function RegenerateButton({
  pending,
  onClick,
}: {
  pending: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={pending}
      className="inline-flex items-center gap-1 rounded-[4px] border border-[var(--border)] px-2 py-[2px] font-mono text-[10px] text-[var(--text-muted)] transition hover:text-[var(--artemis-white)] disabled:opacity-50"
      aria-label="Regenerate rollup"
    >
      <RefreshCw
        size={12}
        strokeWidth={1.5}
        className={pending ? "animate-spin" : undefined}
      />
      <span>Regenerate rollup</span>
    </button>
  );
}

// Helper for callers that already have the typed payload in scope. Currently
// unused but exported so future consumers can render a static rollup without
// triggering a fetch.
export type { Rollup };
