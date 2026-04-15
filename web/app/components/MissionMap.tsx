"use client";

import { useCallback, useMemo, useState } from "react";
import {
  Bug,
  CheckCheck,
  Compass,
  GitCommit,
  HelpCircle,
  Lightbulb,
  Radio,
  Satellite,
  Search,
  Sparkles,
  TestTube,
  UserCog,
  Wrench,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";

import { apiUrl } from "../lib/api";
import type {
  Intervention,
  InterventionListResponse,
  PhaseBlock,
  PhaseKind,
  Rollup,
  RollupResponse,
  Turn,
} from "../lib/api-types";
import { useApi } from "../lib/swr";
import { timeAgo } from "../lib/time";
import Card from "./Card";
import PhaseDrawer from "./PhaseDrawer";
import SectionHeader from "./SectionHeader";

/**
 * MissionMap — a planetary "star map" view of a Claude Code session.
 *
 * The existing Timeline tab already renders the tier-3 phase narrative
 * as a vertical stack of PhaseCards. That is good for list-shaped
 * scanning, but it does not convey the overall arc of a session the
 * way the user has been asking for: "what are we doing, where are we
 * in the mission, and what forked off the main path?"
 *
 * MissionMap answers that by treating each session as a scaled-down
 * solar system:
 *
 *   Sun        = the user's top-level goal (tier-2 rollup headline)
 *   Planets    = tier-3 semantic phases, ordered by start_time along
 *                an orbit path
 *   Moons      = individual turns inside each phase (one satellite
 *                per turn_id)
 *   Probe      = the currently-active turn, if the session is still
 *                running — a glowing marker on the most recent planet
 *   Meteors    = operator interventions (side quests pushed into the
 *                session from the dashboard); they branch off the
 *                orbit path at the turn where they were injected
 *
 * The visualisation reuses existing data sources — /v1/sessions/:id
 * /rollup for the tier-2/3 payload, /v1/sessions/:id/interventions
 * for meteors, and the turns array the session page already loads.
 * No new backend endpoint.
 *
 * Layout decisions:
 *
 * - The orbit path is a single horizontal S-curve rendered as an SVG
 *   cubic bezier. Phases are placed along its arclength by start_at
 *   so phase-1 is leftmost and the final phase is on the right.
 * - Phase kinds map to lucide icons. The badge behind each icon is
 *   one of the NASA-Artemis palette colours (NASA Red, Earth Blue,
 *   Shadow Gray) chosen from a fixed palette so colour-to-kind
 *   stays stable across renders.
 * - Moons are distributed around each planet on a second, smaller
 *   circular orbit. The number of moons equals the number of turns
 *   in the phase; they rotate slowly via CSS keyframes.
 * - The probe for a running session sits on the trailing planet's
 *   "leading" orbit position and pulses.
 *
 * Empty state: when the tier-3 narrative has not run yet, we render
 * a callout with a "Generate narrative" button that fires the
 * existing /v1/sessions/:id/narrative POST.
 */

interface MissionMapProps {
  sessionId: string;
  turns: Turn[];
}

// Icon per PhaseKind — reuses the vocabulary already in the tier-3
// summariser prompt so the visual vocabulary matches the LLM output.
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

// Fixed palette per PhaseKind — each entry picks one of the Artemis
// palette tokens so the planet badge colour reads as "this kind of
// work" the same way the tier-1 recap page colours phases.
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
  explore: {
    fill: "var(--accent)",
    ring: "var(--accent)",
    label: "Explore",
  },
  other: {
    fill: "var(--border-bright)",
    ring: "var(--border-bright)",
    label: "Other",
  },
};

// Viewbox + orbit-path constants. The whole map is one 1200×520 SVG
// that scales to the container width. The orbit is a cubic bezier
// that gently S-curves left-to-right; planets are positioned along
// that curve by arclength fraction so evenly-spaced phases are
// visually evenly-spaced.
const VIEW_W = 1200;
const VIEW_H = 520;
const ORBIT_Y_LEFT = 340;
const ORBIT_Y_RIGHT = 260;
const ORBIT_C1 = { x: 380, y: 220 }; // first control point
const ORBIT_C2 = { x: 820, y: 380 }; // second control point

// Evaluate the cubic bezier at parameter t ∈ [0, 1].
function bezier(t: number): { x: number; y: number } {
  const mt = 1 - t;
  const x =
    mt ** 3 * 120 +
    3 * mt ** 2 * t * ORBIT_C1.x +
    3 * mt * t ** 2 * ORBIT_C2.x +
    t ** 3 * 1080;
  const y =
    mt ** 3 * ORBIT_Y_LEFT +
    3 * mt ** 2 * t * ORBIT_C1.y +
    3 * mt * t ** 2 * ORBIT_C2.y +
    t ** 3 * ORBIT_Y_RIGHT;
  return { x, y };
}

