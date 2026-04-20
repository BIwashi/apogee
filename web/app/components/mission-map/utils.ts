// Pure helpers shared across the Mission map row components.
import type { Intervention, PhaseBlock } from "../../lib/api-types";

// bucketInterventions groups operator interventions by the phase
// they landed in, using created_at timestamp overlap. Interventions
// with no matching phase (rare) are dropped — the goal is to show
// "which phase got interrupted", not a global timeline.
export function bucketInterventions(
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

// shortHeadline truncates a one-line headline to maxLen characters
// with an ellipsis. Used to keep cards from wrapping on long
// narrative text.
export function shortHeadline(input: string, max = 90): string {
  const s = input.trim();
  if (s.length <= max) return s;
  return s.slice(0, max - 1).trimEnd() + "…";
}

// formatDuration produces a compact wall-clock span (e.g. "12s",
// "3m45s", "1h12m"). Mirrors the helper the retired PhaseCard used
// so a phase's wall-clock span still surfaces next to its turn count
// on the Mission graph.
export function formatDuration(ms: number): string {
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
