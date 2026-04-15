"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import {
  ChevronsUpDown,
  Clock,
  Languages,
  Layers,
  Monitor,
  Moon,
  RefreshCw,
  Sun,
} from "lucide-react";
import { mutate } from "swr";

import type {
  FilterOptions,
  PreferencesResponse,
  SummarizerLanguage,
} from "../lib/api-types";
import { patchPreferences } from "../lib/preferences";
import { useRefresh } from "../lib/refresh";
import { useApi } from "../lib/swr";
import type { Preference } from "../lib/theme";
import { useTheme } from "../lib/theme";
import {
  DEFAULT_TIME_RANGE_VALUE,
  TIME_RANGE_PRESETS,
  customTimeRange,
} from "../lib/time-range";
import { useSelection } from "../lib/url-state";
import LiveIndicator from "./LiveIndicator";
import SessionCommandPalette from "./SessionCommandPalette";

/**
 * TopRibbon — always-on global selector bar. The ribbon sits above the
 * sidebar-aware main content and owns:
 *
 *   - The wordmark (cleared-state link back to /)
 *   - Env (source_app) selector
 *   - Session selector → opens the command palette
 *   - Time range picker (presets + custom)
 *   - Refresh button (bumps the global refresh token)
 *   - Live indicator (SSE status)
 *
 * Every change flows through useSelection so state is URL-driven and deep
 * linkable. The ribbon is tab-reachable and keyboard-navigable; ⌘K toggles
 * the palette.
 */

function shortId(id: string, len = 8): string {
  if (!id) return "";
  return id.length <= len ? id : id.slice(0, len);
}

