"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { apiUrl } from "../../lib/api";
import type { ApogeeEvent, SessionPayload } from "../../lib/api-types";
import { SSE_EVENT_TYPES } from "../../lib/api-types";
import { useEventStream } from "../../lib/sse";

/**
 * NarrativeGenerationState — the observable state of the tier-3
 * narrative worker, as seen from the Mission UI.
 *
 *   - `generating`: true from the moment the POST kicks off until one
 *     of the exit signals fires. The spinning Re-chart button and the
 *     full-page "Charting mission" card both watch this flag.
 *   - `elapsedSeconds`: integer seconds since the POST. Drives the
 *     "12s elapsed" counter so operators can see the worker is still
 *     making progress through Sonnet's 5–30s call.
 *   - `error`: human-readable string when the POST fails outright,
 *     the safety timeout expires, or the narrative worker reports an
 *     error upstream. `null` otherwise.
 *   - `start()`: kick off a new generation. No-op while `generating`.
 */
export interface NarrativeGenerationState {
  generating: boolean;
  elapsedSeconds: number;
  error: string | null;
  start: () => void;
}

// Safety timeout — how long to wait for the narrative worker before
// deciding it is wedged and flipping the UI back to the error state.
// The Sonnet call itself is bounded by summarizer.Config.Timeout
// (120s default on the server), so 150s leaves headroom for the
// rollup to write + SSE to propagate before the UI gives up.
const NARRATIVE_SAFETY_TIMEOUT_MS = 150_000;

interface NarrativeRequest {
  /** rollup.narrative_generated_at value captured at POST time. The
   *  request is considered complete once the current value differs. */
  baseline: string | null;
  /** Date.now() when the POST was sent. Drives the elapsed counter
   *  and the safety timeout deadline. */
  startedAt: number;
}

/**
 * useNarrativeGeneration — tracks the lifecycle of a tier-3 narrative
 * generation request against a single session. The worker is
 * asynchronous: POST /v1/sessions/:id/narrative returns 202
 * immediately but the actual Sonnet call happens in the background.
 * This hook bridges that gap for the UI.
 *
 * Signals that flip `generating` back to false:
 *
 *   1. The rollup's `narrative_generated_at` timestamp advances past
 *      the baseline captured at POST time. Detected via SWR polling
 *      on `/v1/sessions/:id/rollup` plus an SSE-triggered revalidate
 *      when the collector broadcasts a `session.updated` event for
 *      this session.
 *   2. The safety timeout (150s) expires.
 *   3. The POST itself errors (network failure, 500, etc.).
 *
 * The elapsed-time counter is driven by a 1Hz setInterval while
 * `generating` is true. Counter is reset to 0 when a new generation
 * starts, and left at its final value after completion so operators
 * can see "took 17s" briefly before the graph renders.
 */
export function useNarrativeGeneration({
  sessionId,
  currentGeneratedAt,
  revalidate,
}: {
  sessionId: string;
  currentGeneratedAt: string | null;
  revalidate: () => void;
}): NarrativeGenerationState {
  // The active request. `null` when idle. Set by start(), cleared by
  // the timeout callback or the fetch-error callback. Completion via
  // baseline-advance is handled as a *derived* state below — we do
  // not clear `request` on completion so `elapsedSeconds` stays on
  // its final value for a beat ("took 17s") before the graph
  // renders.
  const [request, setRequest] = useState<NarrativeRequest | null>(null);
  // Wall-clock ticker. Re-rendered every 1Hz while a request is live
  // so the elapsed-time counter updates. Decoupled from `request` so
  // we do not have to call setState from the completion-checking
  // render path.
  const [now, setNow] = useState(() => Date.now());
  const [error, setError] = useState<string | null>(null);

  // Derived: is the request still in flight? True while we have an
  // active request whose baseline has not yet been displaced by a
  // newer rollup. This is computed each render from current props
  // and state, so baseline-advance completion needs no setState
  // (and therefore no react-hooks/set-state-in-effect warning).
  const generating =
    request !== null && currentGeneratedAt === request.baseline;

  const elapsedSeconds = request
    ? Math.max(0, Math.floor((now - request.startedAt) / 1000))
    : 0;

  // 1Hz ticker — only runs while a request is active.
  useEffect(() => {
    if (!request) return;
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => {
      window.clearInterval(id);
    };
  }, [request]);

  // Safety timeout: when the worker never reports a new rollup
  // within the grace window, flip to the error state so the button
  // does not stay permanently disabled.
  useEffect(() => {
    if (!request) return;
    const timer = window.setTimeout(() => {
      setRequest(null);
      setError(
        "Narrative worker did not respond within 150s. It may still finish in the background — try Re-chart again in a moment.",
      );
    }, NARRATIVE_SAFETY_TIMEOUT_MS);
    return () => {
      window.clearTimeout(timer);
    };
  }, [request]);

  // SSE booster: when the collector broadcasts `session.updated` for
  // this session, poke SWR to revalidate the rollup immediately
  // instead of waiting for the next 10s poll tick. This shaves most
  // of the detection latency off the "clicked Re-chart → graph
  // appears" loop. Polling remains the fallback for operators whose
  // SSE stream is down.
  const sessionFilter = useMemo(
    () => (sessionId ? { sessionId } : undefined),
    [sessionId],
  );
  const { subscribe } =
    useEventStream<ApogeeEvent<SessionPayload>>(sessionFilter);
  useEffect(() => {
    if (!generating) return;
    return subscribe((event) => {
      if (event.type === SSE_EVENT_TYPES.SessionUpdated) {
        revalidate();
      }
    });
  }, [generating, subscribe, revalidate]);

  const start = useCallback(() => {
    if (!sessionId) return;
    // Ignore clicks that land while a request is already in flight.
    // Intentionally using a closure over currentGeneratedAt rather
    // than the derived `generating` flag so we do not need to list
    // `generating` in the dependency array and re-create the
    // callback on every tick.
    setRequest((prev) => {
      if (prev !== null) return prev;
      return { baseline: currentGeneratedAt, startedAt: Date.now() };
    });
    setError(null);
    void fetch(apiUrl(`/v1/sessions/${sessionId}/narrative`), {
      method: "POST",
    })
      .then((res) => {
        if (!res.ok) {
          throw new Error(`POST /narrative returned HTTP ${res.status}`);
        }
      })
      .catch((err: unknown) => {
        setRequest(null);
        setError(
          err instanceof Error
            ? err.message
            : "Failed to enqueue narrative generation.",
        );
      });
  }, [sessionId, currentGeneratedAt]);

  return { generating, elapsedSeconds, error, start };
}
