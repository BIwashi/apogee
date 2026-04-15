"use client";

import { useState } from "react";
import { ArrowUpCircle, Loader2 } from "lucide-react";

import type { ApogeeInfo } from "../lib/api-types";
import { useApi } from "../lib/swr";

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