export default function TopRibbon() {
  const { selection } = useSelection();
  const refresh = useRefresh();
  const [paletteOpen, setPaletteOpen] = useState(false);

  // Global ⌘K / Ctrl+K binding. Toggles the palette regardless of focus.
  useEffect(() => {
    function onKey(ev: KeyboardEvent) {
      const isAccel = ev.metaKey || ev.ctrlKey;
      if (isAccel && ev.key.toLowerCase() === "k") {
        ev.preventDefault();
        setPaletteOpen((o) => !o);
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  return (
    <div className="sticky top-0 z-40 border-b border-[var(--border)] bg-[var(--bg-surface)]">
      <div className="flex items-center gap-3 px-4 py-2">
        {/* Wordmark */}
        <Link
          href="/"
          className="font-display text-[13px] tracking-[0.2em] text-[var(--artemis-white)]"
          title="apogee — clear selection"
        >
          APOGEE
        </Link>
        <RibbonDivider />

        <EnvSelector />
        <RibbonDivider />

        <button
          type="button"
          onClick={() => setPaletteOpen(true)}
          className="inline-flex items-center gap-2 rounded-md border border-[var(--border)] bg-[var(--bg-raised)] px-3 py-1.5 font-mono text-[12px] text-[var(--artemis-space)] hover:bg-[var(--bg-overlay)] hover:text-[var(--artemis-white)] focus:outline-none focus-visible:ring-1 focus-visible:ring-[var(--border-bright)]"
          title="Open session palette (⌘K)"
        >
          <Layers size={13} strokeWidth={1.5} />
          <span>
            {selection.sess ? (
              <span className="text-[var(--artemis-white)]">
                {shortId(selection.sess)}
              </span>
            ) : (
              <span>session: — none</span>
            )}
          </span>
          <ChevronsUpDown size={12} strokeWidth={1.5} className="opacity-60" />
        </button>
        <RibbonDivider />

        <TimeRangePicker />
        <RibbonDivider />

        <ThemeToggle />
        <RibbonDivider />

        <LanguagePicker />

        <div className="flex flex-1 items-center justify-end gap-3">
          <button
            type="button"
            onClick={() => refresh.bump()}
            className="rounded-md border border-[var(--border)] bg-[var(--bg-raised)] p-1.5 text-[var(--artemis-space)] hover:bg-[var(--bg-overlay)] hover:text-[var(--artemis-white)] focus:outline-none focus-visible:ring-1 focus-visible:ring-[var(--border-bright)]"
            title="Refresh dashboard"
            aria-label="Refresh"
          >
            <RefreshCw size={13} strokeWidth={1.5} />
          </button>
          <LiveIndicator />
        </div>
      </div>
      {/* Accent gradient beat — 1px line that brands the ribbon. */}
      <div className="accent-gradient-bar h-[1px] w-full" />
      <SessionCommandPalette
        open={paletteOpen}
        onClose={() => setPaletteOpen(false)}
      />
    </div>
  );
}

function RibbonDivider() {
  return <span className="h-4 w-px bg-[var(--border)]" aria-hidden />;
}

function EnvSelector() {
  const { selection, setSelection } = useSelection();
  const { data } = useApi<FilterOptions>("/v1/filter-options", { refreshInterval: 30_000 });
  const [open, setOpen] = useState(false);

  const apps = data?.source_apps ?? [];
  const label = selection.env ? selection.env : "env: all";

  return (
    <div className="relative">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="inline-flex items-center gap-2 rounded-md border border-[var(--border)] bg-[var(--bg-raised)] px-3 py-1.5 font-mono text-[12px] text-[var(--artemis-space)] hover:bg-[var(--bg-overlay)] hover:text-[var(--artemis-white)]"
      >
        <span>{label}</span>
        <ChevronsUpDown size={12} strokeWidth={1.5} className="opacity-60" />
      </button>
      {open && (
        <div className="absolute left-0 top-full z-50 mt-1 min-w-[180px] rounded-md border border-[var(--border-bright)] bg-[var(--bg-overlay)] shadow-[var(--shadow-lg)]">
          <button
            type="button"
            onClick={() => {
              setSelection({ env: null });
              setOpen(false);
            }}
            className={`block w-full px-3 py-1.5 text-left font-mono text-[12px] hover:bg-[var(--bg-raised)] ${
              !selection.env ? "text-[var(--artemis-white)]" : "text-[var(--artemis-space)]"
            }`}
          >
            env: all
          </button>
          {apps.map((app) => (
            <button
              key={app}
              type="button"
              onClick={() => {
                setSelection({ env: app });
                setOpen(false);
              }}
              className={`block w-full px-3 py-1.5 text-left font-mono text-[12px] hover:bg-[var(--bg-raised)] ${
                selection.env === app ? "text-[var(--artemis-white)]" : "text-[var(--artemis-space)]"
              }`}
            >
              {app}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

function TimeRangePicker() {
  const { selection, setSelection } = useSelection();
  const [open, setOpen] = useState(false);
  const [customSince, setCustomSince] = useState("");
  const [customUntil, setCustomUntil] = useState("");

  const applyCustom = useCallback(() => {
    if (!customSince || !customUntil) return;
    const a = new Date(customSince);
    const b = new Date(customUntil);
    if (Number.isNaN(a.getTime()) || Number.isNaN(b.getTime())) return;
    setSelection({ time: customTimeRange(a, b) });
    setOpen(false);
  }, [customSince, customUntil, setSelection]);

  return (
    <div className="relative">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="inline-flex items-center gap-2 rounded-md border border-[var(--border)] bg-[var(--bg-raised)] px-3 py-1.5 font-mono text-[12px] text-[var(--artemis-space)] hover:bg-[var(--bg-overlay)] hover:text-[var(--artemis-white)]"
      >
        <Clock size={13} strokeWidth={1.5} />
        <span>{selection.time.label}</span>
        <ChevronsUpDown size={12} strokeWidth={1.5} className="opacity-60" />
      </button>
      {open && (
        <div className="absolute left-0 top-full z-50 mt-1 min-w-[220px] rounded-md border border-[var(--border-bright)] bg-[var(--bg-overlay)] p-1 shadow-[var(--shadow-lg)]">
          {TIME_RANGE_PRESETS.map((preset) => {
            const active =
              selection.time.shorthand === preset.value ||
              (selection.time.shorthand === null &&
                preset.value === DEFAULT_TIME_RANGE_VALUE &&
                selection.time.label === preset.label);
            return (
              <button
                key={preset.value}
                type="button"
                onClick={() => {
                  setSelection({ time: preset.value });
                  setOpen(false);
                }}
                className={`block w-full rounded px-3 py-1.5 text-left font-mono text-[12px] hover:bg-[var(--bg-raised)] ${
                  active ? "text-[var(--artemis-white)]" : "text-[var(--artemis-space)]"
                }`}
              >
                {preset.label}
              </button>
            );
          })}
          <div className="mt-1 border-t border-[var(--border)] px-3 py-2">
            <p className="mb-1 font-display text-[10px] uppercase tracking-[0.14em] text-[var(--artemis-space)]">
              Custom
            </p>
            <div className="flex flex-col gap-1">
              <input
                type="datetime-local"
                value={customSince}
                onChange={(e) => setCustomSince(e.target.value)}
                className="rounded border border-[var(--border)] bg-[var(--bg-surface)] px-2 py-1 font-mono text-[11px] text-[var(--artemis-white)]"
              />
              <input
                type="datetime-local"
                value={customUntil}
                onChange={(e) => setCustomUntil(e.target.value)}
                className="rounded border border-[var(--border)] bg-[var(--bg-surface)] px-2 py-1 font-mono text-[11px] text-[var(--artemis-white)]"
              />
              <button
                type="button"
                onClick={applyCustom}
                className="mt-1 rounded border border-[var(--border-bright)] bg-[var(--bg-raised)] px-2 py-1 text-[11px] text-[var(--artemis-white)] hover:bg-[var(--bg-overlay)]"
              >
                Apply
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

/**
 * LanguagePicker — compact EN / JA toggle that lives between the time-range
 * picker and the LIVE indicator. Reads /v1/preferences via SWR (60 s
 * refresh) and PATCHes optimistically on click. Falls back to "EN" when the
 * API is unreachable so the UI keeps working offline.
 */
const LANGUAGE_OPTIONS: Array<{ value: SummarizerLanguage; label: string }> = [
  { value: "en", label: "EN" },
  { value: "ja", label: "JA" },
];

function LanguagePicker() {
  const { data, mutate: revalidate } = useApi<PreferencesResponse>(
    "/v1/preferences",
    { refreshInterval: 60_000 },
  );
  const [open, setOpen] = useState(false);
  const [pending, setPending] = useState<SummarizerLanguage | null>(null);

  const current: SummarizerLanguage =
    pending ?? data?.preferences?.["summarizer.language"] ?? "en";
  const label = LANGUAGE_OPTIONS.find((o) => o.value === current)?.label ?? "EN";

  const apply = useCallback(
    async (next: SummarizerLanguage) => {
      setOpen(false);
      if (next === current) return;
      setPending(next);
      try {
        const updated = await patchPreferences({
          "summarizer.language": next,
        });
        // Push the new preferences into every SWR cache key under
        // /v1/preferences (the useApi key is a tuple, so revalidate the
        // local hook AND broadcast a global mutate for siblings).
        await revalidate(updated, { revalidate: false });
        await mutate(
          (key) =>
            Array.isArray(key) &&
            key[0] === "/v1/preferences",
          undefined,
          { revalidate: true },
        );
      } catch {
        // Swallow — the language picker degrades to its last known good
        // state on failure. The settings page surfaces a real error.
      } finally {
        setPending(null);
      }
    },
    [current, revalidate],
  );

  // Keyboard handling: Esc closes the menu when it has focus inside.
  useEffect(() => {
    if (!open) return;
    function onKey(ev: KeyboardEvent) {
      if (ev.key === "Escape") {
        setOpen(false);
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open]);

  return (
    <div className="relative">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="inline-flex items-center gap-1.5 rounded-md border border-[var(--border)] bg-[var(--bg-raised)] px-2 py-1.5 font-mono text-[12px] text-[var(--artemis-space)] hover:bg-[var(--bg-overlay)] hover:text-[var(--artemis-white)] focus:outline-none focus-visible:ring-1 focus-visible:ring-[var(--border-bright)]"
        title="Summarizer output language"
        aria-label={`Summarizer language: ${label}`}
        aria-haspopup="listbox"
        aria-expanded={open}
      >
        <Languages size={13} strokeWidth={1.5} />
        <span>{label}</span>
        <ChevronsUpDown size={11} strokeWidth={1.5} className="opacity-60" />
      </button>
      {open && (
        <div
          role="listbox"
          aria-label="Summarizer language"
          className="absolute right-0 top-full z-50 mt-1 min-w-[80px] rounded-md border border-[var(--border-bright)] bg-[var(--bg-overlay)] p-1 shadow-[var(--shadow-lg)]"
        >
          {LANGUAGE_OPTIONS.map((opt) => {
            const active = opt.value === current;
            return (
              <button
                key={opt.value}
                type="button"
                role="option"
                aria-selected={active}
                onClick={() => apply(opt.value)}
                className={`block w-full rounded px-3 py-1.5 text-left font-mono text-[12px] hover:bg-[var(--bg-raised)] ${
                  active ? "text-[var(--artemis-white)]" : "text-[var(--artemis-space)]"
                }`}
              >
                {opt.label}
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}

/**
 * ThemeToggle — cycles through `system → light → dark → system`. The
 * current state drives both the icon (`Monitor` / `Sun` / `Moon`) and
 * the accessible label so the button reads naturally to screen readers.
 * Tab-reachable and Enter / Space activated via the native button.
 */
const THEME_ORDER: Preference[] = ["system", "light", "dark"];
const THEME_LABEL: Record<Preference, string> = {
  system: "system",
  light: "light",
  dark: "dark",
};

function nextPreference(p: Preference): Preference {
  const idx = THEME_ORDER.indexOf(p);
  return THEME_ORDER[(idx + 1) % THEME_ORDER.length]!;
}

function ThemeToggle() {
  const { preference, setPreference } = useTheme();
  const next = nextPreference(preference);
  const Icon =
    preference === "system" ? Monitor : preference === "light" ? Sun : Moon;

  const label = `Theme: ${THEME_LABEL[preference]} (click to switch to ${THEME_LABEL[next]})`;

  return (
    <button
      type="button"
      onClick={() => setPreference(next)}
      className="inline-flex h-[28px] w-[28px] items-center justify-center rounded-md border border-[var(--border)] bg-[var(--bg-raised)] text-[var(--artemis-space)] hover:bg-[var(--bg-overlay)] hover:text-[var(--artemis-white)] focus:outline-none focus-visible:ring-1 focus-visible:ring-[var(--border-bright)]"
      title={label}
      aria-label={label}
    >
      <Icon size={13} strokeWidth={1.5} />
    </button>
  );
}
