"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  AlertTriangle,
  Bug,
  CheckCheck,
  Compass,
  Crosshair,
  GitCommit,
  HelpCircle,
  Lightbulb,
  Loader,
  Radio,
  Search,
  Send,
  Sparkles,
  Square,
  TestTube,
  UserCog,
  Wrench,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { apiUrl } from "../lib/api";
import type {
  ApogeeEvent,
  ForecastPhase,
  Intervention,
  InterventionListResponse,
  PhaseBlock,
  PhaseKind,
  Rollup,
  RollupResponse,
  SessionPayload,
  SessionTodosResponse,
  SessionTopic,
  SessionTopicsResponse,
  TodoItem,
  Turn,
} from "../lib/api-types";
import { SSE_EVENT_TYPES } from "../lib/api-types";
import { useEventStream } from "../lib/sse";
import { useApi } from "../lib/swr";
import { timeAgo } from "../lib/time";
import Card from "./Card";
import PhaseDrawer from "./PhaseDrawer";
import SectionHeader from "./SectionHeader";

/**
 * MissionMap — a vertical git-graph view of a Claude Code session.
 *
 * The earlier planetary orbit rendering (Sun / Planets / Moons) was
 * visually distinctive but structurally redundant with the Timeline
 * tab: both were flat lists of phase headlines with a subtle layout
 * flourish. The "Mission" name is good — the metaphor is a mission
 * with a main line of progress, side-quests that branch off when
 * something unexpected comes up, and future stops that have not been
 * reached yet. The right visual for that is a git graph, not a
 * solar system.
 *
 * Layout:
 *
 *   - A single vertical spine on the left carries the session's
 *     main line of phases in chronological order (top = mission
 *     start, bottom = current tail, below that = forecast).
 *   - Each phase is a coloured circle on the spine plus a card
 *     body to its right that shows the kind chip, headline,
 *     narrative excerpt, and key-step bullets pulled from the
 *     tier-3 narrative blob.
 *   - Operator interventions that landed inside a phase fork off
 *     the spine as a short branch. The branch line leaves the
 *     phase node horizontally, hosts the intervention's headline
 *     pill, and merges back into the spine at the next phase
 *     boundary — expressing "side quest, resolved, back to main".
 *   - The currently-running turn (if any) gets a pulsing ring on
 *     the trailing real phase node so operators can tell the
 *     mission is still in motion.
 *   - Tier-3 forecast entries render below the last real phase
 *     node as dashed circles on a dashed extension of the spine.
 *     These are "probable next stops" — the horizon, still
 *     unrealised.
 *
 * Data sources (all existing, no new endpoints):
 *
 *   - /v1/sessions/:id/rollup → tier-2 rollup.headline (mission
 *     goal) + tier-3 rollup.phases[] (main-line nodes) +
 *     rollup.forecast[] (dashed future nodes).
 *   - /v1/sessions/:id/interventions → branch nodes, positioned
 *     via created_at overlap with the phase window.
 *   - turns[] (already fetched by the session detail page) →
 *     the running turn marker.
 *
 * Visual flavour: the card ships with a CSS-only deep-space
 * starfield background (`.mission-starfield`) so the git graph
 * still reads as "mission" even though the planets are gone. The
 * name "Mission" stays; the orbit SVG does not.
 */

interface MissionMapProps {
  sessionId: string;
  turns: Turn[];
  /** When true, return null instead of the MissionEmpty placeholder.
   *  Used on the Live page where the empty state is distracting —
   *  the Mission section should appear silently once the narrative
   *  worker produces a rollup, not advertise its absence. */
  hideEmpty?: boolean;
}

// Icon per PhaseKind — reuses the vocabulary already in the tier-3
// summariser prompt so the node badge matches the LLM output.
const KIND_ICON: Record<PhaseKind, LucideIcon> = {
  implement: Wrench,
  review: Search,
  debug: Bug,
  plan: Compass,
  test: TestTube,
  commit: GitCommit,
  delegate: UserCog,
  explore: Lightbulb,
  other: HelpCircle,
};

// Fixed palette per PhaseKind. Each entry pulls one of the Artemis
// tokens so the node colour on the graph reads as "this kind of
// work" at a glance.
const KIND_TONE: Record<
  PhaseKind,
  { fill: string; ring: string; label: string }
> = {
  implement: {
    fill: "var(--artemis-earth)",
    ring: "var(--artemis-earth)",
    label: "Implement",
  },
  review: {
    fill: "var(--artemis-space)",
    ring: "var(--artemis-space)",
    label: "Review",
  },
  debug: {
    fill: "var(--artemis-red)",
    ring: "var(--artemis-red)",
    label: "Debug",
  },
  plan: {
    fill: "var(--artemis-blue)",
    ring: "var(--artemis-blue)",
    label: "Plan",
  },
  test: {
    fill: "var(--status-warning)",
    ring: "var(--status-warning)",
    label: "Test",
  },
  commit: {
    fill: "var(--status-success)",
    ring: "var(--status-success)",
    label: "Commit",
  },
  delegate: {
    fill: "var(--artemis-shadow)",
    ring: "var(--artemis-shadow)",
    label: "Delegate",
  },
  explore: { fill: "var(--accent)", ring: "var(--accent)", label: "Explore" },
  other: {
    fill: "var(--border-bright)",
    ring: "var(--border-bright)",
    label: "Other",
  },
};

