"use client";

import { useEffect, useMemo, useState } from "react";
import {
  CheckCircle2,
  Circle,
  Copy,
  Languages,
  Monitor,
  Moon,
  RotateCcw,
  Save,
  Sun,
} from "lucide-react";
import { mutate } from "swr";

import Card from "../components/Card";
import SectionHeader from "../components/SectionHeader";
import type {
  ApogeeInfo,
  ModelInfo,
  ModelsResponse,
  ModelUseCase,
  PreferencesResponse,
  SummarizerLanguage,
  SummarizerPreferences,
  TelemetryStatus,
} from "../lib/api-types";
import {
  DEFAULT_SUMMARIZER_PREFERENCES,
  patchPreferences,
  resetPreferences,
} from "../lib/preferences";
import { useApi } from "../lib/swr";
import type { Preference } from "../lib/theme";
import { useTheme } from "../lib/theme";

const SYSTEM_PROMPT_MAX = 2048;

/**
 * `/settings` — read-only collector info. Reads `/v1/info` for build
 * metadata and `/v1/telemetry/status` for the OTel exporter snapshot.
 * Daemon and hook install flows are CLI-only for now; the page points
 * the operator at the relevant commands instead of bundling install
 * UI here.
 */

function humanUptime(seconds: number): string {
  if (!seconds || seconds < 0) return "—";
  if (seconds < 60) return `${Math.floor(seconds)}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  const remMin = minutes % 60;
  if (hours < 24) return remMin ? `${hours}h${remMin}m` : `${hours}h`;
  const days = Math.floor(hours / 24);
  const remHour = hours % 24;
  return remHour ? `${days}d${remHour}h` : `${days}d`;
}

interface KVProps {
  label: string;
  value: React.ReactNode;
  mono?: boolean;
}

function KV({ label, value, mono }: KVProps) {
  return (
    <div className="grid grid-cols-[160px_1fr] items-start gap-3 border-b border-[var(--border)] py-2 text-[12px] last:border-b-0">
      <span className="font-display text-[10px] tracking-[0.14em] text-[var(--text-muted)]">
        {label}
      </span>
      <span className={mono ? "font-mono text-[11px] text-[var(--artemis-white)]" : "text-[var(--artemis-white)]"}>
        {value}
      </span>
    </div>
  );
}

export default function SettingsPage() {
  const { data: info } = useApi<ApogeeInfo>("/v1/info", {
    refreshInterval: 10_000,
  });
  const { data: telemetry } = useApi<TelemetryStatus>(
    "/v1/telemetry/status",
    { refreshInterval: 10_000 },
  );

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <header className="flex flex-wrap items-end justify-between gap-4 pt-6">
        <div>
          <h1 className="font-display text-3xl tracking-[0.16em] text-[var(--artemis-white)]">
            SETTINGS
          </h1>
          <div className="accent-gradient-bar mt-3 h-[3px] w-32 rounded-full" />
          <p className="mt-3 max-w-xl text-[13px] text-[var(--text-muted)]">
            Collector and exporter status. Read-only for now — install flows
            (daemon, hooks) are driven from the <code>apogee</code> CLI.
          </p>
        </div>
      </header>

      <AppearanceSection />

      <section>
        <SectionHeader
          title="Collector"
          subtitle="Build + runtime metadata from /v1/info."
        />
        <Card>
          <KV
            label="Name"
            value={info?.name ?? "apogee"}
            mono
          />
          <KV
            label="Version"
            value={info?.version ?? "—"}
            mono
          />
          <KV
            label="Commit"
            value={info?.commit ?? "—"}
            mono
          />
          <KV
            label="Build date"
            value={info?.build_date ?? "—"}
            mono
          />
          <KV
            label="HTTP address"
            value={info?.collector_addr ?? "—"}
            mono
          />
          <KV
            label="Uptime"
            value={humanUptime(info?.uptime_seconds ?? 0)}
            mono
          />
        </Card>
      </section>

      <section>
        <SectionHeader
          title="OTel exporter"
          subtitle="Live snapshot from /v1/telemetry/status."
        />
        <Card>
          <KV
            label="Enabled"
            value={
              <span className="inline-flex items-center gap-2">
                {telemetry?.enabled ? (
                  <CheckCircle2
                    size={14}
                    strokeWidth={1.5}
                    className="text-[var(--status-success)]"
                  />
                ) : (
                  <Circle
                    size={14}
                    strokeWidth={1.5}
                    className="text-[var(--status-muted)]"
                  />
                )}
                <span>{telemetry?.enabled ? "enabled" : "disabled"}</span>
              </span>
            }
          />
          <KV
            label="Endpoint"
            value={telemetry?.endpoint || "—"}
            mono
          />
          <KV
            label="Protocol"
            value={telemetry?.protocol || "—"}
            mono
          />
          <KV
            label="Service name"
            value={telemetry?.service_name || "—"}
            mono
          />
          <KV
            label="Sample ratio"
            value={
              telemetry?.sample_ratio !== undefined
                ? telemetry.sample_ratio.toFixed(3)
                : "—"
            }
            mono
          />
          <KV
            label="Spans exported"
            value={String(telemetry?.spans_exported_total ?? 0)}
            mono
          />
        </Card>
      </section>

      <section>
        <SectionHeader
          title="Config file"
          subtitle="Read by the collector on startup."
        />
        <Card>
          <KV
            label="Path"
            value={
              <span className="inline-flex items-center gap-2">
                <code className="text-[var(--artemis-white)]">~/.apogee/config.toml</code>
                <Copy
                  size={12}
                  strokeWidth={1.5}
                  className="text-[var(--text-muted)]"
                />
              </span>
            }
          />
        </Card>
      </section>

      <section>
        <SectionHeader
          title="Daemon"
          subtitle="Background collector installation (CLI only for now)."
        />
        <Card>
          <p className="text-[12px] text-[var(--text-muted)]">
            Install the daemon with{" "}
            <code className="font-mono text-[var(--artemis-white)]">apogee daemon install</code>.
            A dashboard installer UI will land in a follow-up.
          </p>
        </Card>
      </section>

      <section>
        <SectionHeader
          title="Hooks"
          subtitle="Claude Code hook wiring (CLI only for now)."
        />
        <Card>
          <p className="text-[12px] text-[var(--text-muted)]">
            Install hooks with{" "}
            <code className="font-mono text-[var(--artemis-white)]">apogee init</code>. The
            init command writes <code>~/.claude/settings.json</code> so every
            hook event posts to this collector.
          </p>
        </Card>
      </section>

      <SummarizerSection />
    </div>
  );
}

/**
 * SummarizerSection — operator-controlled language + system prompt + model
 * overrides for the LLM recap and rollup workers. Persists everything to the
 * collector's user_preferences DuckDB table via PATCH /v1/preferences.
 */
function SummarizerSection() {
  const { data, mutate: revalidate } = useApi<PreferencesResponse>(
    "/v1/preferences",
    { refreshInterval: 30_000 },
  );
  const { data: modelsData } = useApi<ModelsResponse>(
    "/v1/models",
    { refreshInterval: 60_000 },
  );
  const models = modelsData?.models ?? [];
  const defaults = modelsData?.defaults ?? {
    recap: "",
    rollup: "",
    narrative: "",
  };

  const persisted: Required<SummarizerPreferences> = useMemo(() => {
    const p = data?.preferences ?? {};
    return {
      "summarizer.language":
        p["summarizer.language"] ?? DEFAULT_SUMMARIZER_PREFERENCES["summarizer.language"],
      "summarizer.recap_system_prompt":
        p["summarizer.recap_system_prompt"] ??
        DEFAULT_SUMMARIZER_PREFERENCES["summarizer.recap_system_prompt"],
      "summarizer.rollup_system_prompt":
        p["summarizer.rollup_system_prompt"] ??
        DEFAULT_SUMMARIZER_PREFERENCES["summarizer.rollup_system_prompt"],
      "summarizer.narrative_system_prompt":
        p["summarizer.narrative_system_prompt"] ??
        DEFAULT_SUMMARIZER_PREFERENCES["summarizer.narrative_system_prompt"],
      "summarizer.recap_model":
        p["summarizer.recap_model"] ??
        DEFAULT_SUMMARIZER_PREFERENCES["summarizer.recap_model"],
      "summarizer.rollup_model":
        p["summarizer.rollup_model"] ??
        DEFAULT_SUMMARIZER_PREFERENCES["summarizer.rollup_model"],
      "summarizer.narrative_model":
        p["summarizer.narrative_model"] ??
        DEFAULT_SUMMARIZER_PREFERENCES["summarizer.narrative_model"],
    };
  }, [data]);

  const [draft, setDraft] = useState<Required<SummarizerPreferences>>(persisted);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Reset the draft whenever the persisted snapshot changes (e.g. after a
  // successful save or a background revalidate).
  useEffect(() => {
    setDraft(persisted);
  }, [persisted]);

  const dirty = useMemo(() => {
    return (
      draft["summarizer.language"] !== persisted["summarizer.language"] ||
      draft["summarizer.recap_system_prompt"] !==
        persisted["summarizer.recap_system_prompt"] ||
      draft["summarizer.rollup_system_prompt"] !==
        persisted["summarizer.rollup_system_prompt"] ||
      draft["summarizer.narrative_system_prompt"] !==
        persisted["summarizer.narrative_system_prompt"] ||
      draft["summarizer.recap_model"] !== persisted["summarizer.recap_model"] ||
      draft["summarizer.rollup_model"] !==
        persisted["summarizer.rollup_model"] ||
      draft["summarizer.narrative_model"] !==
        persisted["summarizer.narrative_model"]
    );
  }, [draft, persisted]);

  async function onSave() {
    if (!dirty || busy) return;
    setBusy(true);
    setError(null);
    try {
      const updated = await patchPreferences(draft);
      await revalidate(updated, { revalidate: false });
      // Broadcast so siblings (TopRibbon LanguagePicker) see the change.
      await mutate(
        (key) => Array.isArray(key) && key[0] === "/v1/preferences",
        undefined,
        { revalidate: true },
      );
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  function onRevert() {
    setDraft(persisted);
    setError(null);
  }

  async function onResetAll() {
    if (busy) return;
    setBusy(true);
    setError(null);
    try {
      const updated = await resetPreferences();
      await revalidate(updated, { revalidate: false });
      await mutate(
        (key) => Array.isArray(key) && key[0] === "/v1/preferences",
        undefined,
        { revalidate: true },
      );
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section>
      <SectionHeader
        title="Summarizer"
        subtitle="Language, system prompts, and model overrides for the recap + rollup workers."
        actions={
          dirty ? (
            <span className="font-mono text-[10px] uppercase tracking-[0.14em] text-[var(--status-warning)]">
              unsaved changes
            </span>
          ) : null
        }
      />
      <Card>
        <div className="flex flex-col gap-4 p-1 text-[12px]">
          <LanguageRow
            value={draft["summarizer.language"]}
            onChange={(v) => setDraft((d) => ({ ...d, "summarizer.language": v }))}
          />

          <ModelDropdownRow
            label="RECAP MODEL"
            hint="Tier 1 — per-turn summary. Default picks the cheapest currently-available entry."
            models={models}
            useCase="recap"
            defaultAlias={defaults.recap}
            value={draft["summarizer.recap_model"] ?? ""}
            onChange={(v) =>
              setDraft((d) => ({ ...d, "summarizer.recap_model": v }))
            }
          />
          <ModelDropdownRow
            label="ROLLUP MODEL"
            hint="Tier 2 — per-session narrative digest."
            models={models}
            useCase="rollup"
            defaultAlias={defaults.rollup}
            value={draft["summarizer.rollup_model"] ?? ""}
            onChange={(v) =>
              setDraft((d) => ({ ...d, "summarizer.rollup_model": v }))
            }
          />
          <ModelDropdownRow
            label="NARRATIVE MODEL"
            hint="Tier 3 — phase timeline. Falls back to the rollup model when unset."
            models={models}
            useCase="narrative"
            defaultAlias={defaults.narrative}
            value={draft["summarizer.narrative_model"] ?? ""}
            onChange={(v) =>
              setDraft((d) => ({ ...d, "summarizer.narrative_model": v }))
            }
          />

          <SystemPromptField
            label="Recap system prompt"
            value={draft["summarizer.recap_system_prompt"]}
            onChange={(v) =>
              setDraft((d) => ({ ...d, "summarizer.recap_system_prompt": v }))
            }
          />
          <SystemPromptField
            label="Rollup system prompt"
            value={draft["summarizer.rollup_system_prompt"]}
            onChange={(v) =>
              setDraft((d) => ({ ...d, "summarizer.rollup_system_prompt": v }))
            }
          />
          <SystemPromptField
            label="Narrative system prompt"
            value={draft["summarizer.narrative_system_prompt"]}
            onChange={(v) =>
              setDraft((d) => ({
                ...d,
                "summarizer.narrative_system_prompt": v,
              }))
            }
          />

          {error && (
            <div className="rounded border border-[var(--status-warning)] bg-[var(--bg-raised)] px-3 py-2 font-mono text-[11px] text-[var(--status-warning)]">
              {error}
            </div>
          )}

          <div className="flex flex-wrap items-center gap-3">
            <button
              type="button"
              onClick={onSave}
              disabled={!dirty || busy}
              className="inline-flex items-center gap-2 rounded-md border border-[var(--border-bright)] bg-[var(--bg-raised)] px-3 py-1.5 font-mono text-[12px] text-[var(--artemis-white)] hover:bg-[var(--bg-overlay)] disabled:cursor-not-allowed disabled:opacity-40"
            >
              <Save size={13} strokeWidth={1.5} />
              Save changes
            </button>
            <button
              type="button"
              onClick={onRevert}
              disabled={!dirty || busy}
              className="inline-flex items-center gap-2 rounded-md border border-[var(--border)] bg-[var(--bg-raised)] px-3 py-1.5 font-mono text-[12px] text-[var(--artemis-space)] hover:bg-[var(--bg-overlay)] hover:text-[var(--artemis-white)] disabled:cursor-not-allowed disabled:opacity-40"
            >
              <RotateCcw size={13} strokeWidth={1.5} />
              Revert
            </button>
            <div className="ml-auto">
              <button
                type="button"
                onClick={onResetAll}
                disabled={busy}
                className="font-mono text-[11px] text-[var(--text-muted)] underline-offset-2 hover:text-[var(--status-warning)] hover:underline disabled:cursor-not-allowed disabled:opacity-40"
              >
                Reset all preferences to defaults
              </button>
            </div>
          </div>
        </div>
      </Card>
    </section>
  );
}

function LanguageRow({
  value,
  onChange,
}: {
  value: SummarizerLanguage;
  onChange: (v: SummarizerLanguage) => void;
}) {
  return (
    <div className="grid grid-cols-[160px_1fr] items-center gap-3">
      <span className="font-display text-[10px] tracking-[0.14em] text-[var(--text-muted)]">
        Language
      </span>
      <div className="inline-flex overflow-hidden rounded-md border border-[var(--border)]">
        {(["en", "ja"] as const).map((opt) => {
          const active = value === opt;
          return (
            <button
              key={opt}
              type="button"
              onClick={() => onChange(opt)}
              className={`inline-flex items-center gap-1.5 px-3 py-1.5 font-mono text-[12px] ${
                active
                  ? "bg-[var(--bg-overlay)] text-[var(--artemis-white)]"
                  : "bg-[var(--bg-raised)] text-[var(--artemis-space)] hover:bg-[var(--bg-overlay)] hover:text-[var(--artemis-white)]"
              }`}
              aria-pressed={active}
            >
              <Languages size={12} strokeWidth={1.5} />
              {opt.toUpperCase()}
            </button>
          );
        })}
      </div>
    </div>
  );
}

/**
 * ModelDropdownRow — catalog-backed <select> for the three summarizer
 * tiers. The first option is always "Use default (<display>)" with
 * value="" so an empty string clears any persisted override. Subsequent
 * options are one per recommended catalog entry for the given use case.
 * Unavailable entries (available=false) are still rendered but visually
 * dimmed; when the current value points at one the row surfaces a
 * warning pill so the operator can see the problem.
 */
function ModelDropdownRow({
  label,
  hint,
  models,
  useCase,
  defaultAlias,
  value,
  onChange,
}: {
  label: string;
  hint: string;
  models: ModelInfo[];
  useCase: ModelUseCase;
  defaultAlias: string;
  value: string;
  onChange: (v: string) => void;
}) {
  const recommended = models.filter((m) =>
    m.recommended.includes(useCase),
  );
  const defaultEntry = models.find((m) => m.alias === defaultAlias);
  const defaultLabel = defaultEntry
    ? `Use default (${defaultEntry.display})`
    : "Use default";
  const current = value
    ? models.find((m) => m.alias === value)
    : undefined;
  const warnUnavailable = Boolean(current && !current.available);
  return (
    <div className="grid grid-cols-[160px_1fr] items-start gap-3">
      <span className="font-display text-[10px] tracking-[0.14em] text-[var(--text-muted)]">
        {label}
      </span>
      <div className="flex flex-col gap-1">
        <div className="flex flex-wrap items-center gap-2">
          <select
            value={value}
            onChange={(e) => onChange(e.target.value)}
            aria-label={`${label} override (empty uses the catalog default)`}
            className="w-full max-w-[420px] rounded border border-[var(--border)] bg-[var(--bg-surface)] px-3 py-1.5 font-mono text-[12px] text-[var(--artemis-white)] focus:border-[var(--border-bright)] focus:outline-none"
          >
            <option value="">{defaultLabel}</option>
            {recommended.map((m) => (
              <option
                key={m.alias}
                value={m.alias}
                data-unavailable={!m.available || undefined}
              >
                {m.display} — {m.status}
                {m.available ? "" : ", unavailable"}
              </option>
            ))}
          </select>
          {warnUnavailable ? (
            <span className="font-mono text-[10px] uppercase tracking-[0.14em] text-[var(--status-warning)]">
              currently unavailable
            </span>
          ) : null}
        </div>
        <div className="flex items-center justify-between font-mono text-[10px] text-[var(--text-muted)]">
          <span>{hint}</span>
          <a
            href="/v1/models"
            target="_blank"
            rel="noreferrer"
            className="underline-offset-2 hover:text-[var(--artemis-white)] hover:underline"
          >
            View all models →
          </a>
        </div>
      </div>
    </div>
  );
}

function SystemPromptField({
  label,
  value,
  onChange,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
}) {
  const tooLong = value.length > SYSTEM_PROMPT_MAX;
  return (
    <div className="flex flex-col gap-1">
      <label className="font-display text-[10px] tracking-[0.14em] text-[var(--text-muted)]">
        {label}
      </label>
      <textarea
        rows={6}
        value={value}
        maxLength={SYSTEM_PROMPT_MAX}
        onChange={(e) => onChange(e.target.value)}
        className="w-full rounded border border-[var(--border)] bg-[var(--bg-surface)] px-3 py-2 font-mono text-[12px] text-[var(--artemis-white)] placeholder:text-[var(--text-muted)] focus:border-[var(--border-bright)] focus:outline-none"
        placeholder="Optional. Appended to the default summarizer instructions."
      />
      <div className="flex items-center justify-between font-mono text-[10px] text-[var(--text-muted)]">
        <span>
          Appended to the default system prompt. Use this to customise tone or
          add domain context.
        </span>
        <span className={tooLong ? "text-[var(--status-warning)]" : undefined}>
          {value.length} / {SYSTEM_PROMPT_MAX}
        </span>
      </div>
    </div>
  );
}

/**
 * AppearanceSection — segmented control for the theme preference. Writes
 * immediately on click via `useTheme` (which persists to localStorage and
 * tracks `prefers-color-scheme` when `system` is active). No save button —
 * the control is identical in behaviour to the TopRibbon ThemeToggle.
 */
const APPEARANCE_OPTIONS: Array<{
  value: Preference;
  label: string;
  icon: typeof Sun;
  hint: string;
}> = [
  {
    value: "system",
    label: "System",
    icon: Monitor,
    hint: "Follow the OS-level prefers-color-scheme setting.",
  },
  {
    value: "light",
    label: "Light",
    icon: Sun,
    hint: "Always render the light palette.",
  },
  {
    value: "dark",
    label: "Dark",
    icon: Moon,
    hint: "Always render the dark palette (the original apogee look).",
  },
];

function AppearanceSection() {
  const { preference, setPreference, theme } = useTheme();
  const active = APPEARANCE_OPTIONS.find((o) => o.value === preference);
  return (
    <section>
      <SectionHeader
        title="Appearance"
        subtitle="Dashboard theme. Saves immediately — no reload required."
      />
      <Card>
        <div className="flex flex-col gap-3 p-1 text-[12px]">
          <div
            className="inline-flex self-start overflow-hidden rounded-md border border-[var(--border)]"
            role="group"
            aria-label="Theme preference"
          >
            {APPEARANCE_OPTIONS.map((opt) => {
              const Icon = opt.icon;
              const isActive = opt.value === preference;
              return (
                <button
                  key={opt.value}
                  type="button"
                  onClick={() => setPreference(opt.value)}
                  aria-pressed={isActive}
                  title={opt.hint}
                  className={`inline-flex items-center gap-1.5 px-3 py-1.5 font-mono text-[12px] transition-colors ${
                    isActive
                      ? "bg-[var(--bg-overlay)] text-[var(--artemis-white)]"
                      : "bg-[var(--bg-raised)] text-[var(--artemis-space)] hover:bg-[var(--bg-overlay)] hover:text-[var(--artemis-white)]"
                  }`}
                >
                  <Icon size={13} strokeWidth={1.5} />
                  {opt.label}
                </button>
              );
            })}
          </div>
          <p className="font-mono text-[10px] text-[var(--text-muted)]">
            {active?.hint}{" "}
            {preference === "system" && (
              <span>
                Currently resolving to{" "}
                <span className="text-[var(--artemis-white)]">{theme}</span>.
              </span>
            )}
          </p>
        </div>
      </Card>
    </section>
  );
}
