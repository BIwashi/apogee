"use client";

import type { ComponentType, ReactNode } from "react";
import {
  ArrowRightCircle,
  CheckCheck,
  CheckCircle2,
  Clock,
  GitCommit,
  Layers,
  Loader,
  Octagon,
  Plus,
  Shuffle,
  XCircle,
} from "lucide-react";
import type {
  Intervention,
  InterventionMode,
  InterventionScope,
  InterventionStatus,
  InterventionUrgency,
} from "../lib/api-types";

/**
 * Shared visual helpers for the operator intervention UI. Centralises the
 * status → {dot, icon}, mode → icon, and scope → icon maps so the queue
 * and timeline components render chips with identical language.
 */

type IconComponent = ComponentType<{
  size?: number | string;
  strokeWidth?: number | string;
  color?: string;
  className?: string;
}>;

export const STATUS_META: Record<
  InterventionStatus,
  { color: string; icon: IconComponent; label: string }
> = {
  queued: {
    color: "var(--status-warning)",
    icon: Loader,
    label: "queued",
  },
  claimed: {
    color: "var(--artemis-earth)",
    icon: ArrowRightCircle,
    label: "claimed",
  },
  delivered: {
    color: "var(--status-info)",
    icon: CheckCircle2,
    label: "delivered",
  },
  consumed: {
    color: "var(--status-success)",
    icon: CheckCheck,
    label: "consumed",
  },
  expired: {
    color: "var(--status-muted)",
    icon: Clock,
    label: "expired",
  },
  cancelled: {
    color: "var(--status-muted)",
    icon: XCircle,
    label: "cancelled",
  },
};

export const MODE_ICON: Record<InterventionMode, IconComponent> = {
  interrupt: Octagon,
  context: Plus,
  both: Shuffle,
};

export const SCOPE_ICON: Record<InterventionScope, IconComponent> = {
  this_turn: GitCommit,
  this_session: Layers,
};

export const URGENCY_RANK: Record<InterventionUrgency, number> = {
  high: 0,
  normal: 1,
  low: 2,
};

export function urgencyAccent(urgency: InterventionUrgency): string {
  switch (urgency) {
    case "high":
      return "var(--status-critical)";
    case "normal":
      return "var(--status-warning)";
    case "low":
    default:
      return "var(--border-bright)";
  }
}

/**
 * sortInterventions — urgency desc (high first), then created_at asc.
 */
export function sortInterventions(list: Intervention[]): Intervention[] {
  return [...list].sort((a, b) => {
    const ra = URGENCY_RANK[a.urgency] ?? 3;
    const rb = URGENCY_RANK[b.urgency] ?? 3;
    if (ra !== rb) return ra - rb;
    return a.created_at.localeCompare(b.created_at);
  });
}

/**
 * computeStaleness derives a queued-state staleness warning. Pure: accepts
 * the intervention and a reference `now` so callers can drive a single
 * 1Hz tick without each row holding its own interval.
 */
export interface StalenessState {
  tone: "warning" | "critical" | null;
  label: string;
  seconds: number;
}

export function computeStaleness(
  iv: Intervention,
  nowMs: number,
): StalenessState {
  const status = iv.status;
  if (status === "queued") {
    const created = Date.parse(iv.created_at);
    if (!Number.isFinite(created)) {
      return { tone: null, label: "", seconds: 0 };
    }
    const seconds = Math.max(0, Math.floor((nowMs - created) / 1000));
    if (seconds > 120) {
      return {
        tone: "critical",
        label: `stalled — no hook activity ${humanDuration(seconds)}`,
        seconds,
      };
    }
    if (seconds > 30) {
      return {
        tone: "warning",
        label: `waiting ${humanDuration(seconds)}`,
        seconds,
      };
    }
    return { tone: null, label: "", seconds };
  }
  if (status === "claimed") {
    const claimed = iv.claimed_at ? Date.parse(iv.claimed_at) : NaN;
    if (!Number.isFinite(claimed)) {
      return { tone: null, label: "", seconds: 0 };
    }
    const seconds = Math.max(0, Math.floor((nowMs - claimed) / 1000));
    if (seconds > 10) {
      return {
        tone: "warning",
        label: `claim stuck ${humanDuration(seconds)}`,
        seconds,
      };
    }
    return { tone: null, label: "", seconds };
  }
  return { tone: null, label: "", seconds: 0 };
}

export function humanDuration(totalSeconds: number): string {
  if (totalSeconds < 60) return `${totalSeconds}s`;
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  if (minutes < 60) {
    return seconds === 0 ? `${minutes}m` : `${minutes}m ${seconds}s`;
  }
  const hours = Math.floor(minutes / 60);
  const mins = minutes % 60;
  return mins === 0 ? `${hours}h` : `${hours}h ${mins}m`;
}

export function Chip({
  icon,
  children,
  tone = "muted",
}: {
  icon?: ReactNode;
  children: ReactNode;
  tone?: "muted" | "warning" | "critical" | "info";
}) {
  const color =
    tone === "warning"
      ? "var(--status-warning)"
      : tone === "critical"
        ? "var(--status-critical)"
        : tone === "info"
          ? "var(--status-info)"
          : "var(--text-muted)";
  return (
    <span
      className="inline-flex items-center gap-1 rounded border px-2 py-[2px] font-mono text-[10px]"
      style={{
        background: "var(--bg-overlay)",
        borderColor: "var(--border-bright)",
        color,
      }}
    >
      {icon}
      <span className="uppercase tracking-wider">{children}</span>
    </span>
  );
}
