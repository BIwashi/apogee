"use client";

import { usePathname } from "next/navigation";
import { useState, useSyncExternalStore } from "react";
import {
  Activity,
  GitBranch,
  LayoutGrid,
  Palette,
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
  disabled?: boolean;
}

const NAV: NavItem[] = [
  { href: "/", label: "Overview", icon: LayoutGrid },
  { href: "/timeline", label: "Timeline", icon: Activity, disabled: true },
  { href: "/sessions", label: "Sessions", icon: GitBranch, disabled: true },
  { href: "/agents", label: "Agents", icon: Users, disabled: true },
  { href: "/settings", label: "Settings", icon: Settings, disabled: true },
  { href: "/styleguide", label: "Styleguide", icon: Palette },
];

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
            <h1 className="font-display text-base tracking-[0.2em] text-white">
              APOGEE
            </h1>
          ) : (
            <span className="font-display text-sm text-white">A</span>
          )}
          <button
            onClick={() => setUserCollapsed(!collapsed)}
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
          {NAV.map(({ href, label, icon: Icon, disabled }) => {
            const active = pathname === href;
            const base =
              "flex items-center gap-2.5 rounded px-3 py-1.5 text-[13px] transition-colors";
            const tone = disabled
              ? "cursor-not-allowed text-[var(--text-muted)] opacity-50"
              : active
                ? "bg-[var(--accent)]/10 font-medium text-[var(--accent)]"
                : "text-[var(--text-muted)] hover:bg-[var(--surface-raised)] hover:text-gray-200";
            const body = (
              <>
                <Icon size={16} strokeWidth={1.5} className="flex-shrink-0" />
                {!collapsed && <span>{label}</span>}
              </>
            );
            return (
              <li key={href}>
                {disabled ? (
                  <span
                    aria-disabled
                    className={`${base} ${tone}`}
                    title={collapsed ? `${label} (coming soon)` : "coming soon"}
                  >
                    {body}
                  </span>
                ) : (
                  <a
                    href={href}
                    className={`${base} ${tone}`}
                    title={collapsed ? label : undefined}
                  >
                    {body}
                  </a>
                )}
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
