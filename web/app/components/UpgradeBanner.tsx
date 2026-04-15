"use client";

import { useEffect, useState } from "react";
import { ArrowUpCircle, Loader2, Timer } from "lucide-react";

import type { ApogeeInfo } from "../lib/api-types";
import { useApi } from "../lib/swr";

// formatDuration turns a second count into "m:ss" (or "Xs" below a
// minute) for the auto-restart countdown. Non-positive values render
// as "now" so the label does not flicker to "-1s" between the tick
// expiry and the page reload.
function formatDuration(sec: number): string {
  if (sec <= 0) return "now";
  if (sec < 60) return `${sec}s`;
  const m = Math.floor(sec / 60);
  const s = sec % 60;
  return `${m}:${s.toString().padStart(2, "0")}`;
}

/**
 * UpgradeBanner — a slim coloured stripe that renders above the TopRibbon
 * when the collector's upgrade-watcher has noticed a newer apogee binary
 * on disk. The trigger is typically `brew upgrade apogee`: the file at
 * the running binary's path changes mtime, the watcher shells out to
 * `<path> version`, and /v1/info starts reporting `update_available` /
 * `available_version`. Clicking "Restart now" POSTs to
 * /v1/daemon/restart, which fires `launchctl kickstart -k` (macOS) or
 * `systemctl --user restart apogee.service` (linux) so the supervisor
 * relaunches the daemon into the new binary.
 *
 * The banner is intentionally non-blocking: it never modal-traps the
 * user, the rest of the dashboard stays interactive, and a failed
 * restart leaves the running process untouched. Polling cadence is
 * 30 s — the banner is informational, not real-time.
 */
export default function UpgradeBanner() {
  const { data } = useApi<ApogeeInfo>("/v1/info", { refreshInterval: 30_000 });
  const [restarting, setRestarting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Local 1-second tick so the auto-restart countdown visibly updates
  // even though /v1/info only polls every 30s. We seed from the
  // server-reported seconds and decrement locally; the next info poll
  // re-syncs if we drifted.
  const serverSeconds = data?.auto_restart_in_seconds;
  const [localRemaining, setLocalRemaining] = useState<number | null>(
    typeof serverSeconds === "number" ? serverSeconds : null,
  );
  useEffect(() => {
    if (typeof serverSeconds === "number") {
      setLocalRemaining(serverSeconds);
    }
  }, [serverSeconds]);
  useEffect(() => {
    if (localRemaining === null || localRemaining <= 0) return;
    const handle = window.setInterval(() => {
      setLocalRemaining((n) => (n !== null && n > 0 ? n - 1 : n));
    }, 1000);
    return () => window.clearInterval(handle);
  }, [localRemaining]);

  if (!data || !data.update_available) {
    return null;
  }

  // Defensive: if available_version somehow matches the running version
  // (e.g. reset state mid-upgrade), do not show a banner that asks the
  // user to restart into the same build.
  if (data.available_version && data.available_version === data.version) {
    return null;
  }

  async function handleRestart() {
    setError(null);
    setRestarting(true);
    try {
      const res = await fetch("/v1/daemon/restart", { method: "POST" });
      if (!res.ok) {
        throw new Error(`HTTP ${res.status}`);
      }
      // The daemon is about to die. Give the supervisor a moment to
      // relaunch the new binary, then reload the page so the dashboard
      // re-bootstraps against the fresh build.
      setTimeout(() => {
        window.location.reload();
      }, 4000);
    } catch (err) {
      setRestarting(false);
      setError(err instanceof Error ? err.message : String(err));
    }
  }

  return (
    <div
      role="status"
      aria-live="polite"
      className="border-b border-[var(--accent)]/40 bg-[var(--accent)]/15 px-4 py-2 text-[12px] text-[var(--artemis-white)]"
    >
      <div className="flex items-center gap-3">
        <ArrowUpCircle
          size={14}
          strokeWidth={1.75}
          className="flex-shrink-0 text-[var(--accent)]"
        />
        <div className="flex-1">
          <span className="font-display text-[10px] uppercase tracking-[0.16em] text-[var(--accent)]">
            Update available
          </span>
          <span className="ml-2 text-[var(--artemis-white)]">
            apogee {data.available_version ?? "(new build)"} is installed on
            disk. Restart the daemon to load it.
          </span>
          {data.auto_restart_enabled && localRemaining !== null ? (
            <span className="ml-2 inline-flex items-center gap-1 text-[var(--text-muted)]">
              <Timer size={11} strokeWidth={1.75} />
              auto-restart in {formatDuration(localRemaining)}
            </span>
          ) : null}
          {error && (
            <span className="ml-2 text-[var(--status-critical)]">
              · restart failed: {error}
            </span>
          )}
        </div>
        <button
          type="button"
          onClick={handleRestart}
          disabled={restarting}
          className="inline-flex items-center gap-1.5 rounded-md border border-[var(--accent)]/60 bg-[var(--accent)]/20 px-3 py-1 font-display text-[10px] uppercase tracking-[0.16em] text-[var(--artemis-white)] transition-colors hover:bg-[var(--accent)]/30 disabled:cursor-not-allowed disabled:opacity-60"
        >
          {restarting ? (
            <>
              <Loader2 size={12} strokeWidth={1.75} className="animate-spin" />
              Restarting…
            </>
          ) : (
            <>Restart now</>
          )}
        </button>
      </div>
    </div>
  );
}
