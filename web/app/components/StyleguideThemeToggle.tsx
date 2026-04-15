"use client";

import { Monitor, Moon, Sun } from "lucide-react";

import type { Preference } from "../lib/theme";
import { useTheme } from "../lib/theme";

/**
 * StyleguideThemeToggle — a big three-state segmented control used on the
 * /styleguide page so designers can flip between light and dark without
 * leaving the page. Shares the same `useTheme` hook as the TopRibbon
 * toggle and the Settings Appearance section, so all three stay in sync.
 */
const OPTIONS: Array<{ value: Preference; label: string; icon: typeof Sun }> = [
  { value: "system", label: "System", icon: Monitor },
  { value: "light", label: "Light", icon: Sun },
  { value: "dark", label: "Dark", icon: Moon },
];

export default function StyleguideThemeToggle() {
  const { preference, setPreference, theme } = useTheme();
  return (
    <div className="flex flex-wrap items-center gap-4">
      <div
        className="inline-flex overflow-hidden rounded-md border border-[var(--border)]"
        role="group"
        aria-label="Theme preference"
      >
        {OPTIONS.map((opt) => {
          const Icon = opt.icon;
          const isActive = opt.value === preference;
          return (
            <button
              key={opt.value}
              type="button"
              onClick={() => setPreference(opt.value)}
              aria-pressed={isActive}
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
      <span className="font-mono text-[10px] text-[var(--text-muted)]">
        resolved theme:{" "}
        <span className="text-[var(--artemis-white)]">{theme}</span>
      </span>
    </div>
  );
}