// Layout constants for the git-graph. All measurements are in
// pixels inside the card; the whole component is fluid-width and
// only the spine column is fixed.
const SPINE_X = 40; // x coordinate of the main-line spine
const NODE_R = 14; // outer radius of a phase node
const ROW_GAP = 28; // vertical gap between rows (in addition to card height)
const BRANCH_WIDTH = 120; // how far a side-quest branch extends to the right
const BRANCH_R = 7; // radius of a branch (intervention) node

// bucketInterventions groups operator interventions by the phase
// they landed in, using created_at timestamp overlap. Interventions
// with no matching phase (rare) are dropped — the goal is to show
// "which phase got interrupted", not a global timeline.
function bucketInterventions(
  interventions: Intervention[],
  phases: PhaseBlock[],
): Map<number, Intervention[]> {
  const out = new Map<number, Intervention[]>();
  for (const iv of interventions) {
    if (!iv.created_at) continue;
    for (let i = 0; i < phases.length; i++) {
      const p = phases[i];
      if (iv.created_at >= p.started_at && iv.created_at <= p.ended_at) {
        const arr = out.get(i) ?? [];
        arr.push(iv);
        out.set(i, arr);
        break;
      }
    }
  }
  return out;
}

function shortHeadline(input: string, max = 90): string {
  const s = input.trim();
  if (s.length <= max) return s;
  return s.slice(0, max - 1).trimEnd() + "…";
}

/**
 * NarrativeGenerationState — the observable state of the tier-3 narrative
 * worker, as seen from the Mission UI.
 *
 *   - `generating`: true from the moment the POST kicks off until one of
 *     the exit signals fires. The spinning Re-chart button and the
 *     full-page "Charting mission" card both watch this flag.
 *   - `elapsedSeconds`: integer seconds since the POST. Drives the
 *     "12s elapsed" counter so operators can see the worker is still
 *     making progress through Sonnet's 5–30s call.
 *   - `error`: human-readable string when the POST fails outright, the
 *     safety timeout expires, or the narrative worker reports an error
 *     upstream. `null` otherwise.
 *   - `start()`: kick off a new generation. No-op while `generating`.
 */
interface NarrativeGenerationState {
  generating: boolean;
  elapsedSeconds: number;
  error: string | null;
  start: () => void;
}

// Safety timeout — how long to wait for the narrative worker before
// deciding it is wedged and flipping the UI back to the error state.
// The Sonnet call itself is bounded by summarizer.Config.Timeout
// (120s default on the server), so 150s leaves headroom for the rollup
// to write + SSE to propagate before the UI gives up.
const NARRATIVE_SAFETY_TIMEOUT_MS = 150_000;

/**
 * useNarrativeGeneration — tracks the lifecycle of a tier-3 narrative
 * generation request against a single session. The worker is
 * asynchronous: POST /v1/sessions/:id/narrative returns 202 immediately
 * but the actual Sonnet call happens in the background. This hook
 * bridges that gap for the UI.
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
interface NarrativeRequest {
  /** rollup.narrative_generated_at value captured at POST time. The
   *  request is considered complete once the current value differs. */
  baseline: string | null;
  /** Date.now() when the POST was sent. Drives the elapsed counter
   *  and the safety timeout deadline. */
  startedAt: number;
}

