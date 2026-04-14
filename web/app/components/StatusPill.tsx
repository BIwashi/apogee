import type { StatusKey } from "../lib/design-tokens";

/**
 * StatusPill — small rounded badge that renders one of the five semantic
 * status colors. Used for tags like "running", "critical", "waiting".
 */

interface StatusPillProps {
  tone: StatusKey;
  children: React.ReactNode;
}

const TONE_CLASS: Record<StatusKey, string> = {
  critical: "badge-critical",
  warning: "badge-warning",
  success: "badge-success",
  info: "badge-info",
  muted: "badge-muted",
};

export default function StatusPill({ tone, children }: StatusPillProps) {
  return (
    <span
      className={`inline-flex items-center gap-1 rounded-[4px] px-2 py-[2px] text-[11px] font-medium ${TONE_CLASS[tone]}`}
    >
      {children}
    </span>
  );
}
