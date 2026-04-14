import { Radio } from "lucide-react";

import Card from "./components/Card";
import EventTypeBadge from "./components/EventTypeBadge";
import SectionHeader from "./components/SectionHeader";
import StatusPill from "./components/StatusPill";
import { sessionPalette } from "./lib/design-tokens";
import { EVENT_TYPES } from "./lib/event-types";

/**
 * Landing page — serves double duty as the Overview route and as the
 * design-system showcase. It renders without any backend so `next build`
 * stays self-contained in this scaffold PR.
 */

export default function Page() {
  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-10">
      {/* ── Hero ───────────────────────────────────────── */}
      <header className="pt-6">
        <h1 className="font-display text-5xl leading-none tracking-[0.16em] text-white md:text-6xl">
          APOGEE
        </h1>
        <div className="accent-gradient-bar mt-4 h-[3px] w-40 rounded-full" />
        <p className="mt-4 max-w-2xl text-[15px] text-[var(--text-muted)]">
          The highest vantage point over your Claude Code agents. apogee
          captures every hook event from every session, stores them in DuckDB,
          and streams them live to this console.
        </p>
      </header>

      {/* ── Status pills ───────────────────────────────── */}
      <section>
        <SectionHeader
          title="Status palette"
          subtitle="Five semantic tones that every apogee surface speaks."
        />
        <Card>
          <div className="flex flex-wrap items-center gap-3">
            <StatusPill tone="critical">critical — tool failure</StatusPill>
            <StatusPill tone="warning">warning — permission requested</StatusPill>
            <StatusPill tone="success">success — session complete</StatusPill>
            <StatusPill tone="info">info — tool invoked</StatusPill>
            <StatusPill tone="muted">muted — idle</StatusPill>
          </div>
        </Card>
      </section>

      {/* ── Event catalogue ────────────────────────────── */}
      <section>
        <SectionHeader
          title="Hook events"
          subtitle="Twelve Claude Code hook events, each with an assigned icon and tone."
        />
        <Card>
          <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3">
            {EVENT_TYPES.map((spec) => (
              <EventTypeBadge key={spec.id} spec={spec} />
            ))}
          </div>
        </Card>
      </section>

      {/* ── Session palette ────────────────────────────── */}
      <section>
        <SectionHeader
          title="Session palette"
          subtitle="Ten OKLCH-derived colors for per-session chart series."
        />
        <Card>
          <div className="flex flex-wrap items-center gap-4">
            {sessionPalette.map((hex, i) => (
              <div key={hex} className="flex flex-col items-center gap-1.5">
                <span
                  className="block h-10 w-10 rounded-full border border-[var(--border-bright)]"
                  style={{ background: hex }}
                  aria-label={`Session color ${i + 1}`}
                />
                <code className="font-mono text-[10px] text-[var(--text-muted)]">
                  {hex}
                </code>
              </div>
            ))}
          </div>
        </Card>
      </section>

      {/* ── Live pulse placeholder ─────────────────────── */}
      <section>
        <SectionHeader
          title="Live pulse"
          subtitle="Event stream from the collector will replace this card."
        />
        <Card>
          <div className="flex flex-col items-center justify-center gap-3 py-10 text-center">
            <div className="rounded-full border border-[var(--border-bright)] bg-[var(--bg-raised)] p-3">
              <Radio
                size={22}
                strokeWidth={1.5}
                className="text-[var(--artemis-earth)]"
              />
            </div>
            <p className="font-display text-[12px] text-white">No events yet</p>
            <p className="max-w-sm text-[12px] text-[var(--text-muted)]">
              Start the collector and point your Claude Code hooks at
              <code className="mx-1 font-mono text-[11px] text-[var(--artemis-earth)]">
                http://localhost:8000/ingest
              </code>
              to see events appear here in real time.
            </p>
          </div>
        </Card>
      </section>

      <footer className="pb-8 pt-4">
        <p className="font-mono text-[10px] text-[var(--text-muted)]">
          apogee 0.0.0-dev — scaffold preview
        </p>
      </footer>
    </div>
  );
}
