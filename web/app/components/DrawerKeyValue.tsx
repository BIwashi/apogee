"use client";

import { type ReactNode, useCallback } from "react";
import { Copy } from "lucide-react";

/**
 * DrawerKeyValue — a vertically stacked key/value grid used inside every
 * cross-cutting drawer's Details tab. Keys are small, uppercase, muted; values
 * are mono or sans depending on the `mono` flag and may optionally carry a
 * copy button for ids / hashes the operator frequently wants in the clipboard.
 *
 * This primitive is intentionally typed strictly — rows are plain data, the
 * caller is free to pass a ReactNode for rich values (chips, links, nested
 * spans). Nothing here pulls data, so it has no effect on SWR cache keys.
 */

export interface DrawerKeyValueRow {
  label: string;
  value: ReactNode;
  mono?: boolean;
  copyable?: string;
  tone?: "default" | "muted" | "accent" | "warning" | "critical" | "success";
}

interface DrawerKeyValueProps {
  rows: DrawerKeyValueRow[];
}

const TONE_COLOR: Record<NonNullable<DrawerKeyValueRow["tone"]>, string> = {
  default: "var(--artemis-white)",
  muted: "var(--text-muted)",
  accent: "var(--accent)",
  warning: "var(--status-warning)",
  critical: "var(--status-critical)",
  success: "var(--status-success)",
};

function CopyButton({ text }: { text: string }) {
  const onClick = useCallback(
    (event: React.MouseEvent<HTMLButtonElement>) => {
      event.stopPropagation();
      if (typeof navigator === "undefined" || !navigator.clipboard) return;
      void navigator.clipboard.writeText(text);
    },
    [text],
  );
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label="Copy to clipboard"
      title="Copy"
      className="inline-flex items-center justify-center rounded p-1 text-[var(--text-muted)] transition-colors hover:bg-[var(--bg-raised)] hover:text-[var(--artemis-white)]"
    >
      <Copy size={12} strokeWidth={1.5} />
    </button>
  );
}

export default function DrawerKeyValue({ rows }: DrawerKeyValueProps) {
  return (
    <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-2 text-[12px]">
      {rows.map((row, idx) => {
        const color = TONE_COLOR[row.tone ?? "default"];
        const valueClass = row.mono ? "font-mono text-[11px]" : "text-[12px]";
        return (
          <div key={`${row.label}-${idx}`} className="contents">
            <dt className="self-start font-display text-[10px] uppercase tracking-[0.14em] text-[var(--text-muted)]">
              {row.label}
            </dt>
            <dd className={`self-start ${valueClass}`} style={{ color }}>
              <span className="inline-flex min-w-0 max-w-full items-center gap-1">
                <span className="min-w-0 break-words">{row.value}</span>
                {row.copyable ? <CopyButton text={row.copyable} /> : null}
              </span>
            </dd>
          </div>
        );
      })}
    </dl>
  );
}

interface DrawerSectionProps {
  title: string;
  children: ReactNode;
  action?: ReactNode;
}

/**
 * DrawerSection — a titled wrapper inside a drawer body. Keeps the visual
 * rhythm consistent across Details / Attributes / Events tabs.
 */
export function DrawerSection({ title, children, action }: DrawerSectionProps) {
  return (
    <section className="flex flex-col gap-2">
      <div className="flex items-center justify-between gap-2">
        <h4 className="font-display text-[10px] uppercase tracking-[0.16em] text-[var(--text-muted)]">
          {title}
        </h4>
        {action}
      </div>
      {children}
    </section>
  );
}
