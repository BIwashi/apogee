"use client";

import { useEffect, useState } from "react";
import {
  Activity,
  GitBranch,
  LayoutGrid,
  PanelLeftClose,
  PanelLeftOpen,
  Settings,
  Users,
  type LucideIcon,
} from "lucide-react";

/**
 * Sidebar — the primary navigation shell. apogee ships five top-level
 * destinations; only "Overview" is wired up in this PR because the other
 * routes don't exist yet.
 */

interface NavItem {
  href: string;
  label: string;
  icon: LucideIcon;
}

const NAV: NavItem[] = [
  { href: "/", label: "Overview", icon: LayoutGrid },
  { href: "/timeline", label: "Timeline", icon: Activity },
  { href: "/sessions", label: "Sessions", icon: GitBranch },
  { href: "/agents", label: "Agents", icon: Users },
  { href: "/settings", label: "Settings", icon: Settings },
];

export default function Sidebar({ children }: { children: React.ReactNode }) {
  const [collapsed, setCollapsed] = useState(false);

  useEffect(() => {
    const mq = window.matchMedia("(max-width: 768px)");
    if (mq.matches) setCollapsed(true);
    const handler = (e: MediaQueryListEvent) => setCollapsed(e.matches);
    mq.addEventListener("change", handler);
    return () => mq.removeEventListener("change", handler);
  }, []);

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
            <h1 className="font-display text-base tracking-[0.2em] text-white">
              APOGEE
            </h1>
          ) : (
            <span className="font-display text-sm text-white">A</span>
          )}
          <button
            onClick={() => setCollapsed(!collapsed)}
            className="ml-1 rounded p-1 text-[var(--text-muted)] transition-colors hover:bg-[var(--surface-raised)] hover:text-gray-300"
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
          {NAV.map(({ href, label, icon: Icon }) => {
            // Only "Overview" is active in this scaffold PR; the other
            // routes are placeholders that will light up in future PRs.
            const active = href === "/";
            return (
              <li key={href}>
                <a
                  href={href}
                  aria-disabled={!active}
                  className={`flex items-center gap-2.5 rounded px-3 py-1.5 text-[13px] transition-colors ${
                    active
                      ? "bg-[var(--accent)]/10 font-medium text-[var(--accent)]"
                      : "text-[var(--text-muted)] hover:bg-[var(--surface-raised)] hover:text-gray-200"
                  }`}
                  title={collapsed ? label : undefined}
                >
                  <Icon size={16} strokeWidth={1.5} className="flex-shrink-0" />
                  {!collapsed && <span>{label}</span>}
                </a>
              </li>
            );
          })}
        </ul>

        {/* Footer — build version stub */}
        {!collapsed && (
          <div className="border-t border-[var(--border)] px-4 py-3">
            <p className="font-mono text-[10px] text-[var(--text-muted)]">
              apogee 0.0.0-dev
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
