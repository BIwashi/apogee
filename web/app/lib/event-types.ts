/**
 * Claude Code hook event catalogue.
 *
 * Every hook event the collector ingests is enumerated here together with its
 * human label, the status color it should render in, and the lucide icon name
 * used to identify it in the UI. Keep this list aligned with the collector's
 * ingest schema and with `docs/design-tokens.md`.
 *
 * The icon field is the lucide component name (string). Consumers import the
 * component from `lucide-react` themselves so this module stays dependency
 * free and can be reused server-side.
 */

import type { StatusKey } from "./design-tokens";

export type EventToneKey = StatusKey | "accent" | "earth";

export interface EventTypeSpec {
  /** Canonical Claude Code hook name. */
  id: string;
  /** Short human-facing label. */
  label: string;
  /** Status-token key or `accent` for the brand gradient. */
  tone: EventToneKey;
  /** lucide-react icon name. */
  icon: string;
  /** 1-line description for tooltips / docs. */
  description: string;
}

export const EVENT_TYPES: readonly EventTypeSpec[] = [
  {
    id: "PreToolUse",
    label: "Pre Tool Use",
    tone: "info",
    icon: "Wrench",
    description: "Fired before Claude Code invokes a tool.",
  },
  {
    id: "PostToolUse",
    label: "Post Tool Use",
    tone: "info",
    icon: "Wrench",
    description: "Fired after a tool completes successfully.",
  },
  {
    id: "PostToolUseFailure",
    label: "Tool Failure",
    tone: "critical",
    icon: "AlertOctagon",
    description: "A tool call returned an error or non-zero exit.",
  },
  {
    id: "UserPromptSubmit",
    label: "User Prompt",
    tone: "info",
    icon: "MessageSquare",
    description: "The user submitted a new prompt to the agent.",
  },
  {
    id: "Notification",
    label: "Notification",
    tone: "warning",
    icon: "Bell",
    description: "Claude Code surfaced a notification to the user.",
  },
  {
    id: "PermissionRequest",
    label: "Permission Request",
    tone: "warning",
    icon: "Shield",
    description: "A permission prompt is waiting on a human decision.",
  },
  {
    id: "SessionStart",
    label: "Session Start",
    tone: "earth",
    icon: "PlayCircle",
    description: "A new Claude Code session was initialised.",
  },
  {
    id: "SessionEnd",
    label: "Session End",
    tone: "muted",
    icon: "StopCircle",
    description: "A Claude Code session terminated.",
  },
  {
    id: "Stop",
    label: "Stop",
    tone: "earth",
    icon: "Octagon",
    description: "The agent signalled a stop — reached the end of its turn.",
  },
  {
    id: "SubagentStart",
    label: "Subagent Start",
    tone: "accent",
    icon: "Users",
    description: "A subagent was spawned by the parent session.",
  },
  {
    id: "SubagentStop",
    label: "Subagent Stop",
    tone: "accent",
    icon: "UserCheck",
    description: "A subagent finished its task and was reclaimed.",
  },
  {
    id: "PreCompact",
    label: "Pre Compact",
    tone: "muted",
    icon: "Minimize2",
    description: "Claude Code is about to compact the conversation history.",
  },
] as const;

export const EVENT_TYPES_BY_ID: Record<string, EventTypeSpec> =
  Object.fromEntries(EVENT_TYPES.map((e) => [e.id, e]));

export function getEventType(id: string): EventTypeSpec | undefined {
  return EVENT_TYPES_BY_ID[id];
}
