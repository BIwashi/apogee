import {
  AlertOctagon,
  Bell,
  MessageSquare,
  Minimize2,
  Octagon,
  PlayCircle,
  Shield,
  StopCircle,
  UserCheck,
  Users,
  Wrench,
  type LucideIcon,
} from "lucide-react";

import type { EventToneKey, EventTypeSpec } from "../lib/event-types";

/**
 * EventTypeBadge — renders a Claude Code hook event as an icon + label pill.
 * Consumers pass an `EventTypeSpec` (from `lib/event-types.ts`) and this
 * component looks up the right lucide icon and tone-specific styling.
 */

const ICON_MAP: Record<string, LucideIcon> = {
  Wrench,
  AlertOctagon,
  MessageSquare,
  Bell,
  Shield,
  PlayCircle,
  StopCircle,
  Octagon,
  Users,
  UserCheck,
  Minimize2,
};

const TONE_STYLES: Record<EventToneKey, { bg: string; fg: string; border: string }> = {
  critical: {
    bg: "var(--tint-critical)",
    fg: "var(--status-critical)",
    border: "var(--tint-critical-border)",
  },
  warning: {
    bg: "var(--tint-warning)",
    fg: "var(--status-warning)",
    border: "var(--tint-warning-border)",
  },
  success: {
    bg: "var(--tint-success)",
    fg: "var(--status-success)",
    border: "var(--tint-success-border)",
  },
  info: {
    bg: "var(--tint-info)",
    fg: "var(--status-info)",
    border: "var(--tint-info-border)",
  },
  muted: {
    bg: "var(--tint-muted)",
    fg: "var(--artemis-space)",
    border: "var(--tint-muted-border)",
  },
  earth: {
    bg: "var(--tint-earth)",
    fg: "var(--artemis-earth)",
    border: "var(--tint-earth-border)",
  },
  accent: {
    // accent renders as a subtle gradient border w/ neutral fill
    bg: "var(--tint-accent)",
    fg: "var(--accent-foreground)",
    border: "var(--tint-accent-border)",
  },
};

interface EventTypeBadgeProps {
  spec: EventTypeSpec;
}

export default function EventTypeBadge({ spec }: EventTypeBadgeProps) {
  const Icon = ICON_MAP[spec.icon] ?? Wrench;
  const tone = TONE_STYLES[spec.tone];

  return (
    <div
      className="flex items-center gap-2 rounded-md px-2.5 py-1.5 text-[12px]"
      style={{
        background: tone.bg,
        color: tone.fg,
        border: `1px solid ${tone.border}`,
      }}
      title={spec.description}
    >
      <Icon size={14} strokeWidth={1.5} className="flex-shrink-0" />
      <span className="font-medium">{spec.label}</span>
      <span className="font-mono text-[10px] opacity-60">{spec.id}</span>
    </div>
  );
}
