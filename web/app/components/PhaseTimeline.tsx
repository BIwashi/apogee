"use client";

import { useCallback, useMemo, useState } from "react";
import { RefreshCw, Sparkles } from "lucide-react";

import { apiUrl } from "../lib/api";
import type { PhaseBlock, Rollup, RollupResponse, Turn } from "../lib/api-types";
import { useApi } from "../lib/swr";
import { timeAgo } from "../lib/time";
import Card from "./Card";
import PhaseCard, { PHASE_COLORS } from "./PhaseCard";
import PhaseDrawer from "./PhaseDrawer";
import SectionHeader from "./SectionHeader";

/**
 * PhaseTimeline — the Timeline tab's primary surface. Renders a vertical
 * timeline of PhaseCards derived from the tier-3 narrative rollup, with a
 * side-drawer side panel that opens on click.
 *
 * Empty state: when `phases` is missing or empty the timeline shows a
 * "Generate narrative" button that POSTs to /v1/sessions/:id/narrative and
 * polls the rollup endpoint for an updated response.
 */

interface PhaseTimelineProps {
  sessionId: string;
  turns: Turn[];
}

export default function PhaseTimeline({ sessionId, turns }: PhaseTimelineProps) {
  const { data, mutate } = useApi<RollupResponse>(
    sessionId ? `/v1/sessions/${sessionId}/rollup` : null,
    { refreshInterval: 10_000 },
  );
  const rollup: Rollup | null = data?.rollup ?? null;
  const phases: PhaseBlock[] = useMemo(() => rollup?.phases ?? [], [rollup]);
  const [pending, setPending] = useState(false);
  const [active, setActive] = useState<PhaseBlock | null>(null);
  const [drawerOpen, setDrawerOpen] = useState(false);

  const onGenerate = useCallback(async () => {
    if (!sessionId || pending) return;
    setPending(true);
    try {
      await fetch(apiUrl(`/v1/sessions/${sessionId}/narrative`), {
        method: "POST",
      });
      // Optimistic revalidate after a short delay.
      window.setTimeout(() => {
        void mutate();
      }, 1500);
    } finally {
      setPending(false);
    }
  }, [sessionId, pending, mutate]);

  const onPhaseClick = useCallback((phase: PhaseBlock) => {
    setActive(phase);
    setDrawerOpen(true);
  }, []);

  const closeDrawer = useCallback(() => {
    setDrawerOpen(false);
  }, []);

  if (!rollup) {
    return (
      <EmptyState
        title="No rollup yet"
        body="The session rollup will appear once at least two turns have closed. The phase narrative is computed on top of the rollup."
        buttonLabel="Generate narrative"
        pending={pending}
        onGenerate={onGenerate}
      />
    );
  }

  if (phases.length === 0) {
    return (
      <EmptyState
        title="No phase narrative yet"
        body="The tier-3 narrative worker has not run for this session. Generating a narrative reuses the existing rollup and groups the turns into semantic phases."
        buttonLabel="Generate narrative"
        pending={pending}
        onGenerate={onGenerate}
      />
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <SectionHeader
        title="Phase narrative"
        subtitle="Clustered view of every closed turn, written by the tier-3 summarizer."
        actions={
          rollup.narrative_generated_at ? (
            <div className="flex items-center gap-2 font-mono text-[10px] text-[var(--text-muted)]">
              <span>
                generated {timeAgo(rollup.narrative_generated_at)} ·{" "}
                {rollup.narrative_model || rollup.model}
              </span>
              <button
                type="button"
                onClick={onGenerate}
                disabled={pending}
                className="inline-flex items-center gap-1 rounded border border-[var(--border)] px-2 py-[2px] text-[10px] text-[var(--text-muted)] hover:text-white disabled:opacity-50"
                aria-label="Regenerate phase narrative"
              >
                <RefreshCw
                  size={12}
                  strokeWidth={1.5}
                  className={pending ? "animate-spin" : undefined}
                />
                <span>regenerate</span>
              </button>
            </div>
          ) : null
        }
      />
      <div className="relative pl-8">
        {/* Vertical rail */}
        <div
          aria-hidden
          className="absolute bottom-0 left-3 top-0 w-[2px] rounded-full"
          style={{ background: "var(--border)" }}
        />
        <ul className="flex flex-col gap-4">
          {phases.map((phase) => (
            <li key={`${phase.index}-${phase.started_at}`} className="relative">
              <span
                aria-hidden
                className="absolute -left-6 top-4 h-3 w-3 rounded-full border-2"
                style={{
                  background: "var(--bg-surface)",
                  borderColor: PHASE_COLORS[phase.kind] ?? "var(--text-muted)",
                }}
              />
              <PhaseCard phase={phase} turns={turns} onClick={onPhaseClick} />
            </li>
          ))}
        </ul>
      </div>
      <PhaseDrawer
        open={drawerOpen}
        sessionId={sessionId}
        phase={active}
        turns={turns}
        onClose={closeDrawer}
      />
    </div>
  );
}

function EmptyState({
  title,
  body,
  buttonLabel,
  pending,
  onGenerate,
}: {
  title: string;
  body: string;
  buttonLabel: string;
  pending: boolean;
  onGenerate: () => void;
}) {
  return (
    <Card className="flex flex-col items-start gap-3 p-6">
      <div className="flex items-center gap-2">
        <Sparkles size={14} strokeWidth={1.5} color="var(--accent)" />
        <p className="font-display text-[12px] uppercase tracking-[0.16em] text-white">
          {title}
        </p>
      </div>
      <p className="max-w-2xl text-[12px] leading-relaxed text-[var(--text-muted)]">
        {body}
      </p>
      <button
        type="button"
        onClick={onGenerate}
        disabled={pending}
        className="inline-flex items-center gap-2 rounded-md border border-[var(--border-bright)] bg-[var(--bg-raised)] px-3 py-1.5 font-mono text-[12px] text-white hover:bg-[var(--bg-overlay)] disabled:cursor-not-allowed disabled:opacity-40"
      >
        <RefreshCw
          size={13}
          strokeWidth={1.5}
          className={pending ? "animate-spin" : undefined}
        />
        {buttonLabel}
      </button>
    </Card>
  );
}
