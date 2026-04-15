"use client";

import { usePathname } from "next/navigation";
import { useState, useSyncExternalStore } from "react";
import {
  BarChart3,
  Layers,
  Palette,
  PanelLeftClose,
  PanelLeftOpen,
  Radar,
  ScrollText,
  SlidersHorizontal,
  Users,
  type LucideIcon,
} from "lucide-react";

/**
 * Sidebar — the primary navigation shell. PR #24 rewrites the information
 * architecture so every entry renders a genuinely different page:
 *
 *   /           Live focus dashboard (flame graph + triage rail)
 *   /sessions   Service catalog (search + filter)
 *   /agents     Per-agent main/subagent view
 *   /insights   Aggregate analytics
 *   /settings   Collector info + telemetry status
 *   /styleguide Dev tokens reference
 *
 * Search is reachable via ⌘K on the TopRibbon palette, not a route. Timeline
 * was deleted — it was a thin alias of `/` from PR #6.5.
 */

interface NavItem {
  href: string;
  label: string;
  icon: LucideIcon;
}

const NAV: NavItem[] = [
  { href: "/", label: "Live", icon: Radar },
  { href: "/sessions", label: "Sessions", icon: Layers },
  { href: "/agents", label: "Agents", icon: Users },
  { href: "/insights", label: "Insights", icon: BarChart3 },
  { href: "/events", label: "Events", icon: ScrollText },
  { href: "/settings", label: "Settings", icon: SlidersHorizontal },
  { href: "/styleguide", label: "Styleguide", icon: Palette },
];

function isActive(pathname: string, item: NavItem): boolean {
  // Strip trailing slash so "/sessions/" matches "/sessions".
  const normalised = pathname.endsWith("/") && pathname !== "/"
    ? pathname.slice(0, -1)
    : pathname;
  if (item.href === "/") {
    return normalised === "/" || normalised === "";
  }
  return normalised === item.href || normalised.startsWith(`${item.href}/`);
}

const MOBILE_QUERY = "(max-width: 768px)";

function subscribeMobile(callback: () => void): () => void {
  if (typeof window === "undefined") return () => {};
  const mq = window.matchMedia(MOBILE_QUERY);
  mq.addEventListener("change", callback);
  return () => mq.removeEventListener("change", callback);
}

function getMobileSnapshot(): boolean {
  if (typeof window === "undefined") return false;
  return window.matchMedia(MOBILE_QUERY).matches;
}

function getMobileServerSnapshot(): boolean {
  return false;
}

export default function Sidebar({ children }: { children: React.ReactNode }) {
  const isMobile = useSyncExternalStore(
    subscribeMobile,
    getMobileSnapshot,
    getMobileServerSnapshot,
  );
  const [userCollapsed, setUserCollapsed] = useState<boolean | null>(null);
  const collapsed = userCollapsed ?? isMobile;
  const pathname = usePathname();

  return (
    <div className="flex">
      <nav
        className={`${
          collapsed ? "w-14" : "w-56"
        } fixed flex min-h-screen flex-col border-r border-[var(--border)] bg-[var(--bg-surface)] transition-all duration-200`}
      >
        {/* Brand header */}
        <div
          className={`flex items-center px-4 py-5 ${
            collapsed ? "justify-center" : "justify-between"
          }`}
        >
          {!collapsed ? (
            <h1 className="font-display text-base tracking-[0.2em] text-[var(--artemis-white)]">
              APOGEE
            </h1>
          ) : (
            <span className="font-display text-sm text-[var(--artemis-white)]">A</span>
          )}
          <button
            onClick={() => setUserCollapsed(!collapsed)}
            className="ml-1 rounded p-1 text-[var(--text-muted)] transition-colors hover:bg-[var(--surface-raised)] hover:text-[var(--artemis-white)]"
            title={collapsed ? "Expand sidebar" : "Collapse sidebar"}
          >
            {collapsed ? (
              <PanelLeftOpen size={14} strokeWidth={1.5} />
            ) : (
              <PanelLeftClose size={14} strokeWidth={1.5} />
            )}
          </button>
        </div>

        {/* Nav */}
        <ul className="mt-2 flex-1 space-y-0.5 px-2">
          {NAV.map((item) => {
            const { href, label, icon: Icon } = item;
            const active = isActive(pathname, item);
            const base =
              "flex items-center gap-2.5 rounded px-3 py-1.5 text-[13px] transition-colors w-full";
            const tone = active
              ? "bg-[var(--accent)]/10 font-medium text-[var(--accent)]"
              : "text-[var(--text-muted)] hover:bg-[var(--surface-raised)] hover:text-[var(--artemis-white)]";
            return (
              <li key={href}>
                <a
                  href={href}
                  className={`${base} ${tone}`}
                  title={collapsed ? label : undefined}
                >
                  <Icon size={16} strokeWidth={1.5} className="flex-shrink-0" />
                  {!collapsed && <span>{label}</span>}
                </a>
              </li>
            );
          })}
        </ul>

        {/* Footer — build version stub + ⌘K hint */}
        {!collapsed && (
          <div className="border-t border-[var(--border)] px-4 py-3">
            <p className="font-mono text-[10px] text-[var(--text-muted)]">
              apogee 0.0.0-dev
            </p>
            <p className="mt-1 font-mono text-[10px] text-[var(--text-muted)]">
              <kbd className="rounded border border-[var(--border)] bg-[var(--bg-raised)] px-1 py-[1px]">
                ⌘K
              </kbd>{" "}
              session palette
            </p>
          </div>
        )}
      </nav>

      <main
        className={`${
          collapsed ? "ml-14" : "ml-56"
        } flex-1 p-4 transition-all duration-200 md:p-6`}
      >
        {children}
      </main>
    </div>
  );
}