// Deterministic "random" offset for moon placement so the moons of
// a given turn always land in the same spot across re-renders.
function hashFloat(seed: string): number {
  let h = 2166136261;
  for (let i = 0; i < seed.length; i++) {
    h ^= seed.charCodeAt(i);
    h = Math.imul(h, 16777619);
  }
  return ((h >>> 0) % 10_000) / 10_000;
}

interface InterventionRef {
  intervention: Intervention;
  phaseIndex: number;
  angle: number;
}

function interventionsByPhase(
  interventions: Intervention[],
  phases: PhaseBlock[],
): InterventionRef[] {
  const refs: InterventionRef[] = [];
  for (const iv of interventions) {
    if (!iv.created_at) continue;
    let idx = -1;
    for (let i = 0; i < phases.length; i++) {
      const p = phases[i];
      if (iv.created_at >= p.started_at && iv.created_at <= p.ended_at) {
        idx = i;
        break;
      }
    }
    if (idx < 0) continue;
    refs.push({
      intervention: iv,
      phaseIndex: idx,
      angle: hashFloat(iv.intervention_id || iv.created_at) * Math.PI * 2,
    });
  }
  return refs;
}

export default function MissionMap({ sessionId, turns }: MissionMapProps) {
  const { data: rollupData, mutate } = useApi<RollupResponse>(
    sessionId ? `/v1/sessions/${sessionId}/rollup` : null,
    { refreshInterval: 10_000 },
  );
  const { data: interventionsData } = useApi<InterventionListResponse>(
    sessionId ? `/v1/sessions/${sessionId}/interventions?limit=200` : null,
    { refreshInterval: 10_000 },
  );

  const rollup: Rollup | null = rollupData?.rollup ?? null;
  const phases: PhaseBlock[] = useMemo(
    () => rollup?.phases ?? [],
    [rollup],
  );
  const interventions = useMemo(
    () => interventionsData?.interventions ?? [],
    [interventionsData],
  );
  const meteors = useMemo(
    () => interventionsByPhase(interventions, phases),
    [interventions, phases],
  );

  const runningTurn = useMemo(
    () => turns.find((t) => String(t.status) === "running") ?? null,
    [turns],
  );

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

  if (!rollup) {
    return (
      <MissionEmpty
        title="Mission not yet charted"
        body="A session rollup is needed before the mission map can render. The rollup worker runs automatically once at least two turns have closed, or you can trigger it manually."
        buttonLabel="Generate narrative"
        pending={pending}
        onGenerate={onGenerate}
      />
    );
  }

  if (phases.length === 0) {
    return (
      <MissionEmpty
        title="No phase narrative yet"
        body="The mission map plots one planet per semantic phase from the tier-3 narrative. That worker has not run for this session yet."
        buttonLabel="Generate narrative"
        pending={pending}
        onGenerate={onGenerate}
      />
    );
  }

  // Arclength-fraction positions for each planet along the orbit.
  // t=0 is the left edge (mission start), t=1 is the right edge
  // (mission tail). With N phases we space them evenly on the curve
  // but shifted inward by a padding so the first and last planets
  // sit a little inside the frame.
  const n = phases.length;
  const pad = n === 1 ? 0.5 : 0.08;
  const positions = phases.map((_, i) =>
    bezier(pad + (i * (1 - 2 * pad)) / Math.max(1, n - 1)),
  );
  const orbitPath = `M 120 ${ORBIT_Y_LEFT} C ${ORBIT_C1.x} ${ORBIT_C1.y}, ${ORBIT_C2.x} ${ORBIT_C2.y}, 1080 ${ORBIT_Y_RIGHT}`;

  // Sun label is the rollup headline. Fall back to the raw narrative
  // if the headline is empty so we never render a blank sun.
  const sunLabel = rollup.headline || rollup.narrative.slice(0, 80) || "Mission";

  return (
    <div className="flex flex-col gap-4">
      <SectionHeader
        title="Mission map"
        subtitle="Planetary view of the session arc. Sun = top-level goal, planets = phases, moons = turns."
        actions={
          <button
            type="button"
            onClick={onGenerate}
            disabled={pending}
            className="inline-flex items-center gap-2 rounded-md border border-[var(--border)] bg-[var(--bg-raised)] px-3 py-1.5 font-display text-[10px] uppercase tracking-[0.16em] text-[var(--text-muted)] transition-colors hover:bg-[var(--bg-overlay)] hover:text-[var(--artemis-white)] disabled:cursor-not-allowed disabled:opacity-60"
            title="Re-run the tier-3 narrative worker"
          >
            <Sparkles size={12} strokeWidth={1.75} />
            {pending ? "Charting…" : "Re-chart"}
          </button>
        }
      />

      <Card className="relative overflow-hidden p-0">
        {/* Starfield — a CSS-rendered deep-space background. The
            radial gradient gives the sense that the viewer is
            looking out from inside the session toward its horizon. */}
        <div className="mission-starfield pointer-events-none absolute inset-0" aria-hidden="true" />

        <svg
          viewBox={`0 0 ${VIEW_W} ${VIEW_H}`}
          className="relative z-10 h-auto w-full"
          role="img"
          aria-label={`Mission map with ${phases.length} phases and ${meteors.length} interventions`}
        >
          <defs>
            {/* Orbit path fades at the edges so the planets appear
                to emerge from deep space. */}
            <linearGradient id="orbit-gradient" x1="0" x2="1" y1="0" y2="0">
              <stop offset="0" stopColor="var(--artemis-earth)" stopOpacity="0" />
              <stop offset="0.15" stopColor="var(--artemis-earth)" stopOpacity="0.45" />
              <stop offset="0.85" stopColor="var(--artemis-earth)" stopOpacity="0.45" />
              <stop offset="1" stopColor="var(--artemis-earth)" stopOpacity="0" />
            </linearGradient>
            <radialGradient id="sun-gradient" cx="0.5" cy="0.5" r="0.5">
              <stop offset="0" stopColor="#ffffff" stopOpacity="1" />
              <stop offset="0.35" stopColor="var(--artemis-red)" stopOpacity="0.95" />
              <stop offset="0.7" stopColor="var(--artemis-red)" stopOpacity="0.4" />
              <stop offset="1" stopColor="var(--artemis-red)" stopOpacity="0" />
            </radialGradient>
            <filter id="planet-glow" x="-50%" y="-50%" width="200%" height="200%">
              <feGaussianBlur stdDeviation="6" />
            </filter>
          </defs>

          {/* Orbit path */}
          <path
            d={orbitPath}
            fill="none"
            stroke="url(#orbit-gradient)"
            strokeWidth="1.5"
            strokeDasharray="4 4"
          />

          {/* Sun — the top-level goal */}
          <g transform="translate(90, 90)">
            <circle r="52" fill="url(#sun-gradient)" filter="url(#planet-glow)" />
            <circle r="30" fill="var(--artemis-red)" opacity="0.9" />
            <circle r="30" fill="none" stroke="#fff" strokeOpacity="0.45" strokeWidth="1" />
          </g>
          <text
            x="90"
            y="170"
            textAnchor="middle"
            className="fill-[var(--artemis-white)] font-display text-[11px] uppercase"
            style={{ letterSpacing: "0.14em" }}
          >
            MISSION
          </text>

          {/* Planets */}
          {phases.map((phase, i) => {
            const { x, y } = positions[i];
            const tone = KIND_TONE[phase.kind] ?? KIND_TONE.other;
            const Icon = KIND_ICON[phase.kind] ?? KIND_ICON.other;
            const radius = Math.max(22, Math.min(40, 18 + phase.turn_count * 3));
            const isLast = i === phases.length - 1;
            return (
              <g
                key={phase.index}
                transform={`translate(${x}, ${y})`}
                className="cursor-pointer transition-opacity hover:opacity-90"
                onClick={() => onPhaseClick(phase)}
              >
                {/* Glow halo */}
                <circle
                  r={radius + 12}
                  fill={tone.fill}
                  opacity="0.18"
                  filter="url(#planet-glow)"
                />
                {/* Planet body */}
                <circle r={radius} fill={tone.fill} opacity="0.92" />
                <circle
                  r={radius}
                  fill="none"
                  stroke={tone.ring}
                  strokeWidth="1.5"
                  opacity="0.6"
                />
                {/* Icon mount (we draw the icon as a foreignObject
                    so it inherits the lucide stroke style without
                    having to hand-code every SVG path). */}
                <foreignObject
                  x={-radius / 2}
                  y={-radius / 2}
                  width={radius}
                  height={radius}
                >
                  <div className="flex h-full w-full items-center justify-center text-[var(--artemis-white)]">
                    <Icon size={Math.max(14, radius - 14)} strokeWidth={1.75} />
                  </div>
                </foreignObject>
                {/* Moons — one satellite per turn in the phase */}
                {phase.turn_ids.slice(0, 8).map((turnId, moonIdx) => {
                  const moonR = radius + 14;
                  const total = Math.min(8, phase.turn_ids.length);
                  const a =
                    (moonIdx / total) * Math.PI * 2 +
                    hashFloat(phase.index + ":" + turnId) * Math.PI * 0.3;
                  const mx = Math.cos(a) * moonR;
                  const my = Math.sin(a) * moonR * 0.5;
                  return (
                    <circle
                      key={turnId}
                      cx={mx}
                      cy={my}
                      r="2.5"
                      fill="var(--artemis-white)"
                      opacity="0.75"
                    />
                  );
                })}
                {/* Probe marker for the running turn */}
                {isLast && runningTurn && (
                  <g transform={`translate(${radius + 6}, ${-radius - 6})`}>
                    <circle r="7" fill="var(--status-success)" opacity="0.25" />
                    <circle r="3.5" fill="var(--status-success)">
                      <animate
                        attributeName="opacity"
                        values="1;0.3;1"
                        dur="1.6s"
                        repeatCount="indefinite"
                      />
                    </circle>
                  </g>
                )}
                {/* Phase label — headline under the planet */}
                <text
                  x="0"
                  y={radius + 22}
                  textAnchor="middle"
                  className="fill-[var(--artemis-white)] font-display text-[9px] uppercase"
                  style={{ letterSpacing: "0.14em" }}
                >
                  {String(i + 1).padStart(2, "0")} · {tone.label}
                </text>
                <foreignObject
                  x={-90}
                  y={radius + 30}
                  width="180"
                  height="44"
                >
                  <p className="text-center text-[10px] leading-snug text-[var(--text-muted)]">
                    {phase.headline}
                  </p>
                </foreignObject>
              </g>
            );
          })}

          {/* Meteors — interventions branching off the phase they
              were injected into. Drawn as a NASA Red arc leaving
              the planet at the hashed angle with a small head. */}
          {meteors.map((m, i) => {
            const { x, y } = positions[m.phaseIndex];
            const radius = Math.max(
              22,
              Math.min(40, 18 + (phases[m.phaseIndex]?.turn_count ?? 1) * 3),
            );
            const len = 52;
            const mx = x + Math.cos(m.angle) * (radius + len);
            const my = y + Math.sin(m.angle) * (radius + len) * 0.8;
            const cx = x + Math.cos(m.angle) * (radius + len * 0.45);
            const cy = y + Math.sin(m.angle) * (radius + len * 0.45) * 0.4;
            return (
              <g key={m.intervention.intervention_id || i}>
                <path
                  d={`M ${x + Math.cos(m.angle) * radius} ${
                    y + Math.sin(m.angle) * radius * 0.8
                  } Q ${cx} ${cy} ${mx} ${my}`}
                  fill="none"
                  stroke="var(--artemis-red)"
                  strokeOpacity="0.85"
                  strokeWidth="1.5"
                  strokeLinecap="round"
                />
                <circle cx={mx} cy={my} r="3" fill="var(--artemis-red)" />
              </g>
            );
          })}
        </svg>

        {/* Overlay caption for the sun */}
        <div className="absolute left-4 top-4 max-w-[240px] text-[11px] text-[var(--artemis-white)]">
          <p className="font-display text-[9px] uppercase tracking-[0.16em] text-[var(--artemis-red)]">
            Top-level goal
          </p>
          <p className="mt-1 leading-snug">{sunLabel}</p>
          {rollup.narrative_generated_at ? (
            <p className="mt-1 font-mono text-[9px] text-[var(--text-muted)]">
              charted {timeAgo(rollup.narrative_generated_at)} ·{" "}
              {rollup.narrative_model || rollup.model}
            </p>
          ) : null}
        </div>

        {/* Legend in the bottom-right */}
        <div className="absolute bottom-3 right-4 flex items-center gap-3 text-[9px] uppercase tracking-[0.12em] text-[var(--text-muted)]">
          <span className="flex items-center gap-1">
            <span
              className="inline-block h-2 w-2 rounded-full"
              style={{ background: "var(--artemis-red)" }}
            />
            Sun
          </span>
          <span className="flex items-center gap-1">
            <Satellite size={10} strokeWidth={1.75} />
            Phase
          </span>
          <span className="flex items-center gap-1">
            <Radio size={10} strokeWidth={1.75} />
            Intervention
          </span>
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

function MissionEmpty({
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
    <div className="flex flex-col gap-4">
      <SectionHeader
        title="Mission map"
        subtitle="Planetary view of the session arc."
      />
      <Card className="relative overflow-hidden p-10">
        <div className="mission-starfield pointer-events-none absolute inset-0" aria-hidden="true" />
        <div className="relative z-10 mx-auto flex max-w-[520px] flex-col items-center gap-4 text-center">
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
            onClick={onGenerate}
            disabled={pending}
            className="mt-2 inline-flex items-center gap-2 rounded border border-[var(--accent)]/40 bg-[var(--accent)]/15 px-4 py-2 font-display text-[11px] uppercase tracking-[0.16em] text-[var(--artemis-white)] transition-colors hover:bg-[var(--accent)]/25 disabled:cursor-not-allowed disabled:opacity-60"
          >
            <Sparkles size={12} strokeWidth={1.75} />
            {pending ? "Charting…" : buttonLabel}
          </button>
        </div>
      </Card>
    </div>
  );
}
