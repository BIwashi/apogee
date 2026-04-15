"use client";

import { Radar } from "lucide-react";
import Card from "./Card";

/**
 * FocusCardEmpty — the zero-state variant of the live page's hero card.
 * Shown when no turn is currently running and nothing is queued for focus.
 * Points the first-time operator at the two commands they need to light
 * the collector up: `apogee serve` and `apogee init`.
 */
export default function FocusCardEmpty() {
  return (
    <Card className="flex flex-col items-center gap-4 px-6 py-16 text-center">
      <div className="rounded-full border border-[var(--border-bright)] bg-[var(--bg-raised)] p-4">
        <Radar
          size={32}
          strokeWidth={1.5}
          className="text-[var(--artemis-earth)]"
        />
      </div>
      <div className="flex flex-col gap-2">
        <p className="font-display text-[13px] tracking-[0.14em] text-[var(--artemis-white)]">
          NO LIVE TURN
        </p>
        <p className="max-w-md text-[12px] text-[var(--text-muted)]">
          Waiting for the next user turn. Start a Claude Code session that
          reports to this collector and the focus card will light up with the
          live flame graph + recap.
        </p>
      </div>
      <ol className="flex max-w-md flex-col gap-2 text-left font-mono text-[11px] text-[var(--text-muted)]">
        <li className="flex gap-2">
          <span className="text-[var(--artemis-space)]">01</span>
          <span>
            <code className="text-[var(--artemis-white)]">apogee serve</code>{" "}
            keeps the collector running.
          </span>
        </li>
        <li className="flex gap-2">
          <span className="text-[var(--artemis-space)]">02</span>
          <span>
            <code className="text-[var(--artemis-white)]">apogee init</code>{" "}
            wires Claude Code&apos;s hooks into the collector.
          </span>
        </li>
        <li className="flex gap-2">
          <span className="text-[var(--artemis-space)]">03</span>
          <span>Start a Claude Code session — the focus card tracks it.</span>
        </li>
      </ol>
    </Card>
  );
}
