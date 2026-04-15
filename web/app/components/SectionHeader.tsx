import type { ReactNode } from "react";

/**
 * SectionHeader — Artemis-Inter uppercase title used above cards, grids, and
 * tables. The small accent bar on the left locks the header to the brand
 * gradient without needing a hero-sized type treatment.
 */

interface SectionHeaderProps {
  title: string;
  subtitle?: ReactNode;
  actions?: ReactNode;
}

export default function SectionHeader({
  title,
  subtitle,
  actions,
}: SectionHeaderProps) {
  return (
    <div className="mb-3 flex items-end justify-between gap-4">
      <div className="flex items-center gap-3">
        <span className="accent-gradient-bar inline-block h-5 w-[3px] rounded-sm" />
        <div>
          <h2 className="font-display text-[13px] text-[var(--artemis-white)]">
            {title}
          </h2>
          {subtitle && (
            <p className="mt-0.5 text-[11px] text-[var(--text-muted)]">
              {subtitle}
            </p>
          )}
        </div>
      </div>
      {actions && <div className="flex items-center gap-2">{actions}</div>}
    </div>
  );
}
