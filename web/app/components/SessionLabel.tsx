"use client";

import { useCallback } from "react";

import type { SessionSummary } from "../lib/api-types";
import { useApi } from "../lib/swr";
import { useDrawerState } from "../lib/drawer";

/**
 * SessionLabel — the cross-cutting "session id → something human" helper
 * component. Raw session ids like `sess-alpha1234...` are meaningless on
 * their own; every table row that shows one must also show a short headline
 * and the source_app so the operator can tell two sessions apart at a
 * glance.
 *
 * PR #36 introduces this primitive and replaces the raw-text session column
 * across `/agents`, `/sessions`, and the session-detail Turns tab. The
 * component also doubles as the trigger that opens the cross-cutting
 * SessionDrawer in place, matching the Datadog row-click pattern used by
 * every other table on the dashboard.
 *
 * Rendering:
 *
 *   sess-abc1234  ·  my-api        <- mono, one line, --artemis-white
 *     "refactor auth middleware"   <- body, 1 line ellipsis, --text-muted
 *
 * When `headline` is not passed the component fetches
 * `/v1/sessions/:id/summary` on demand via SWR. Multiple rows pointing at
 * the same session share the SWR cache key so the network only sees one
 * request per id.
 */

export interface SessionLabelProps {
  sessionID: string;
  sourceApp?: string | null;
  headline?: string | null;
  size?: "sm" | "md";
  clickable?: boolean;
}

function shortId(id: string, len = 8): string {
  if (!id) return "—";
  return id.length <= len ? id : id.slice(0, len);
}

export default function SessionLabel({
  sessionID,
  sourceApp,
  headline,
  size = "sm",
  clickable = true,
}: SessionLabelProps) {
  const needsFetch = !headline && !sourceApp;
  const { data } = useApi<SessionSummary>(
    needsFetch && sessionID ? `/v1/sessions/${sessionID}/summary` : null,
    { refreshInterval: 0 },
  );
  const resolvedSource = sourceApp ?? data?.source_app ?? "";
  const resolvedHeadline = headline ?? data?.latest_headline ?? "";

  const { open } = useDrawerState();

  const onClick = useCallback(
    (event: React.MouseEvent<HTMLElement>) => {
      if (!clickable) return;
      if (event.defaultPrevented) return;
      if (event.button !== 0) return;
      if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) {
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      open({ kind: "session", id: sessionID });
    },
    [clickable, open, sessionID],
  );

  const idClass =
    size === "md"
      ? "font-mono text-[12px] text-[var(--artemis-white)]"
      : "font-mono text-[11px] text-[var(--artemis-white)]";
  const metaClass =
    size === "md" ? "text-[11px]" : "text-[10px]";

  const content = (
    <span className="flex min-w-0 flex-col gap-0.5">
      <span className="flex min-w-0 items-center gap-1">
        <span className={idClass} title={sessionID}>
          {shortId(sessionID)}
        </span>
        {resolvedSource ? (
          <>
            <span
              className={`${metaClass} text-[var(--text-muted)]`}
              aria-hidden
            >
              ·
            </span>
            <span
              className={`${metaClass} truncate text-[var(--text-muted)]`}
              title={resolvedSource}
            >
              {resolvedSource}
            </span>
          </>
        ) : null}
      </span>
      {resolvedHeadline ? (
        <span
          className={`${metaClass} truncate text-[var(--text-muted)]`}
          title={resolvedHeadline}
        >
          {resolvedHeadline}
        </span>
      ) : null}
    </span>
  );

  if (!clickable) {
    return <span className="inline-flex min-w-0 max-w-full">{content}</span>;
  }

  return (
    <a
      href={`/session/?id=${sessionID}&tab=overview`}
      onClick={onClick}
      className="inline-flex min-w-0 max-w-full rounded text-left transition-colors hover:text-[var(--accent)] focus:outline-none focus-visible:ring-1 focus-visible:ring-[var(--border-bright)]"
    >
      {content}
    </a>
  );
}