function useNarrativeGeneration({
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
  // baseline-advance is handled as a *derived* state below — we do not
  // clear `request` on completion so `elapsedSeconds` stays on its
  // final value for a beat ("took 17s") before the graph renders.
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

  // 1Hz ticker — only runs while a request is active. Updates the
  // `now` clock which in turn drives `elapsedSeconds` above. The
  // first tick fires a second after start() so the first render
  // briefly shows "0s elapsed"; that is fine — it is honest.
  useEffect(() => {
    if (!request) return;
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => {
      window.clearInterval(id);
    };
  }, [request]);

  // Safety timeout: when the worker never reports a new rollup within
  // the grace window, flip to the error state so the button does not
  // stay permanently disabled.
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

// Compact duration string. Mirrors the helper the retired PhaseCard
// used so a phase's wall-clock span still surfaces next to its turn
// count on the Mission graph.
function formatDuration(ms: number): string {
  if (!ms || ms < 0) return "";
  if (ms < 1000) return `${ms}ms`;
  const seconds = Math.round(ms / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const remSec = seconds % 60;
  if (minutes < 60) return remSec ? `${minutes}m${remSec}s` : `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  const remMin = minutes % 60;
  return remMin ? `${hours}h${remMin}m` : `${hours}h`;
}

export default function MissionMap({
  sessionId,
  turns,
  hideEmpty,
}: MissionMapProps) {
  const { data: rollupData, mutate } = useApi<RollupResponse>(
    sessionId ? `/v1/sessions/${sessionId}/rollup` : null,
    { refreshInterval: 10_000 },
  );
  const { data: interventionsData } = useApi<InterventionListResponse>(
    sessionId ? `/v1/sessions/${sessionId}/interventions?limit=200` : null,
    { refreshInterval: 10_000 },
  );
  const { data: todosData } = useApi<SessionTodosResponse>(
    sessionId ? `/v1/sessions/${sessionId}/todos` : null,
    { refreshInterval: 5_000 },
  );
  const { data: topicsData } = useApi<SessionTopicsResponse>(
    sessionId ? `/v1/sessions/${sessionId}/topics` : null,
    { refreshInterval: 10_000 },
  );

  const rollup: Rollup | null = rollupData?.rollup ?? null;
  const phases: PhaseBlock[] = useMemo(() => rollup?.phases ?? [], [rollup]);
  const forecast: ForecastPhase[] = useMemo(
    () => rollup?.forecast ?? [],
    [rollup],
  );
  const interventions = useMemo(
    () => interventionsData?.interventions ?? [],
    [interventionsData],
  );
  // Model-declared plan: pending + in-progress rows from the most recent
  // TodoWrite call. Completed items are dropped because the phase spine
  // above already tells that story. Order is preserved (Claude writes
  // the list in execution order), with in-progress items rendered with
  // a solid node + pulsing ring and pending items rendered dashed.
  const activeTodos = useMemo(() => {
    const all = todosData?.todos ?? [];
    return all.filter(
      (t) => t.status === "pending" || t.status === "in_progress",
    );
  }, [todosData]);
  const branchesByPhase = useMemo(
    () => bucketInterventions(interventions, phases),
    [interventions, phases],
  );
  const runningTurn = useMemo(
    () => turns.find((t) => String(t.status) === "running") ?? null,
    [turns],
  );

  const [active, setActive] = useState<PhaseBlock | null>(null);
  const [drawerOpen, setDrawerOpen] = useState(false);

  // Scroll plumbing for the bounded mission spine. The spine renders
  // chronologically (oldest → newest → todos → forecast), so the
  // "current" step lives near the bottom rather than the top. We pin
  // the scroll container to a viewport-bounded height, scroll the
  // current step into view on mount + when the session changes, and
  // surface a "Jump to now" floating button when the current step
  // scrolls out of view.
  //
  // The initial-scroll guard and the session id are both held in refs
  // so the effect can read+write them without triggering its own
  // re-run (a setState here would tilt into the
  // react-hooks/set-state-in-effect cascade).
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const currentRef = useRef<HTMLLIElement | null>(null);
  const didInitialScrollRef = useRef(false);
  const lastSessionIdRef = useRef<string | null>(null);
  const [currentInView, setCurrentInView] = useState(true);

  const jumpToNow = useCallback(() => {
    const el = currentRef.current;
    if (!el) return;
    el.scrollIntoView({ behavior: "smooth", block: "center" });
  }, []);

  // Auto-scroll to the current step on first paint after the spine
  // has populated, and on every session switch. Subsequent rerenders
  // (poll ticks, recap landings) do not re-trigger this — operators
  // who scrolled away on purpose would hate having the page yank back
  // every 5 s.
  useEffect(() => {
    if (lastSessionIdRef.current !== sessionId) {
      didInitialScrollRef.current = false;
      lastSessionIdRef.current = sessionId;
    }
    if (didInitialScrollRef.current) return;
    const el = currentRef.current;
    if (!el) return;
    el.scrollIntoView({ behavior: "auto", block: "center" });
    didInitialScrollRef.current = true;
  }, [sessionId, phases.length, activeTodos.length]);

  // Track whether the current step is on screen so the "Jump to now"
  // button can hide itself when no jump is needed. IntersectionObserver
  // is bounded by the scroll container via `root: scrollRef.current` so
  // we don't pick up arbitrary page scroll. setState inside the
  // observer callback is asynchronous-from-React's-pov so it doesn't
  // trip the set-state-in-effect rule.
  useEffect(() => {
    const el = currentRef.current;
    const root = scrollRef.current;
    if (!el || !root) return;
    const observer = new IntersectionObserver(
      ([entry]) => setCurrentInView(entry.isIntersecting),
      { root, threshold: 0.4 },
    );
    observer.observe(el);
    return () => observer.disconnect();
  }, [phases.length, activeTodos.length]);

  // Narrative generation progress tracking. The tier-3 narrative worker
  // runs asynchronously — the POST returns 202 immediately, the actual
  // Sonnet call takes 5-30s in the background. We keep `generating` true
  // from the moment the POST kicks off until one of three exit signals
  // fires: (a) the rollup's narrative_generated_at timestamp advances
  // past the baseline we captured at POST time (detected via SWR polling
  // + SSE-triggered revalidation), (b) a safety timeout expires so a
  // wedged worker cannot permanently disable the button, or (c) the POST
  // itself errors. `startedAt` drives the elapsed-time counter so
  // operators see visible progress; `error` surfaces the timeout /
  // network failure state to the empty-state card.
  const narrative = useNarrativeGeneration({
    sessionId,
    currentGeneratedAt: rollup?.narrative_generated_at ?? null,
    revalidate: mutate,
  });

  const onPhaseClick = useCallback((phase: PhaseBlock) => {
    setActive(phase);
    setDrawerOpen(true);
  }, []);

  if (!rollup) {
    if (hideEmpty) return null;
    return (
      <MissionEmpty
        title="Mission not yet charted"
        body="A session rollup is needed before the mission map can render. The rollup worker runs automatically once at least two turns have closed, or you can trigger it manually."
        buttonLabel="Generate narrative"
        narrative={narrative}
      />
    );
  }

  if (phases.length === 0) {
    if (hideEmpty) return null;
    return (
      <MissionEmpty
        title="No phase narrative yet"
        body="The mission graph plots one node per semantic phase from the tier-3 narrative. That worker has not run for this session yet."
        buttonLabel="Generate narrative"
        narrative={narrative}
      />
    );
  }

  const missionGoal =
    rollup.headline?.trim() ||
    rollup.narrative?.trim().slice(0, 120) ||
    "Mission goal not yet summarised";

  // Per-session topic forest written by the per-turn topic classifier.
  // When non-empty we render one Mission Goal banner per topic instead
  // of the single session-wide rollup headline, since the rollup
  // assumes one workstream per session and a multi-topic session
  // breaks that assumption.
  const topics: SessionTopic[] = topicsData?.topics ?? [];
  const activeTopicID = topics.length
    ? topics.reduce(
        (acc, t) => (t.last_seen_at > acc.last_seen_at ? t : acc),
        topics[0],
      ).topic_id
    : null;

  return (
    <div className="flex flex-col gap-4">
      <SectionHeader
        title="Mission"
        subtitle="Git graph of the session. Main spine = phases, branches = operator interventions, plan = TodoWrite, dashed tail = tier-3 forecast."
        actions={
          <button
            type="button"
            onClick={narrative.start}
            disabled={narrative.generating}
            className="inline-flex items-center gap-2 rounded-md border border-[var(--border)] bg-[var(--bg-raised)] px-3 py-1.5 font-display text-[10px] uppercase tracking-[0.16em] text-[var(--text-muted)] transition-colors hover:bg-[var(--bg-overlay)] hover:text-[var(--artemis-white)] disabled:cursor-not-allowed disabled:opacity-60"
            title={
              narrative.error
                ? `Last attempt failed: ${narrative.error}`
                : "Re-run the tier-3 narrative worker"
            }
          >
            {narrative.generating ? (
              <Loader size={12} strokeWidth={1.75} className="animate-spin" />
            ) : (
              <Sparkles size={12} strokeWidth={1.75} />
            )}
            {narrative.generating
              ? `Re-charting… ${narrative.elapsedSeconds}s`
              : "Re-chart"}
          </button>
        }
      />

      <Card className="relative overflow-hidden p-0">
        {/* Starfield — CSS-only deep-space background. Same class the
            earlier orbit rendering used, so the "mission" flavour
            carries over without the orbit geometry. */}
        <div
          className="mission-starfield pointer-events-none absolute inset-0"
          aria-hidden="true"
        />

        <div className="relative z-10 flex flex-col gap-1 p-5">
          {/* Mission goal banner. When the per-turn topic classifier
              has produced topics, render one banner per topic in
              chronological order so multi-topic sessions show their
              full intent stack. Otherwise fall back to the single
              session-wide rollup headline. */}
          {topics.length > 0 ? (
            <div className="mb-3 flex flex-col gap-1.5">
              {topics.map((t) => (
                <TopicGoalBanner
                  key={t.topic_id}
                  topic={t}
                  isActive={t.topic_id === activeTopicID}
                />
              ))}
            </div>
          ) : (
            <div className="mb-3 flex items-start gap-3 rounded border border-[var(--border)] bg-[var(--bg-raised)] p-3">
              <div className="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-full bg-[var(--artemis-red)]/20 text-[var(--artemis-red)]">
                <Sparkles size={14} strokeWidth={1.75} />
              </div>
              <div className="flex-1 min-w-0">
                <p className="font-display text-[9px] uppercase tracking-[0.16em] text-[var(--artemis-red)]">
                  Mission goal
                </p>
                <p className="mt-1 text-[13px] leading-snug text-[var(--artemis-white)]">
                  {missionGoal}
                </p>
                {rollup.narrative_generated_at ? (
                  <p className="mt-1 font-mono text-[9px] text-[var(--text-muted)]">
                    charted {timeAgo(rollup.narrative_generated_at)} ·{" "}
                    {rollup.narrative_model || rollup.model}
                  </p>
                ) : null}
              </div>
            </div>
          )}

          {/* The graph. Chronological order top → bottom: oldest
              phase first, newest phase last, then TodoWrite "now" rows,
              then the tier-3 forecast tail. The order matches how the
              spine reads visually — past at the top, present in the
              middle, future at the bottom — so the "Next" cards no
              longer contradict the rest of the timeline.

              The whole spine sits inside a bounded-height scroll
              container so the Mission card cannot push the rest of the
              page when a session has many phases. We also scroll the
              "current" step into view on mount and surface a
              "Jump to now" button when the operator scrolls away from
              it. */}
          <div className="relative">
            <div
              ref={scrollRef}
              className="mission-spine-scroll relative max-h-[60vh] overflow-y-auto pr-2"
            >
              <ol className="flex flex-col">
                {phases.map((phase, i, arr) => {
                  // The newest phase sits at the bottom in chronological
                  // order. That is where the running-turn pulse belongs
                  // unless a TodoRow is going to take over (an
                  // in-progress todo is a stronger "now" signal than
                  // the latest phase).
                  const isOldest = i === 0;
                  const isNewest = i === arr.length - 1;
                  const todoTakesOverPulse = activeTodos.some(
                    (t) => t.status === "in_progress",
                  );
                  const isCurrent =
                    isNewest && !todoTakesOverPulse && runningTurn !== null;
                  // Anchor the "current" ref to whichever phase is
                  // pulsing. When no phase is pulsing (idle session or
                  // an in-progress todo takes over), we anchor on the
                  // newest phase as a sane fallback so the initial
                  // scroll still lands at the most recent activity.
                  const anchorHere =
                    isCurrent ||
                    (isNewest &&
                      !todoTakesOverPulse &&
                      activeTodos.length === 0);
                  return (
                    <PhaseRow
                      key={phase.index}
                      phase={phase}
                      index={i}
                      isFirst={isOldest}
                      isLast={
                        isNewest &&
                        activeTodos.length === 0 &&
                        forecast.length === 0
                      }
                      branches={branchesByPhase.get(i) ?? []}
                      runningTurn={isCurrent ? runningTurn : null}
                      isCurrent={isCurrent}
                      currentRef={anchorHere ? currentRef : undefined}
                      onClick={() => onPhaseClick(phase)}
                    />
                  );
                })}
                {activeTodos.map((todo, i) => {
                  const inProgress = todo.status === "in_progress";
                  // The first in-progress todo wins the "current"
                  // anchor. If there are no in-progress todos but
                  // pending ones, the anchor stays on the newest phase
                  // (handled above).
                  const firstInProgressIdx = activeTodos.findIndex(
                    (t) => t.status === "in_progress",
                  );
                  const anchorHere = inProgress && i === firstInProgressIdx;
                  return (
                    <TodoRow
                      key={`todo-${i}-${todo.content}`}
                      todo={todo}
                      index={i}
                      isLast={
                        i === activeTodos.length - 1 && forecast.length === 0
                      }
                      currentRef={anchorHere ? currentRef : undefined}
                    />
                  );
                })}
                {forecast.map((entry, i) => (
                  <ForecastRow
                    key={`forecast-${i}`}
                    entry={entry}
                    index={i}
                    isLast={i === forecast.length - 1}
                    hasPrior={i === 0}
                  />
                ))}
              </ol>
            </div>
            {/* Jump-to-now floating button. Only renders when the
                current step has scrolled out of view. Sits over the
                spine's lower-right corner so it is reachable without
                covering content. */}
            {!currentInView && (
              <button
                type="button"
                onClick={jumpToNow}
                className="absolute bottom-3 right-4 z-20 inline-flex items-center gap-1.5 rounded-full border border-[var(--border)] bg-[var(--bg-overlay)]/90 px-3 py-1.5 font-display text-[10px] uppercase tracking-[0.14em] text-[var(--text-muted)] shadow-lg backdrop-blur transition-colors hover:border-[var(--artemis-earth)] hover:text-[var(--artemis-white)]"
                title="Scroll back to the current step"
              >
                <Crosshair size={11} strokeWidth={1.75} />
                Jump to now
              </button>
            )}
          </div>

          {/* Legend — small, unobtrusive. */}
          <div className="mt-4 flex flex-wrap items-center gap-4 border-t border-[var(--border)] pt-3 font-mono text-[10px] text-[var(--text-muted)]">
            <span className="inline-flex items-center gap-1">
              <span
                className="inline-block h-2 w-2 rounded-full"
                style={{ background: "var(--artemis-earth)" }}
              />
              Phase
            </span>
            <span className="inline-flex items-center gap-1">
              <Radio size={10} strokeWidth={1.75} />
              Intervention branch
            </span>
            <span className="inline-flex items-center gap-1">
              <Square size={10} strokeWidth={1.75} />
              Plan · TodoWrite
            </span>
            <span className="inline-flex items-center gap-1">
              <span
                className="inline-block h-2 w-2 rounded-full border border-dashed border-[var(--text-muted)]"
                aria-hidden="true"
              />
              Forecast
            </span>
            <span className="inline-flex items-center gap-1">
              <span className="inline-block h-2 w-2 rounded-full bg-[var(--status-success)] animate-pulse" />
              Running turn
            </span>
          </div>
        </div>
      </Card>

      <PhaseDrawer
        sessionId={sessionId}
        turns={turns}
        phase={active}
        open={drawerOpen}
        onClose={() => setDrawerOpen(false)}
      />
    </div>
  );
}

// PhaseRow renders one row of the git graph: the graph column on
// the left (spine segment + node + optional branch), then a
// clickable card on the right with the phase content. Each row
// sizes to its card height; the spine segments stretch to fill
// the row so there are no gaps between consecutive nodes.
function PhaseRow({
  phase,
  index,
  isFirst,
  isLast,
  branches,
  runningTurn,
  isCurrent,
  currentRef,
  onClick,
}: {
  phase: PhaseBlock;
  index: number;
  isFirst: boolean;
  isLast: boolean;
  branches: Intervention[];
  runningTurn: Turn | null;
  isCurrent?: boolean;
  currentRef?: React.Ref<HTMLLIElement>;
  onClick: () => void;
}) {
  const tone = KIND_TONE[phase.kind] ?? KIND_TONE.other;
  const Icon = KIND_ICON[phase.kind] ?? KIND_ICON.other;

  return (
    <li ref={currentRef} className="flex min-h-[112px] gap-3">
      {/* Graph column — fixed width, full height. Draws the spine
          segment + the node circle + branches. */}
      <div className="relative flex-shrink-0" style={{ width: SPINE_X * 2 }}>
        <svg
          className="h-full w-full"
          viewBox={`0 0 ${SPINE_X * 2} 100`}
          preserveAspectRatio="xMidYMid meet"
          style={{ overflow: "visible" }}
        >
          {/* Spine (top half): from top of row to node centre. Hidden
              on the first row so the graph visually "starts" there. */}
          {!isFirst && (
            <line
              x1={SPINE_X}
              y1="0"
              x2={SPINE_X}
              y2="50"
              stroke="var(--artemis-earth)"
              strokeWidth="2"
              strokeOpacity="0.55"
            />
          )}
          {/* Spine (bottom half): from node centre to bottom of row.
              Hidden on the very last row unless there is a forecast
              tail below. */}
          {!isLast && (
            <line
              x1={SPINE_X}
              y1="50"
              x2={SPINE_X}
              y2="100"
              stroke="var(--artemis-earth)"
              strokeWidth="2"
              strokeOpacity="0.55"
            />
          )}

          {/* Phase number label to the left of the node */}
          <text
            x={SPINE_X - NODE_R - 6}
            y="54"
            textAnchor="end"
            className="fill-[var(--text-muted)] font-mono"
            style={{ fontSize: "10px" }}
          >
            {String(index + 1).padStart(2, "0")}
          </text>

          {/* Node glow */}
          <circle
            cx={SPINE_X}
            cy="50"
            r={NODE_R + 4}
            fill={tone.fill}
            opacity="0.18"
          />
          {/* Node body */}
          <circle
            cx={SPINE_X}
            cy="50"
            r={NODE_R}
            fill={tone.fill}
            opacity="0.95"
          />
          <circle
            cx={SPINE_X}
            cy="50"
            r={NODE_R}
            fill="none"
            stroke={tone.ring}
            strokeWidth="1.5"
            opacity="0.7"
          />

          {/* Running-turn indicator on the trailing phase. A pulsing
              outer ring that the css animation drives. */}
          {runningTurn && (
            <>
              <circle
                cx={SPINE_X}
                cy="50"
                r={NODE_R + 6}
                fill="none"
                stroke="var(--status-success)"
                strokeWidth="1.5"
                strokeOpacity="0.9"
              >
                <animate
                  attributeName="r"
                  values={`${NODE_R + 2};${NODE_R + 10};${NODE_R + 2}`}
                  dur="2s"
                  repeatCount="indefinite"
                />
                <animate
                  attributeName="stroke-opacity"
                  values="0.9;0;0.9"
                  dur="2s"
                  repeatCount="indefinite"
                />
              </circle>
            </>
          )}
        </svg>

        {/* Kind icon sits in the middle of the node. We use absolute
            positioning so the lucide stroke rendering stays crisp at
            any scale. */}
        <div
          className="pointer-events-none absolute flex items-center justify-center text-[var(--artemis-white)]"
          style={{
            left: SPINE_X - NODE_R,
            top: `calc(50% - ${NODE_R}px)`,
            width: NODE_R * 2,
            height: NODE_R * 2,
          }}
        >
          <Icon size={14} strokeWidth={1.75} />
        </div>
      </div>

      {/* Right-hand card: phase content + any branch chips below.
          When this row is the "current" step (running turn lives on
          this phase, no in-progress todo), the card border softly
          pulses so operators can spot the foothold even while
          scrolling through a long timeline. */}
      <div className="flex flex-1 flex-col gap-2 pb-4">
        <button
          type="button"
          onClick={onClick}
          className={`group flex flex-col items-start gap-1 rounded border bg-[var(--bg-raised)] p-3 text-left transition-colors hover:bg-[var(--bg-overlay)] ${
            isCurrent
              ? "mission-current-pulse border-[var(--status-success)]/60"
              : "border-[var(--border)]"
          }`}
        >
          <div className="flex w-full items-center justify-between gap-2">
            <span
              className="font-display text-[10px] uppercase tracking-[0.14em]"
              style={{ color: tone.ring }}
            >
              {tone.label}
            </span>
            <span className="font-mono text-[10px] text-[var(--text-muted)]">
              {phase.turn_count} turn{phase.turn_count === 1 ? "" : "s"}
              {formatDuration(phase.duration_ms)
                ? ` · ${formatDuration(phase.duration_ms)}`
                : ""}
              {" · "}
              {timeAgo(phase.started_at)}
            </span>
          </div>
          <p className="text-[13px] leading-snug text-[var(--artemis-white)]">
            {phase.headline}
          </p>
          {phase.narrative ? (
            <p className="text-[11px] leading-snug text-[var(--text-muted)]">
              {shortHeadline(phase.narrative, 180)}
            </p>
          ) : null}
          {phase.key_steps.length > 0 ? (
            <ul className="mt-1 flex flex-col gap-0.5 text-[11px] leading-snug text-[var(--text-muted)]">
              {phase.key_steps.slice(0, 3).map((step, idx) => (
                <li key={idx} className="flex gap-1">
                  <span className="text-[var(--artemis-earth)]">·</span>
                  <span>{step}</span>
                </li>
              ))}
              {phase.key_steps.length > 3 ? (
                <li className="ml-2 font-mono text-[10px] text-[var(--text-muted)]">
                  +{phase.key_steps.length - 3} more
                </li>
              ) : null}
            </ul>
          ) : null}
        </button>

        {/* Branches — one row per intervention that landed in this
            phase. Drawn as an indented flex strip so they visually
            hang off the phase card. The branch line itself is
            rendered via a small SVG inside each chip. */}
        {branches.length > 0 && (
          <ul className="flex flex-col gap-1 pl-4">
            {branches.map((iv) => (
              <li
                key={iv.intervention_id}
                className="flex items-start gap-2 rounded border border-[var(--artemis-red)]/40 bg-[var(--artemis-red)]/10 p-2"
              >
                <div className="flex h-5 w-5 flex-shrink-0 items-center justify-center rounded-full bg-[var(--artemis-red)]/30 text-[var(--artemis-red)]">
                  <Send size={10} strokeWidth={1.75} />
                </div>
                <div className="flex-1 min-w-0">
                  <p className="font-display text-[9px] uppercase tracking-[0.14em] text-[var(--artemis-red)]">
                    Side quest · {iv.delivery_mode}
                  </p>
                  <p className="text-[11px] leading-snug text-[var(--artemis-white)]">
                    {shortHeadline(iv.message, 140)}
                  </p>
                  <p className="mt-0.5 font-mono text-[9px] text-[var(--text-muted)]">
                    injected {timeAgo(iv.created_at)} · {iv.status}
                  </p>
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>
    </li>
  );
}

// TodoRow renders one row of Claude's own self-declared plan, sourced
// from the most recent TodoWrite tool call. The visual sits between
// the realised phase spine (solid) and the tier-3 forecast tail
// (dashed). An `in_progress` todo gets a solid filled node with a
// pulsing outer ring so operators can see exactly which step Claude
// thinks it is currently on; a `pending` todo gets a hollow dashed
// node because the step is declared but not started.
//
// Unlike phase nodes, todo rows are not clickable — the underlying
// span is already surfaced in the Events tab and clicking here would
// collide with the PhaseDrawer flow. The row exists to answer the
// question "what is Claude planning to do next", not to deep-link.
function TodoRow({
  todo,
  index,
  isLast,
  currentRef,
}: {
  todo: TodoItem;
  index: number;
  isLast: boolean;
  currentRef?: React.Ref<HTMLLIElement>;
}) {
  const inProgress = todo.status === "in_progress";
  const label = inProgress ? "In flight" : "Planned";
  const body =
    (inProgress && todo.active_form?.trim()) ||
    todo.content?.trim() ||
    "(no content)";

  return (
    <li ref={currentRef} className="flex min-h-[80px] gap-3">
      <div className="relative flex-shrink-0" style={{ width: SPINE_X * 2 }}>
        <svg
          className="h-full w-full"
          viewBox={`0 0 ${SPINE_X * 2} 100`}
          preserveAspectRatio="xMidYMid meet"
          style={{ overflow: "visible" }}
        >
          {/* Top spine — always drawn: todo rows always sit below a
              phase node or another todo row, so there is never a gap
              above. Use a half-dashed style (solid near the top, so
              it reads as "continuation of the real spine", fading to
              dashed near the node for "forward-looking"). */}
          <line
            x1={SPINE_X}
            y1="0"
            x2={SPINE_X}
            y2="50"
            stroke="var(--artemis-earth)"
            strokeWidth="1.75"
            strokeOpacity="0.5"
            strokeDasharray={inProgress ? undefined : "3 4"}
          />
          {/* Bottom spine — hidden on the final todo row unless a
              forecast tail is going to render below it. The parent
              passes isLast so we know when to cut. */}
          {!isLast && (
            <line
              x1={SPINE_X}
              y1="50"
              x2={SPINE_X}
              y2="100"
              stroke="var(--artemis-earth)"
              strokeWidth="1.75"
              strokeOpacity="0.5"
              strokeDasharray="3 4"
            />
          )}

          {/* Label: "TODO" column so the row visually parallels the
              numbered phase labels on PhaseRow and the "NEXT" label
              on ForecastRow. */}
          <text
            x={SPINE_X - NODE_R - 6}
            y="54"
            textAnchor="end"
            className="fill-[var(--text-muted)] font-mono"
            style={{ fontSize: "9px" }}
          >
            {inProgress ? "NOW" : `T${String(index + 1).padStart(2, "0")}`}
          </text>

          {/* In-progress nodes get a solid fill + glow so they pop on
              the spine as the current foothold. Pending nodes are
              hollow with a dashed outline. */}
          {inProgress ? (
            <>
              <circle
                cx={SPINE_X}
                cy="50"
                r={NODE_R + 3}
                fill="var(--status-success)"
                opacity="0.2"
              />
              <circle
                cx={SPINE_X}
                cy="50"
                r={NODE_R - 2}
                fill="var(--status-success)"
                opacity="0.95"
              />
              {/* Pulsing outer ring — the same SMIL cue the phase spine
                  uses for a running turn, lifted verbatim so the two
                  visuals read as the same "alive right now" signal. */}
              <circle
                cx={SPINE_X}
                cy="50"
                r={NODE_R + 6}
                fill="none"
                stroke="var(--status-success)"
                strokeWidth="1.5"
                strokeOpacity="0.9"
              >
                <animate
                  attributeName="r"
                  values={`${NODE_R + 2};${NODE_R + 10};${NODE_R + 2}`}
                  dur="2s"
                  repeatCount="indefinite"
                />
                <animate
                  attributeName="stroke-opacity"
                  values="0.9;0;0.9"
                  dur="2s"
                  repeatCount="indefinite"
                />
              </circle>
            </>
          ) : (
            <circle
              cx={SPINE_X}
              cy="50"
              r={NODE_R - 2}
              fill="var(--bg-surface)"
              stroke="var(--artemis-earth)"
              strokeWidth="1.25"
              strokeDasharray="3 3"
              strokeOpacity="0.8"
            />
          )}
        </svg>
        <div
          className="pointer-events-none absolute flex items-center justify-center"
          style={{
            left: SPINE_X - NODE_R,
            top: `calc(50% - ${NODE_R}px)`,
            width: NODE_R * 2,
            height: NODE_R * 2,
            color: inProgress ? "var(--artemis-white)" : "var(--artemis-earth)",
          }}
        >
          {inProgress ? (
            <Loader size={12} strokeWidth={1.75} className="animate-spin" />
          ) : (
            <Square size={12} strokeWidth={1.5} />
          )}
        </div>
      </div>

      <div className="flex flex-1 flex-col gap-1 pb-3">
        <div
          className={
            inProgress
              ? "mission-current-pulse rounded border border-[var(--status-success)]/60 bg-[var(--status-success)]/10 p-3"
              : "rounded border border-dashed border-[var(--artemis-earth)]/40 bg-[var(--bg-raised)]/60 p-3"
          }
        >
          <span
            className="font-display text-[10px] uppercase tracking-[0.14em]"
            style={{
              color: inProgress
                ? "var(--status-success)"
                : "var(--artemis-earth)",
            }}
          >
            {label} · todo
          </span>
          <p
            className={
              inProgress
                ? "mt-1 text-[12px] leading-snug text-[var(--artemis-white)]"
                : "mt-1 text-[12px] leading-snug text-[var(--text-muted)]"
            }
          >
            {body}
          </p>
        </div>
      </div>
    </li>
  );
}

// ForecastRow renders one row of the dashed forecast tail below the
// realised phase spine. Nodes are dashed-outline circles and the
// spine segment is a dashed line so the whole block visually reads
// as "probable but not realised".
function ForecastRow({
  entry,
  index,
  isLast,
  hasPrior,
}: {
  entry: ForecastPhase;
  index: number;
  isLast: boolean;
  hasPrior: boolean;
}) {
  const tone = KIND_TONE[entry.kind] ?? KIND_TONE.other;
  const Icon = KIND_ICON[entry.kind] ?? KIND_ICON.other;

  return (
    <li className="flex min-h-[96px] gap-3">
      <div className="relative flex-shrink-0" style={{ width: SPINE_X * 2 }}>
        <svg
          className="h-full w-full"
          viewBox={`0 0 ${SPINE_X * 2} 100`}
          preserveAspectRatio="xMidYMid meet"
          style={{ overflow: "visible" }}
        >
          {/* Top spine — always visible for forecast rows so the
              first forecast node visually connects to the last
              real phase sitting above it. */}
          {hasPrior || index > 0 ? (
            <line
              x1={SPINE_X}
              y1="0"
              x2={SPINE_X}
              y2="50"
              stroke="var(--text-muted)"
              strokeWidth="1.5"
              strokeOpacity="0.5"
              strokeDasharray="3 4"
            />
          ) : null}
          {/* Bottom spine — hidden on the final forecast node. */}
          {!isLast && (
            <line
              x1={SPINE_X}
              y1="50"
              x2={SPINE_X}
              y2="100"
              stroke="var(--text-muted)"
              strokeWidth="1.5"
              strokeOpacity="0.5"
              strokeDasharray="3 4"
            />
          )}

          {/* Label */}
          <text
            x={SPINE_X - NODE_R - 6}
            y="54"
            textAnchor="end"
            className="fill-[var(--text-muted)] font-mono"
            style={{ fontSize: "9px" }}
          >
            NEXT
          </text>

          <circle
            cx={SPINE_X}
            cy="50"
            r={NODE_R}
            fill="var(--bg-surface)"
            stroke={tone.ring}
            strokeWidth="1.25"
            strokeDasharray="3 3"
            strokeOpacity="0.7"
          />
        </svg>
        <div
          className="pointer-events-none absolute flex items-center justify-center text-[var(--text-muted)]"
          style={{
            left: SPINE_X - NODE_R,
            top: `calc(50% - ${NODE_R}px)`,
            width: NODE_R * 2,
            height: NODE_R * 2,
          }}
        >
          <Icon size={12} strokeWidth={1.5} />
        </div>
      </div>

      <div className="flex flex-1 flex-col gap-1 pb-3 opacity-80">
        <div className="rounded border border-dashed border-[var(--border)] bg-[var(--bg-raised)]/40 p-3">
          <span
            className="font-display text-[10px] uppercase tracking-[0.14em]"
            style={{ color: tone.ring }}
          >
            Next · {tone.label}
          </span>
          <p className="mt-1 text-[12px] leading-snug text-[var(--text-muted)]">
            {entry.headline}
          </p>
          {entry.rationale ? (
            <p className="mt-1 font-mono text-[10px] text-[var(--text-muted)]">
              rationale: {entry.rationale}
            </p>
          ) : null}
        </div>
      </div>
    </li>
  );
}

function MissionEmpty({
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
              {/* Active-generation state. Big spinner + elapsed-time
                  counter so operators can see the worker is still
                  running during the 5–30s Sonnet call. */}
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
              {/* Error state. Shown when the POST fails outright or
                  the safety timeout expires without a rollup refresh. */}
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

// TopicGoalBanner is one row in the per-session topic-goal stack. The
// active topic (most-recent last_seen_at) gets a brighter accent so
// the operator can tell which topic Claude is currently inside; the
// rest fade slightly to read as past or paused branches. Branched
// topics (parent_topic_id non-null) are inset and prefixed with a
// "↳" connector so the parent / child relationship is obvious without
// dragging in a full tree renderer (that lives in Phase 3b).
function TopicGoalBanner({
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
