"use client";

import type { ReactNode } from "react";
import type { LucideIcon } from "lucide-react";

/**
 * DrawerHeader — shared header shell for every cross-cutting SideDrawer.
 *
 * Renders as:
 *
 *   [icon]  ENTITY TYPE
 *   Truncated title (may wrap)
 *   [tab] [tab] [tab]
 *
 * The outer SideDrawer primitive already renders its own header with the
 * close button; DrawerHeader sits inside the scroll area as the first block
 * so the three drawers share identical visual structure.
 */

interface DrawerHeaderProps {
  icon: LucideIcon;
  kind: string;
  title: ReactNode;
  subtitle?: ReactNode;
  trailing?: ReactNode;
  accent?: string;
}

export default function DrawerHeader({
  icon: Icon,
  kind,
  title,
  subtitle,
  trailing,
  accent = "var(--accent)",
}: DrawerHeaderProps) {
  return (
    <header className="flex flex-col gap-2 border-b border-[var(--border)] pb-3">
      <div className="flex items-center gap-2">
        <span
          className="inline-flex h-6 w-6 items-center justify-center rounded border"
          style={{ borderColor: accent, color: accent }}
        >
          <Icon size={12} strokeWidth={1.5} />
        </span>
        <span
          className="font-display text-[10px] uppercase tracking-[0.16em]"
          style={{ color: accent }}
        >
          {kind}
        </span>
        {trailing ? <span className="ml-auto">{trailing}</span> : null}
      </div>
      <h3 className="font-display text-[18px] leading-snug text-[var(--artemis-white)]">
        {title}
      </h3>
      {subtitle ? (
        <p className="font-mono text-[11px] text-[var(--text-muted)]">
          {subtitle}
        </p>
      ) : null}
    </header>
  );
}

interface DrawerTabBarProps<TabKey extends string> {
  tabs: ReadonlyArray<{ key: TabKey; label: string }>;
  active: TabKey;
  onChange: (key: TabKey) => void;
}

/**
 * DrawerTabBar — compact, horizontally scrollable tab row used inside
 * drawers. Rendered as a separate primitive so drawers without tabs can omit
 * it entirely.
 */
export function DrawerTabBar<TabKey extends string>({
  tabs,
  active,
  onChange,
}: DrawerTabBarProps<TabKey>) {
  return (
    <nav className="-mx-4 overflow-x-auto border-b border-[var(--border)]">
      <ul className="flex min-w-full items-center gap-1 px-4 py-1">
        {tabs.map((tab) => {
          const isActive = tab.key === active;
          return (
            <li key={tab.key}>
              <button
                type="button"
                onClick={() => onChange(tab.key)}
                className={`rounded px-2 py-1 font-display text-[11px] uppercase tracking-[0.14em] transition-colors ${
                  isActive
                    ? "bg-[var(--bg-raised)] text-[var(--artemis-white)]"
                    : "text-[var(--text-muted)] hover:bg-[var(--bg-raised)] hover:text-[var(--artemis-white)]"
                }`}
                aria-pressed={isActive}
              >
                {tab.label}
              </button>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}
