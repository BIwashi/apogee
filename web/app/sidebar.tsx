"use client";

import { useState, useSyncExternalStore } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import {
  BarChart3,
  Layers,
  type LucideIcon,
  Palette,
  PanelLeftClose,
  PanelLeftOpen,
  Radar,
  ScrollText,
  SlidersHorizontal,
  Users,
} from "lucide-react";
import VersionTag from "./components/VersionTag";

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
  hint: string;
}

const NAV: NavItem[] = [
  {
    href: "/",
    label: "Live",
    icon: Radar,
    hint: "Active turns, attention triage, and recent events as they stream in.",
  },
  {
    href: "/sessions",
    label: "Sessions",
    icon: Layers,
    hint: "Every Claude Code session with rollups, phases, and drill-down.",
  },
  {
    href: "/agents",
    label: "Agents",
    icon: Users,
    hint: "Main agent + subagent activity and tool usage per session.",
  },
  {
    href: "/insights",
    label: "Insights",
    icon: BarChart3,
    hint: "Aggregate analytics: tool rate, error rate, HITL, latency.",
  },
  {
    href: "/events",
    label: "Events",
    icon: ScrollText,
    hint: "Datadog-style event explorer with facets and timeseries.",
  },
  {
    href: "/settings",
    label: "Settings",
    icon: SlidersHorizontal,
    hint: "Collector info, summarizer prefs, telemetry, theme.",
  },
  {
    href: "/styleguide",
    label: "Styleguide",
    icon: Palette,
    hint: "Design token reference — colours, typography, spacing.",
  },
];

function isActive(pathname: string, item: NavItem): boolean {
  // Strip trailing slash so "/sessions/" matches "/sessions".
  const normalised =
    pathname.endsWith("/") && pathname !== "/"
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
            <h1 className="font-display-accent text-base tracking-[0.2em] text-[var(--artemis-white)]">
              APOGEE
            </h1>
          ) : (
            <span className="font-display-accent text-sm text-[var(--artemis-white)]">
              A
            </span>
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
            const { href, label, icon: Icon, hint } = item;
            const active = isActive(pathname, item);
            const base =
              "flex items-center gap-2.5 rounded px-3 py-1.5 text-[13px] transition-colors w-full";
            const tone = active
              ? "bg-[var(--accent)]/10 font-medium text-[var(--accent)]"
              : "text-[var(--text-muted)] hover:bg-[var(--surface-raised)] hover:text-[var(--artemis-white)]";
            return (
              <li key={href} className="group relative">
                {/*
                 * next/link, not a bare <a href>: the bare anchor
                 * triggers a full-page reload, and full-page reloads
                 * from inside the Wails WKWebView round-trip through
                 * the reverse-proxy AssetServer in a way that
                 * sometimes drops the navigation entirely (observed
                 * with v0.1.17 on macOS 15). Next.js' Link uses the
                 * client-side router + history.pushState, which the
                 * WebView handles reliably and which also matches
                 * the browser-only usage where the SPA never pays
                 * the cost of re-downloading the whole bundle on
                 * every sidebar click.
                 */}
                <Link
                  href={href}
                  className={`${base} ${tone}`}
                  aria-label={collapsed ? `${label} — ${hint}` : undefined}
                >
                  <Icon size={16} strokeWidth={1.5} className="flex-shrink-0" />
                  {!collapsed && <span>{label}</span>}
                </Link>
                {/* Hover hint — rendered at sidebar edge so it escapes the
                    nav column without shifting the hit target. Keyboard
                    focus opens the same popover for a11y. */}
                <div
                  role="tooltip"
                  className={`pointer-events-none absolute top-1/2 z-50 -translate-y-1/2 whitespace-normal rounded border border-[var(--border-bright)] bg-[var(--bg-overlay)] p-3 text-[11px] leading-snug text-[var(--text-primary)] opacity-0 shadow-lg transition-opacity duration-150 group-hover:opacity-100 group-focus-within:opacity-100 ${
                    collapsed
                      ? "left-[calc(100%+8px)] w-56"
                      : "left-[calc(100%+4px)] w-60"
                  }`}
                >
                  <p className="font-display text-[10px] uppercase tracking-[0.14em] text-[var(--artemis-white)]">
                    {label}
                  </p>
                  <p className="mt-1 text-[var(--text-muted)]">{hint}</p>
                </div>
              </li>
            );
          })}
        </ul>

        {/* Footer — build version (from /v1/info) + ⌘K hint */}
        {!collapsed && (
          <div className="border-t border-[var(--border)] px-4 py-3">
            <VersionTag />
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
