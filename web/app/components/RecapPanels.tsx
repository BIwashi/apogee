"use client";

import { RefreshCw } from "lucide-react";

import type { Recap } from "../lib/api-types";
import { timeAgo } from "../lib/time";
import Card from "./Card";
import RecapOutcomeChip from "./RecapOutcomeChip";
import SectionHeader from "./SectionHeader";

/**
 * RecapPanels — the three summary cards on the turn detail page. When the
 * Haiku-powered summariser has not yet written a recap, the panels render
 * an empty-state hint. When populated, the first card shows the outcome
 * chip plus one-line headline (and a failure cause if present), the second
 * lists the LLM's key steps, and the third shows notable events.
 */

interface Props {
  recap: Recap | null;
  onRegenerate?: () => void;
  regenerating?: boolean;
}

const EMPTY_HINT = "Recap will appear within ~30s of turn close.";

export default function RecapPanels({
  recap,
  onRegenerate,
  regenerating,
}: Props) {
  if (!recap) {
    return (
      <div className="grid gap-3 md:grid-cols-3">
        <Card className="p-4">
          <SectionHeader title="Summary" />
          <p className="text-[12px] text-[var(--text-muted)]">
            Awaiting recap.
          </p>
          <p className="mt-1 font-mono text-[10px] text-[var(--text-muted)]">
            {EMPTY_HINT}
          </p>
        </Card>
        <Card className="p-4">
          <SectionHeader title="Key steps" />
          <p className="text-[12px] text-[var(--text-muted)]">
            Key steps will appear here.
          </p>
        </Card>
        <Card className="p-4">
          <SectionHeader title="Notable events" />
          <p className="text-[12px] text-[var(--text-muted)]">
            Notable events will appear here.
          </p>
        </Card>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="grid gap-3 md:grid-cols-3">
        <Card className="p-4">
          <SectionHeader title="Summary" />
          <div className="mb-2">
            <RecapOutcomeChip outcome={recap.outcome} />
          </div>
          <p className="text-[13px] leading-snug text-white">
            {recap.headline}
          </p>
          {recap.failure_cause && (
            <p className="mt-2 text-[11px] text-[var(--status-critical)]">
              {recap.failure_cause}
            </p>
          )}
        </Card>

        <Card className="p-4">
          <SectionHeader title="Key steps" />
          {recap.key_steps.length === 0 ? (
            <p className="text-[12px] text-[var(--text-muted)]">
              No key steps recorded.
            </p>
          ) : (
            <ol className="flex flex-col gap-1 text-[12px] text-white">
              {recap.key_steps.map((step, idx) => (
                <li key={`step-${idx}`} className="flex gap-2 leading-snug">
                  <span className="font-mono text-[10px] text-[var(--text-muted)]">
                    {String(idx + 1).padStart(2, "0")}
                  </span>
                  <span>{step}</span>
                </li>
              ))}
            </ol>
          )}
        </Card>

        <Card className="p-4">
          <SectionHeader title="Notable events" />
          {recap.notable_events.length === 0 ? (
            <p className="text-[12px] text-[var(--text-muted)]">
              Nothing notable.
            </p>
          ) : (
            <ul className="flex flex-col gap-1 text-[12px] text-white">
              {recap.notable_events.map((ev, idx) => (
                <li key={`ev-${idx}`} className="leading-snug">
                  · {ev}
                </li>
              ))}
            </ul>
          )}
        </Card>
      </div>

      <div className="flex flex-wrap items-center justify-between gap-3 font-mono text-[10px] text-[var(--text-muted)]">
        <span>
          {recap.model} · generated {timeAgo(recap.generated_at)} ago
        </span>
        {onRegenerate && (
          <button
            type="button"
            onClick={onRegenerate}
            disabled={regenerating}
            className="inline-flex items-center gap-1 rounded-[4px] border border-[var(--border)] px-2 py-[2px] text-[var(--text-muted)] transition hover:text-white disabled:opacity-50"
            aria-label="Regenerate recap"
          >
            <RefreshCw
              size={12}
              strokeWidth={1.5}
              className={regenerating ? "animate-spin" : undefined}
            />
            <span>Regenerate</span>
          </button>
        )}
      </div>
    </div>
  );
}
