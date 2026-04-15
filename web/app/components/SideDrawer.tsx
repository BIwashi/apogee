"use client";

import { X } from "lucide-react";
import {
  useCallback,
  useEffect,
  useRef,
  type KeyboardEvent,
  type ReactNode,
} from "react";

/**
 * SideDrawer — Datadog-style overlay that slides in from the right edge of
 * the viewport. PR #30 introduces this primitive so the `/events` page (and
 * any future surface that needs to inspect a row in detail without losing
 * the surrounding context) can present the full payload as an overlay
 * instead of navigating away.
 *
 * Behavior:
 *   - Slide animation: `translate-x-full → translate-x-0` over 200 ms.
 *   - Backdrop fades in with the panel; clicking the backdrop closes the
 *     drawer the same way the X button or the Escape key does.
 *   - Focus is trapped inside the panel while it is open. The first
 *     focusable element (the close button) receives focus on open; Tab /
 *     Shift+Tab cycle through the focusable elements without leaking out
 *     to the underlying page.
 *   - When the drawer closes the previously focused element is restored,
 *     so keyboard navigation feels coherent.
 *
 * Three width tiers are exposed via the `width` prop: `md` (480 px),
 * `lg` (640 px), and `xl` (800 px). Default is `md`.
 */

interface SideDrawerProps {
  open: boolean;
  onClose: () => void;
  title: string;
  children: ReactNode;
  /** Visual width tier — defaults to `md` (480 px). */
  width?: "md" | "lg" | "xl";
}

const WIDTH_PX: Record<NonNullable<SideDrawerProps["width"]>, number> = {
  md: 480,
  lg: 640,
  xl: 800,
};

const FOCUSABLE_SELECTOR =
  'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])';

export default function SideDrawer({
  open,
  onClose,
  title,
  children,
  width = "md",
}: SideDrawerProps) {
  const panelRef = useRef<HTMLDivElement | null>(null);
  const closeButtonRef = useRef<HTMLButtonElement | null>(null);
  const previouslyFocusedRef = useRef<HTMLElement | null>(null);

  // Esc handler. Bound to the window so the drawer reacts even if focus has
  // momentarily left the panel (e.g. while clicking the backdrop).
  useEffect(() => {
    if (!open) return;
    const onKeyDown = (e: globalThis.KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        onClose();
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [open, onClose]);

  // Focus management. Open: remember the trigger and focus the close
  // button. Close: restore focus to the trigger.
  useEffect(() => {
    if (open) {
      previouslyFocusedRef.current = (document.activeElement as HTMLElement | null) ?? null;
      // setTimeout 0 lets the panel mount before grabbing focus.
      const id = window.setTimeout(() => {
        closeButtonRef.current?.focus();
      }, 0);
      return () => window.clearTimeout(id);
    }
    if (previouslyFocusedRef.current) {
      previouslyFocusedRef.current.focus();
      previouslyFocusedRef.current = null;
    }
  }, [open]);

  // Body scroll lock — cheap CSS toggle so the page doesn't scroll behind
  // the overlay while the drawer is open.
  useEffect(() => {
    if (!open) return;
    const previous = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = previous;
    };
  }, [open]);

  const onPanelKeyDown = useCallback(
    (e: KeyboardEvent<HTMLDivElement>) => {
      if (e.key !== "Tab") return;
      const root = panelRef.current;
      if (!root) return;
      const focusables = root.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR);
      if (focusables.length === 0) return;
      const first = focusables[0];
      const last = focusables[focusables.length - 1];
      const active = document.activeElement as HTMLElement | null;
      if (e.shiftKey) {
        if (active === first || !root.contains(active)) {
          e.preventDefault();
          last.focus();
        }
      } else if (active === last) {
        e.preventDefault();
        first.focus();
      }
    },
    [],
  );

  const widthPx = WIDTH_PX[width];

  return (
    <div
      aria-hidden={!open}
      className={`fixed inset-0 z-50 ${open ? "pointer-events-auto" : "pointer-events-none"}`}
    >
      {/* Backdrop */}
      <div
        onClick={onClose}
        className={`absolute inset-0 transition-opacity duration-200 ${open ? "opacity-100" : "opacity-0"}`}
        style={{ background: "color-mix(in srgb, var(--bg-deepspace) 75%, transparent)" }}
        aria-hidden
      />
      {/* Panel */}
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-label={title}
        onKeyDown={onPanelKeyDown}
        tabIndex={-1}
        className={`absolute inset-y-0 right-0 flex h-full max-w-full flex-col border-l border-[var(--border)] bg-[var(--bg-surface)] shadow-2xl transition-transform duration-200 ease-out ${
          open ? "translate-x-0" : "translate-x-full"
        }`}
        style={{ width: `${widthPx}px` }}
      >
        <header className="flex items-center justify-between gap-3 border-b border-[var(--border)] px-4 py-3">
          <h2 className="font-display text-[12px] uppercase tracking-[0.16em] text-white">
            {title}
          </h2>
          <button
            ref={closeButtonRef}
            type="button"
            onClick={onClose}
            aria-label="Close drawer"
            className="rounded p-1 text-[var(--text-muted)] transition-colors hover:bg-[var(--bg-raised)] hover:text-white focus:outline-none focus-visible:ring-1 focus-visible:ring-[var(--border-bright)]"
          >
            <X size={16} strokeWidth={1.5} />
          </button>
        </header>
        <div className="flex-1 overflow-y-auto px-4 py-3">{children}</div>
      </div>
    </div>
  );
}
