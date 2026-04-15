"use client";

import type { ApogeeInfo } from "../lib/api-types";
import { useApi } from "../lib/swr";

/**
 * VersionTag — a tiny footer label that prints the running collector's
 * build version. Every page footer used to hard-code
 *
 *   "apogee 0.0.0-dev — <page hint>"
 *
 * which was both wrong (the binary ships with a real version stamp
 * baked in via ldflags at release time) and unhelpful because it
 * never updated after an upgrade. This component reads the version
 * from /v1/info (already fetched elsewhere on most pages and cached
 * by SWR) so the footer shows the actual running version, e.g.
 * "apogee v0.1.16 — events". The optional `suffix` prop is the page
 * hint (the " — <…>" trailer). Keep it short; do not pack commit
 * message fragments or PR numbers in there — those belong in the PR
 * description, not the UI chrome.
 */

interface VersionTagProps {
  suffix?: string;
}

export default function VersionTag({ suffix }: VersionTagProps) {
  const { data } = useApi<ApogeeInfo>("/v1/info", { refreshInterval: 60_000 });
  const version = data?.version ? `apogee v${data.version}` : "apogee";
  const trailer = suffix ? ` — ${suffix}` : "";
  return (
    <span className="font-mono text-[10px] text-[var(--text-muted)]">
      {version}
      {trailer}
    </span>
  );
}
