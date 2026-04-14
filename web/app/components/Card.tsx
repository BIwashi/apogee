import type { ReactNode } from "react";

/**
 * Card — the canonical apogee surface primitive. Renders a dark panel with a
 * hairline border and 10px radius, matching `docs/design-tokens.md`.
 */

interface CardProps {
  children: ReactNode;
  className?: string;
  raised?: boolean;
}

export default function Card({ children, className = "", raised = false }: CardProps) {
  const base = raised ? "surface-card-raised" : "surface-card";
  return <div className={`${base} p-4 ${className}`}>{children}</div>;
}
