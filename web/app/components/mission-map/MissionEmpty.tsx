"use client";

import { AlertTriangle, CheckCheck, Loader, Sparkles } from "lucide-react";
import Card from "../Card";
import SectionHeader from "../SectionHeader";
import type { NarrativeGenerationState } from "./useNarrativeGeneration";

/**
 * MissionEmpty — the placeholder card the Mission map shows before
 * the tier-3 narrative worker has produced any phases. Three visual
 * states keyed off the narrative-generation hook:
 *
 *   - generating: spinner + "Charting mission" + elapsed counter so
 *     operators see live progress through Sonnet's 5–30s call.
 *   - error: warning icon + the failure message + Retry button.
 *   - idle: success-like check icon + the configured title/body +
 *     a button that kicks off generation.
 *
 * Used both by the Live page (passes hideEmpty=true to MissionMap so
 * this component never renders there) and by the session detail
 * page Mission tab (default state until the worker fires).
 */
export default function MissionEmpty({
  title,
  body,
  buttonLabel,
  narrative,
}: {
  title: string;
  body: string;
  buttonLabel: string;
  narrative: NarrativeGenerationState;
}) {
  const { generating, elapsedSeconds, error, start } = narrative;
  return (
    <div className="flex flex-col gap-4">
      <SectionHeader title="Mission" subtitle="Git graph of the session arc." />
      <Card className="relative overflow-hidden p-10">
        <div
          className="mission-starfield pointer-events-none absolute inset-0"
          aria-hidden="true"
        />
        <div className="relative z-10 mx-auto flex max-w-[520px] flex-col items-center gap-4 text-center">
          {generating ? (
            <>
              <div className="flex h-16 w-16 items-center justify-center rounded-full bg-[var(--accent)]/15 text-[var(--accent)]">
                <Loader size={28} strokeWidth={1.5} className="animate-spin" />
              </div>
              <p className="font-display text-[11px] uppercase tracking-[0.16em] text-[var(--artemis-white)]">
                Charting mission
              </p>
              <p className="text-[12px] leading-relaxed text-[var(--text-muted)]">
                The tier-3 narrative worker is reading the session turns and
                generating the phase story. Sonnet usually takes
                5–30&nbsp;seconds. The graph will appear as soon as the worker
                finishes.
              </p>
              <p className="font-mono text-[11px] text-[var(--text-muted)]">
                {elapsedSeconds}s elapsed
              </p>
            </>
          ) : error ? (
            <>
              <div className="flex h-16 w-16 items-center justify-center rounded-full bg-[var(--status-critical)]/15 text-[var(--status-critical)]">
                <AlertTriangle size={24} strokeWidth={1.5} />
              </div>
              <p className="font-display text-[11px] uppercase tracking-[0.16em] text-[var(--artemis-white)]">
                Narrative generation failed
              </p>
              <p className="text-[12px] leading-relaxed text-[var(--text-muted)]">
                {error}
              </p>
              <button
                type="button"
                onClick={start}
                className="mt-2 inline-flex items-center gap-2 rounded border border-[var(--accent)]/40 bg-[var(--accent)]/15 px-4 py-2 font-display text-[11px] uppercase tracking-[0.16em] text-[var(--artemis-white)] transition-colors hover:bg-[var(--accent)]/25"
              >
                <Sparkles size={12} strokeWidth={1.75} />
                Retry
              </button>
            </>
          ) : (
            <>
              <div className="flex h-16 w-16 items-center justify-center rounded-full bg-[var(--bg-raised)] text-[var(--artemis-earth)]">
                <CheckCheck size={28} strokeWidth={1.5} />
              </div>
              <p className="font-display text-[11px] uppercase tracking-[0.16em] text-[var(--artemis-white)]">
                {title}
              </p>
              <p className="text-[12px] leading-relaxed text-[var(--text-muted)]">
                {body}
              </p>
              <button
                type="button"
                onClick={start}
                className="mt-2 inline-flex items-center gap-2 rounded border border-[var(--accent)]/40 bg-[var(--accent)]/15 px-4 py-2 font-display text-[11px] uppercase tracking-[0.16em] text-[var(--artemis-white)] transition-colors hover:bg-[var(--accent)]/25"
              >
                <Sparkles size={12} strokeWidth={1.75} />
                {buttonLabel}
              </button>
            </>
          )}
        </div>
      </Card>
    </div>
  );
}
