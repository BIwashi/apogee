"use client";

import Link from "next/link";
import { ArrowRight, type LucideIcon } from "lucide-react";
import type { ReactNode } from "react";

/**
 * DrawerFooterAction — the Datadog-style "Open full page →" primary CTA that
 * lives pinned at the bottom of every cross-cutting drawer.
 *
 * Two shapes:
 *   - `href` → renders as a Next.js <Link>. Plain left-click navigates as
 *     usual; the operator can still Cmd+Click to open in a new tab.
 *   - `onClick` → renders as a <button>, used for "Copy span JSON" type
 *     side-effect actions that stay inside the drawer.
 */

interface BaseProps {
  label: ReactNode;
  icon?: LucideIcon;
  tone?: "accent" | "muted";
}

interface LinkProps extends BaseProps {
  href: string;
  onClick?: never;
}

interface ButtonProps extends BaseProps {
  onClick: () => void;
  href?: never;
}

type DrawerFooterActionProps = LinkProps | ButtonProps;

export default function DrawerFooterAction(props: DrawerFooterActionProps) {
  const { label, icon: Icon = ArrowRight, tone = "accent" } = props;
  const color = tone === "accent" ? "var(--accent)" : "var(--text-muted)";
  const classes =
    "mt-4 flex items-center justify-between gap-2 rounded border px-3 py-2 font-display text-[11px] uppercase tracking-[0.16em] transition-colors hover:bg-[var(--bg-raised)]";
  const style = { borderColor: color, color };

  if ("href" in props && props.href !== undefined) {
    return (
      <Link href={props.href} className={classes} style={style}>
        <span>{label}</span>
        <Icon size={14} strokeWidth={1.5} />
      </Link>
    );
  }
  return (
    <button
      type="button"
      onClick={props.onClick}
      className={`${classes} w-full`}
      style={style}
    >
      <span>{label}</span>
      <Icon size={14} strokeWidth={1.5} />
    </button>
  );
}
