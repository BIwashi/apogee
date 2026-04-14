import type { AttentionState, AttentionTone } from "../lib/api-types";

/**
 * AttentionDot — compact pill used as the first column of the active-turns
 * table. Maps the backend-supplied tone onto the `--status-*` CSS variables
 * so the web does not need to re-derive colors. Hover text is the engine's
 * short reason string (`title` attribute, no portal, intentionally minimal).
 */

interface AttentionDotProps {
  state?: AttentionState | string;
  tone?: AttentionTone | string;
  reason?: string;
}

const LABEL_BY_STATE: Record<string, string> = {
  intervene_now: "INTERVENE",
  watch: "WATCH",
  watchlist: "WATCHLIST",
  healthy: "HEALTHY",
};

const COLOR_BY_TONE: Record<string, string> = {
  critical: "var(--status-critical)",
  warning: "var(--status-warning)",
  info: "var(--status-info)",
  success: "var(--status-success)",
  muted: "var(--status-muted)",
};

function resolveTone(
  tone: string | undefined,
  state: string | undefined,
): string {
  if (tone && COLOR_BY_TONE[tone]) return COLOR_BY_TONE[tone]!;
  switch (state) {
    case "intervene_now":
      return COLOR_BY_TONE.critical!;
    case "watch":
      return COLOR_BY_TONE.warning!;
    case "watchlist":
      return COLOR_BY_TONE.info!;
    case "healthy":
      return COLOR_BY_TONE.success!;
    default:
      return COLOR_BY_TONE.muted!;
  }
}

export default function AttentionDot({ state, tone, reason }: AttentionDotProps) {
  const effectiveState = state ?? "healthy";
  const color = resolveTone(tone, effectiveState);
  const label = LABEL_BY_STATE[effectiveState] ?? "—";
  return (
    <span
      title={reason || label}
      className="inline-flex items-center gap-2"
    >
      <span
        aria-hidden
        style={{ background: color, boxShadow: `0 0 6px ${color}` }}
        className="h-[8px] w-[8px] rounded-full"
      />
      <span
        style={{ color }}
        className="font-display text-[10px] tracking-[0.14em]"
      >
        {label}
      </span>
    </span>
  );
}
