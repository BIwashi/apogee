"use client";

import type { LucideIcon } from "lucide-react";

/**
 * Tabs — a plain WAI-ARIA tablist that keeps the active tab in the URL query
 * string so deep-linking survives reloads. The caller owns the active tab
 * state and passes `onSelect(key)` so the tab list stays a controlled
 * component. Styling uses an underline indicator in the accent gradient for
 * the active tab, matching the ribbon's brand beat.
 */

export interface TabItem<K extends string = string> {
  key: K;
  label: string;
  icon?: LucideIcon;
}

interface TabsProps<K extends string> {
  items: TabItem<K>[];
  active: K;
  onSelect: (key: K) => void;
}

export default function Tabs<K extends string>({ items, active, onSelect }: TabsProps<K>) {
  return (
    <div role="tablist" className="flex items-center gap-1 border-b border-[var(--border)]">
      {items.map((item) => {
        const Icon = item.icon;
        const isActive = item.key === active;
        return (
          <button
            key={item.key}
            type="button"
            role="tab"
            aria-selected={isActive}
            onClick={() => onSelect(item.key)}
            className={`relative flex items-center gap-2 px-4 py-2 font-mono text-[12px] transition-colors ${
              isActive
                ? "text-white"
                : "text-[var(--artemis-space)] hover:text-gray-200"
            }`}
          >
            {Icon && <Icon size={13} strokeWidth={1.5} />}
            <span>{item.label}</span>
            {isActive && (
              <span
                aria-hidden
                className="accent-gradient-bar absolute bottom-[-1px] left-2 right-2 h-[2px] rounded-full"
              />
            )}
          </button>
        );
      })}
    </div>
  );
}
