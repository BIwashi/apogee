"use client";

import { Info } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";

import type { TurnStatus } from "../lib/api-types";
import InterventionComposer from "./InterventionComposer";
import InterventionQueue from "./InterventionQueue";
import InterventionTimeline from "./InterventionTimeline";
import SectionHeader from "./SectionHeader";

/**
 * OperatorQueueSection — composite glue that lays out the composer,
 * queue, and timeline on the turn detail page. Respects the turn status
 * and disables composition when the turn is no longer running.
 *
 * Layout responds to container width: the composer and queue sit side
 * by side above 1100px wide, and stack vertically below that breakpoint.
 * The past-interventions timeline always spans the full width below.
 */

export interface OperatorQueueSectionProps {
  sessionId: string;
  turnId: string;
  turnStatus: TurnStatus | string;
  initialCompose?: boolean;
}

export default function OperatorQueueSection({
  sessionId,
  turnId,
  turnStatus,
  initialCompose = false,
}: OperatorQueueSectionProps) {
  const [composeFocused, setComposeFocused] = useState(initialCompose);
  const [composerScope, setComposerScope] = useState<
    "this_turn" | "this_session"
  >("this_turn");

  useEffect(() => {
    setComposeFocused(initialCompose);
  }, [initialCompose]);

  // Alt+I focuses the composer. Mount a single listener on the window so
  // the shortcut works regardless of which span the operator last clicked.
  useEffect(() => {
    const onKey = (ev: KeyboardEvent) => {
      if (ev.altKey && (ev.key === "i" || ev.key === "I")) {
        ev.preventDefault();
        setComposeFocused(false);
        // Force re-run by toggling in the next microtask.
        queueMicrotask(() => setComposeFocused(true));
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const isRunning = turnStatus === "running";
  const disabledReason = useMemo(
    () =>
      isRunning
        ? undefined
        : "This turn has already ended. Target a different turn or scope to this session.",
    [isRunning],
  );

  const onSubmitted = useCallback(() => {
    // Nothing to do — the queue polls + SSE-refreshes on its own.
  }, []);

  return (
    <section className="flex flex-col gap-4">
      <SectionHeader
        title="Operator queue"
        subtitle={
          <span className="flex items-center gap-2">
            Operator-initiated messages pushed into this session.
            <kbd
              className="rounded border px-1 font-mono text-[10px]"
              style={{
                borderColor: "var(--border-bright)",
                background: "var(--bg-overlay)",
                color: "var(--text-muted)",
              }}
            >
              alt+i
            </kbd>
          </span>
        }
      />
      <div className="grid gap-3 min-[1100px]:grid-cols-[2fr_1fr]">
        <div className="flex flex-col gap-2">
          <InterventionComposer
            sessionId={sessionId}
            turnId={turnId}
            autoFocus={composeFocused}
            disabled={!isRunning}
            disabledReason={disabledReason}
            onSubmitted={onSubmitted}
            onScopeChange={setComposerScope}
          />
          {composerScope === "this_session" && (
            <p
              className="flex items-start gap-2 rounded border px-3 py-2 font-mono text-[10px]"
              style={{
                borderColor: "var(--border-bright)",
                background: "var(--bg-overlay)",
                color: "var(--text-muted)",
              }}
            >
              <Info size={16} strokeWidth={1.5} color="var(--artemis-earth)" />
              This message stays queued until the session ends, even if this
              turn completes.
            </p>
          )}
        </div>
        <InterventionQueue sessionId={sessionId} turnId={turnId} />
      </div>
      <InterventionTimeline sessionId={sessionId} turnId={turnId} />
    </section>
  );
}
