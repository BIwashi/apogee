import { Radio } from "lucide-react";
import Card from "../components/Card";
import EventTypeBadge from "../components/EventTypeBadge";
import SectionHeader from "../components/SectionHeader";
import StatusPill from "../components/StatusPill";
import StyleguideThemeToggle from "../components/StyleguideThemeToggle";
import VersionTag from "../components/VersionTag";
import { sessionPalette } from "../lib/design-tokens";
import { EVENT_TYPES } from "../lib/event-types";

/**
 * Styleguide — design system showcase. Lives at /styleguide so the `/`
 * route can host the real live dashboard while design review still has a
 * canonical page that renders every primitive the UI consumes.
 */

export default function StyleguidePage() {
  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-10">
      <header className="pt-6">
        <h1 className="font-display-accent text-5xl leading-none tracking-[0.16em] text-[var(--artemis-white)] md:text-6xl">
          APOGEE
        </h1>
        <div className="accent-gradient-bar mt-4 h-[3px] w-40 rounded-full" />
        <p className="mt-4 max-w-2xl text-[15px] text-[var(--text-muted)]">
          Design system showcase. Every primitive the apogee UI is allowed to
          use appears on this page exactly once.
        </p>
      </header>

      <section>
        <SectionHeader
          title="Theme"
          subtitle="Flip palettes without leaving the page. The /settings page and the TopRibbon toggle drive the same state."
        />
        <Card>
          <StyleguideThemeToggle />
        </Card>
      </section>

      <section>
        <SectionHeader
          title="Status palette"
          subtitle="Five semantic tones that every apogee surface speaks."
        />
        <Card>
          <div className="flex flex-wrap items-center gap-3">
            <StatusPill tone="critical">critical — tool failure</StatusPill>
            <StatusPill tone="warning">
              warning — permission requested
            </StatusPill>
            <StatusPill tone="success">success — session complete</StatusPill>
            <StatusPill tone="info">info — tool invoked</StatusPill>
            <StatusPill tone="muted">muted — idle</StatusPill>
          </div>
        </Card>
      </section>

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

      <section>
        <SectionHeader
          title="Live pulse placeholder"
          subtitle="The / route hosts the real live dashboard; this card is just for contrast."
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
            <p className="font-display text-[12px] text-[var(--artemis-white)]">
              No events yet
            </p>
            <p className="max-w-sm text-[12px] text-[var(--text-muted)]">
              Start the collector and point your Claude Code hooks at
              <code className="mx-1 font-mono text-[11px] text-[var(--artemis-earth)]">
                http://localhost:8000/v1/events
              </code>
              to see events appear here in real time.
            </p>
          </div>
        </Card>
      </section>

      <footer className="pb-8 pt-4">
        <VersionTag suffix="design system preview" />
      </footer>
    </div>
  );
}
