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
    bg: "rgb(252 61 33 / 0.15)",
    fg: "var(--status-critical)",
    border: "rgb(252 61 33 / 0.3)",
  },
  warning: {
    bg: "rgb(224 139 39 / 0.15)",
    fg: "var(--status-warning)",
    border: "rgb(224 139 39 / 0.3)",
  },
  success: {
    bg: "rgb(39 224 161 / 0.15)",
    fg: "var(--status-success)",
    border: "rgb(39 224 161 / 0.3)",
  },
  info: {
    bg: "rgb(39 170 225 / 0.15)",
    fg: "var(--status-info)",
    border: "rgb(39 170 225 / 0.3)",
  },
  muted: {
    bg: "rgb(88 89 91 / 0.18)",
    fg: "var(--artemis-space)",
    border: "rgb(88 89 91 / 0.35)",
  },
  earth: {
    bg: "rgb(39 170 225 / 0.12)",
    fg: "var(--artemis-earth)",
    border: "rgb(39 170 225 / 0.28)",
  },
  accent: {
    // accent renders as a subtle gradient border w/ neutral fill
    bg: "rgb(11 61 145 / 0.18)",
    fg: "#C8D5F5",
    border: "rgb(39 170 225 / 0.45)",
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
