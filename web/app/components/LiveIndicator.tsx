import type { StreamStatus } from "../lib/sse";

/**
 * LiveIndicator — the header status dot. Green when the SSE stream is
 * established, amber while it is (re)connecting, red on fatal error, and
 * muted grey when intentionally closed.
 */

interface LiveIndicatorProps {
  status: StreamStatus;
}

const COLOR_BY_STATUS: Record<StreamStatus, { label: string; color: string }> = {
  open: { label: "LIVE", color: "var(--status-success)" },
  connecting: { label: "CONNECTING", color: "var(--status-warning)" },
  error: { label: "DISCONNECTED", color: "var(--status-critical)" },
  closed: { label: "OFFLINE", color: "var(--status-muted)" },
};

export default function LiveIndicator({ status }: LiveIndicatorProps) {
  const { label, color } = COLOR_BY_STATUS[status];
  const pulsing = status === "open" || status === "connecting";
  return (
    <span className="inline-flex items-center gap-2 font-display text-[11px] tracking-[0.16em] text-white">
      <span className="relative inline-flex h-2 w-2">
        {pulsing && (
          <span
            className="absolute inline-flex h-full w-full animate-ping rounded-full opacity-60"
            style={{ background: color }}
            aria-hidden
          />
        )}
        <span
          className="relative inline-flex h-2 w-2 rounded-full"
          style={{ background: color }}
        />
      </span>
      <span style={{ color }}>{label}</span>
    </span>
  );
}
