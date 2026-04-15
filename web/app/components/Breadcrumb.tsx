import Link from "next/link";
import { ChevronRight } from "lucide-react";

/**
 * Breadcrumb — minimal navigation primitive for nested detail views. Each
 * segment renders as a mono-typed id fragment, separated by a chevron icon.
 * The final segment is rendered in the accent text color and is never linked,
 * even if a href is provided.
 */

export interface BreadcrumbSegment {
  label: string;
  href?: string;
}

interface BreadcrumbProps {
  segments: BreadcrumbSegment[];
}

export default function Breadcrumb({ segments }: BreadcrumbProps) {
  return (
    <nav
      aria-label="Breadcrumb"
      className="flex items-center gap-1 text-[11px] text-[var(--text-muted)]"
    >
      {segments.map((segment, idx) => {
        const isLast = idx === segments.length - 1;
        const cls = isLast
          ? "font-mono text-[var(--accent)]"
          : "font-mono hover:text-[var(--artemis-white)]";
        return (
          <span key={`${segment.label}-${idx}`} className="inline-flex items-center gap-1">
            {idx > 0 && (
              <ChevronRight
                size={12}
                strokeWidth={1.5}
                className="text-[var(--border-bright)]"
                aria-hidden
              />
            )}
            {segment.href && !isLast ? (
              <Link href={segment.href} className={cls}>
                {segment.label}
              </Link>
            ) : (
              <span className={cls}>{segment.label}</span>
            )}
          </span>
        );
      })}
    </nav>
  );
}
