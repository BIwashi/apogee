"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { Bell } from "lucide-react";
import {
  type AnyApogeeEvent,
  type WatchdogListResponse,
  type WatchdogSignal,
  isWatchdogEvent,
} from "../lib/api-types";
import { useEventStream } from "../lib/sse";
import { useApi } from "../lib/swr";
import WatchdogDrawer from "./WatchdogDrawer";

/**
 * WatchdogBell — header-bar bell button that surfaces watchdog anomaly
 * signals. Lives in the TopRibbon between the language picker and the
 * theme toggle. Clicking the button opens `WatchdogDrawer` with the
 * full signal list.
 *
 * Data flow:
 *   - SWR pulls /v1/watchdog/signals?status=unacked&limit=20 every
 *     30 s as the polling backstop.
 *   - The layout-scoped SSE provider streams `watchdog.signal` events;
 *     the bell merges incoming signals into local state so the badge
 *     and drawer update instantly without waiting for the next SWR
 *     refresh.
 *
 * Visual:
 *   - The badge is a small red dot rendered in the top-right corner
 *     when `unread > 0`. The badge tint flips to --status-critical
 *     when there is at least one critical signal in the unread set;
 *     otherwise it stays at --status-warning.
 *   - The bell pulses (CSS keyframes) while there is at least one
 *     critical signal. `prefers-reduced-motion` disables the animation.
 */

export default function WatchdogBell() {
  const { data, mutate } = useApi<WatchdogListResponse>(
    "/v1/watchdog/signals?status=unacked&limit=20",
    { refreshInterval: 30_000 },
  );

  // Local merge buffer so the bell can react to SSE events without
  // waiting for the next SWR refresh. The SWR cache is the source of
  // truth on a refresh; SSE pushes layered on top.
  const [extra, setExtra] = useState<WatchdogSignal[]>([]);

  const { subscribe } = useEventStream({ types: ["watchdog.signal"] });
  useEffect(() => {
    return subscribe((ev) => {
      // The hook gives us the raw envelope; narrow it through the
      // tagged union so TypeScript knows the payload shape.
      const widened = ev as AnyApogeeEvent;
      if (!isWatchdogEvent(widened)) return;
      const incoming = widened.data.signal;
      setExtra((prev) => {
        // Drop duplicates from the local buffer when SWR has caught up
        // with the same id.
        const filtered = prev.filter((s) => s.id !== incoming.id);
        return [incoming, ...filtered];
      });
    });
  }, [subscribe]);

  // Merge the SWR snapshot and the SSE buffer into a single, dedup'd
  // newest-first list. SSE entries take precedence so an ack that
  // landed locally is reflected immediately.
  const merged = useMemo<WatchdogSignal[]>(() => {
    const base = data?.signals ?? [];
    const seen = new Set<number>();
    const out: WatchdogSignal[] = [];
    for (const s of [...extra, ...base]) {
      if (seen.has(s.id)) continue;
      seen.add(s.id);
      out.push(s);
    }
    out.sort((a, b) => (b.detected_at < a.detected_at ? -1 : 1));
    return out;
  }, [data, extra]);

  const unread = useMemo(() => merged.filter((s) => !s.acknowledged), [merged]);
  const hasCritical = unread.some((s) => s.severity === "critical");
  const badgeColor = hasCritical
    ? "var(--status-critical)"
    : "var(--status-warning)";

  const [open, setOpen] = useState(false);

  // When a card inside the drawer acks a signal, update both buffers so
  // the badge count drops without a round trip.
  const onAcknowledged = useCallback(
    (next: WatchdogSignal) => {
      setExtra((prev) => prev.map((s) => (s.id === next.id ? next : s)));
      void mutate(
        (prev) =>
          prev
            ? {
                ...prev,
                signals: prev.signals.filter((s) => s.id !== next.id),
              }
            : prev,
        { revalidate: false },
      );
    },
    [mutate],
  );

  const label =
    unread.length > 0
      ? `Anomalies: ${unread.length} unread`
      : "Anomalies: none";

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        title={label}
        aria-label={label}
        className="relative inline-flex h-[28px] w-[28px] items-center justify-center rounded-md border border-[var(--border)] bg-[var(--bg-raised)] text-[var(--artemis-space)] hover:bg-[var(--bg-overlay)] hover:text-[var(--artemis-white)] focus:outline-none focus-visible:ring-1 focus-visible:ring-[var(--border-bright)]"
      >
        <Bell
          size={13}
          strokeWidth={1.5}
          className={hasCritical ? "watchdog-bell-pulse" : undefined}
        />
        {unread.length > 0 && (
          <span
            aria-hidden
            className="absolute -right-1 -top-1 inline-flex min-w-[14px] items-center justify-center rounded-full px-1 font-mono text-[9px] leading-none text-[var(--artemis-white)]"
            style={{
              background: badgeColor,
              boxShadow: "0 0 0 1px var(--bg-surface)",
              height: 14,
            }}
          >
            {unread.length > 9 ? "9+" : unread.length}
          </span>
        )}
      </button>
      <WatchdogDrawer
        open={open}
        onClose={() => setOpen(false)}
        signals={merged}
        onAcknowledged={onAcknowledged}
      />
    </>
  );
}
