"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Crosshair, Loader, Radio, Sparkles, Square } from "lucide-react";
import type {
  ForecastPhase,
  InterventionListResponse,
  PhaseBlock,
  Rollup,
  RollupResponse,
  SessionTodosResponse,
  SessionTopic,
  SessionTopicsResponse,
  Turn,
} from "../lib/api-types";
import { useApi } from "../lib/swr";
import { timeAgo } from "../lib/time";
import Card from "./Card";
import PhaseDrawer from "./PhaseDrawer";
import SectionHeader from "./SectionHeader";
import ForecastRow from "./mission-map/ForecastRow";
import MissionEmpty from "./mission-map/MissionEmpty";
import PhaseRow from "./mission-map/PhaseRow";
import TodoRow from "./mission-map/TodoRow";
import TopicGoalBanner from "./mission-map/TopicGoalBanner";
import { useNarrativeGeneration } from "./mission-map/useNarrativeGeneration";
import { bucketInterventions } from "./mission-map/utils";

/**
 * MissionMap — the orchestration component for the session-arc
 * git-graph view. Owns SWR fetches (rollup / interventions / todos /
 * topics), derived state (phases / forecast / branches / running
 * turn), and the bounded scroll + jump-to-now plumbing. The
 * per-row presentational components live under
 * `./mission-map/{PhaseRow,TodoRow,ForecastRow,TopicGoalBanner,MissionEmpty}.tsx`
 * and the narrative-worker async state machine in
 * `./mission-map/useNarrativeGeneration.ts`.
 *
 * Visual flavour: the card ships with a CSS-only deep-space
 * starfield background (`.mission-starfield`) so the git graph
 * still reads as "mission" even though the planets are gone.
 *
 * The earlier planetary orbit rendering (Sun / Planets / Moons) was
 * visually distinctive but structurally redundant with the Timeline
 * tab: both were flat lists of phase headlines with a subtle layout
 * flourish. The "Mission" name is good — the metaphor is a mission
 * with a main line of progress, side-quests that branch off when
 * something unexpected comes up, and future stops that have not been
 * reached yet. The right visual for that is a git graph, not a
 * solar system.
 */

interface MissionMapProps {
  sessionId: string;
  turns: Turn[];
  /**
   * When true, return null instead of the MissionEmpty placeholder.
   * Used on the Live page where the empty state is distracting —
   * the Mission section should appear silently once the narrative
   * worker produces a rollup, not advertise its absence.
   */
  hideEmpty?: boolean;
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
  // Model-declared plan: pending + in-progress rows from the most
  // recent TodoWrite call. Completed items are dropped because the
  // phase spine above already tells that story. Order is preserved
  // (Claude writes the list in execution order); in-progress items
  // get a solid node + pulsing ring, pending items render dashed.
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
  // who scrolled away on purpose would hate having the page yank
  // back every 5 s.
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

  // Narrative generation progress tracking. The tier-3 narrative
  // worker runs asynchronously: POST returns 202 immediately, the
  // actual Sonnet call takes 5-30s in the background. The hook
  // owns the lifecycle so the empty-state card and the Re-chart
  // button can both observe it from one place.
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

  // Per-session topic forest written by the per-turn topic
  // classifier. When non-empty we render one Mission Goal banner per
  // topic instead of the single session-wide rollup headline, since
  // the rollup assumes one workstream per session and a multi-topic
  // session breaks that assumption.
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
        {/* Starfield — CSS-only deep-space background. */}
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
              phase first, newest phase last, then TodoWrite "now"
              rows, then the tier-3 forecast tail. The whole spine
              sits inside a bounded-height scroll container so the
              Mission card cannot push the rest of the page when a
              session has many phases. */}
          <div className="relative">
            <div
              ref={scrollRef}
              className="mission-spine-scroll relative max-h-[60vh] overflow-y-auto pr-2"
            >
              <ol className="flex flex-col">
                {phases.map((phase, i, arr) => {
                  const isOldest = i === 0;
                  const isNewest = i === arr.length - 1;
                  const todoTakesOverPulse = activeTodos.some(
                    (t) => t.status === "in_progress",
                  );
                  const isCurrent =
                    isNewest && !todoTakesOverPulse && runningTurn !== null;
                  // Anchor the "current" ref to whichever phase is
                  // pulsing. When no phase is pulsing (idle session
                  // or an in-progress todo takes over), we anchor on
                  // the newest phase as a sane fallback so the
                  // initial scroll still lands at the most recent
                  // activity.
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
                  // anchor.
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
                current step has scrolled out of view. */}
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
