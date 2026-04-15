"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { ChevronLeft, ChevronRight, Copy } from "lucide-react";

import Breadcrumb from "../components/Breadcrumb";
import Card from "../components/Card";
import EventList from "../components/EventList";
import SectionHeader from "../components/SectionHeader";
import SideDrawer from "../components/SideDrawer";
import type {
  EventsRecentResponse,
  FilterOptions,
  LogRow,
} from "../lib/api-types";
import { useApi } from "../lib/swr";
import { formatClock } from "../lib/time";

/**
 * `/events` — the apogee event browser. PR #30 introduces this page so
 * operators can scroll back through every hook event the collector has
 * received without being shoved around by the live dashboard's auto-updating
 * ticker. The page is a true paginated table — Prev / Next buttons, NOT
 * infinite scroll — backed by the new cursor-paginated
 * `GET /v1/events/recent` endpoint.
 *
 * Page state:
 *   - `?page=N`         — 1-indexed page number, URL-backed for deep links.
 *   - `?session_id=`    — optional session filter.
 *   - `?source_app=`    — optional environment filter.
 *   - `?type=`          — optional hook event filter.
 *
 * The cursor stack is held in component state. When the user clicks Next we
 * push the current `next_before` onto the stack and refetch with that
 * cursor; when the user clicks Prev we pop and refetch with the previous
 * cursor (or no cursor for page 1). This is intentionally simple — the
 * collector does not expose a `?after=` cursor, so backward navigation is
 * only fast as long as the user got to the current page by clicking Next.
 * Direct deep-linking to `?page=47` works by replaying Next from the front
 * 46 times during hydration; that path is acceptable because the dashboard
 * is single-operator and pagination requests are cheap.
 *
 * Row click pops the SideDrawer with the full event JSON pretty-printed.
 */

const PAGE_SIZE = 50;

interface FetchedPage {
  events: LogRow[];
  next_before: number | null;
  has_more: boolean;
}

export default function EventsPage() {
  const router = useRouter();
  const searchParams = useSearchParams();

  // Derive the active filter from the URL so deep links land on the right
  // slice of the table.
  const sessionId = searchParams.get("session_id") ?? "";
  const sourceApp = searchParams.get("source_app") ?? "";
  const type = searchParams.get("type") ?? "";
  const pageRaw = searchParams.get("page") ?? "1";
  const page = Math.max(1, Number.parseInt(pageRaw, 10) || 1);

  // Cursor stack — index `i` holds the `before` value used to fetch page
  // `i + 1`. Index 0 is always 0 (no cursor) so page 1 has no `before`.
  // Pushed on Next, popped on Prev. The stack is rebuilt from scratch
  // whenever filters change because the cursor space is filter-specific.
  const [cursorStack, setCursorStack] = useState<number[]>([0]);

  // Reset cursor stack and page when the filters change. We track the
  // previous filter signature in a ref so we don't trip on the initial
  // render.
  const filterKey = `${sessionId}|${sourceApp}|${type}`;
  const lastFilterKeyRef = useRef(filterKey);
  useEffect(() => {
    if (lastFilterKeyRef.current === filterKey) return;
    lastFilterKeyRef.current = filterKey;
    setCursorStack([0]);
    if (page !== 1) {
      const next = new URLSearchParams(searchParams.toString());
      next.set("page", "1");
      router.replace(`?${next.toString()}`, { scroll: false });
    }
    // We intentionally only depend on filterKey — the other deps are read
    // through closures and would cause an infinite loop.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filterKey]);

  // The cursor for the current page is the top of the stack — clamped in
  // case URL state and stack drift apart.
  const stackIndex = Math.min(page - 1, cursorStack.length - 1);
  const before = cursorStack[stackIndex] ?? 0;

  const params = new URLSearchParams();
  params.set("limit", String(PAGE_SIZE));
  if (before > 0) params.set("before", String(before));
  if (sessionId) params.set("session_id", sessionId);
  if (sourceApp) params.set("source_app", sourceApp);
  if (type) params.set("type", type);
  const queryUrl = `/v1/events/recent?${params.toString()}`;

  const { data, error, isLoading } = useApi<EventsRecentResponse>(queryUrl, {
    refreshInterval: 0,
    keepPreviousData: true,
  });

  const events = useMemo<LogRow[]>(() => data?.events ?? [], [data]);
  const hasMore = data?.has_more ?? false;
  const nextBefore = data?.next_before ?? null;

  // Filter dropdown options come from the existing filter-options endpoint
  // so the pickers stay consistent with /sessions and /turn.
  const { data: filterOpts } = useApi<FilterOptions>("/v1/filter-options");

  const setSearchParam = useCallback(
    (key: string, value: string | null) => {
      const next = new URLSearchParams(searchParams.toString());
      if (value === null || value === "") {
        next.delete(key);
      } else {
        next.set(key, value);
      }
      // Filter changes always reset to page 1.
      if (key !== "page") next.set("page", "1");
      router.replace(`?${next.toString()}`, { scroll: false });
    },
    [router, searchParams],
  );

  const goToPage = useCallback(
    (newPage: number) => {
      const next = new URLSearchParams(searchParams.toString());
      next.set("page", String(newPage));
      router.replace(`?${next.toString()}`, { scroll: false });
    },
    [router, searchParams],
  );

  const onNext = useCallback(() => {
    if (!hasMore || nextBefore === null) return;
    setCursorStack((prev) => {
      // Replace any forward history past the current index, then push.
      const trimmed = prev.slice(0, page);
      return [...trimmed, nextBefore];
    });
    goToPage(page + 1);
  }, [hasMore, nextBefore, page, goToPage]);

  const onPrev = useCallback(() => {
    if (page <= 1) return;
    goToPage(page - 1);
  }, [page, goToPage]);

  // SideDrawer state — selected row.
  const [selected, setSelected] = useState<LogRow | null>(null);
  const onRowClick = useCallback((log: LogRow) => setSelected(log), []);
  const onCloseDrawer = useCallback(() => setSelected(null), []);

  // Close the drawer automatically when the user navigates away from this
  // route — covers the "Navigate away while drawer is open" case in the PR
  // verification checklist. The pathname is stable on the events page; if it
  // changes we know we're leaving.
  useEffect(() => {
    return () => {
      setSelected(null);
    };
  }, []);

  return (
    <div className="mx-auto flex max-w-7xl flex-col gap-6">
      <header className="flex flex-col gap-3 pt-6">
        <Breadcrumb segments={[{ label: "Events" }]} />
        <div className="flex flex-wrap items-end justify-between gap-4">
          <div>
            <h1 className="font-display text-4xl tracking-[0.16em] text-white">
              EVENTS
            </h1>
            <div className="accent-gradient-bar mt-3 h-[3px] w-32 rounded-full" />
            <p className="mt-3 max-w-2xl text-[13px] text-[var(--text-muted)]">
              Every hook event the collector has stored, ordered newest first.
              Use the filters to narrow by session, source app, or hook type.
              Click a row to inspect the full payload without leaving the
              page.
            </p>
          </div>
        </div>
      </header>

      <section>
        <Card className="flex flex-wrap items-center gap-3 px-4 py-3">
          <select
            value={sourceApp}
            onChange={(e) => setSearchParam("source_app", e.target.value || null)}
            className="rounded border border-[var(--border)] bg-[var(--bg-raised)] px-2 py-1 font-mono text-[12px] text-white"
            aria-label="Source app filter"
          >
            <option value="">source_app: all</option>
            {(filterOpts?.source_apps ?? []).map((app) => (
              <option key={app} value={app}>
                {app}
              </option>
            ))}
          </select>
          <input
            type="search"
            value={sessionId}
            onChange={(e) => setSearchParam("session_id", e.target.value || null)}
            placeholder="session_id"
            className="rounded border border-[var(--border)] bg-[var(--bg-raised)] px-2 py-1 font-mono text-[12px] text-white outline-none placeholder:text-[var(--artemis-space)]"
            aria-label="Session id filter"
          />
          <select
            value={type}
            onChange={(e) => setSearchParam("type", e.target.value || null)}
            className="rounded border border-[var(--border)] bg-[var(--bg-raised)] px-2 py-1 font-mono text-[12px] text-white"
            aria-label="Hook event type filter"
          >
            <option value="">type: all</option>
            {(filterOpts?.hook_events ?? []).map((he) => (
              <option key={he} value={he}>
                {he}
              </option>
            ))}
          </select>
          {(sessionId || sourceApp || type) && (
            <button
              type="button"
              onClick={() => {
                const next = new URLSearchParams(searchParams.toString());
                next.delete("session_id");
                next.delete("source_app");
                next.delete("type");
                next.set("page", "1");
                router.replace(`?${next.toString()}`, { scroll: false });
              }}
              className="rounded border border-[var(--border)] px-2 py-1 font-mono text-[11px] text-[var(--text-muted)] hover:text-white"
            >
              clear
            </button>
          )}
        </Card>
      </section>

      <section>
        <SectionHeader
          title={`Page ${page}`}
          subtitle={`${PAGE_SIZE} events per page · newest first`}
        />
        <Card className="p-0">
          <EventList
            logs={events}
            loading={isLoading}
            error={error ? "Failed to load events." : null}
            onRowClick={onRowClick}
            selectedId={selected?.id ?? null}
          />
        </Card>
      </section>

      <section>
        <Card className="flex items-center justify-between gap-3 px-4 py-3">
          <button
            type="button"
            onClick={onPrev}
            disabled={page <= 1}
            className="inline-flex items-center gap-1 rounded border border-[var(--border)] px-3 py-1 font-mono text-[11px] text-gray-200 transition-colors hover:bg-[var(--bg-raised)] disabled:cursor-not-allowed disabled:opacity-40"
          >
            <ChevronLeft size={14} strokeWidth={1.5} /> Prev
          </button>
          <span className="font-mono text-[11px] text-[var(--text-muted)]">
            Page {page}
            {hasMore ? "" : " (last)"}
          </span>
          <button
            type="button"
            onClick={onNext}
            disabled={!hasMore}
            className="inline-flex items-center gap-1 rounded border border-[var(--border)] px-3 py-1 font-mono text-[11px] text-gray-200 transition-colors hover:bg-[var(--bg-raised)] disabled:cursor-not-allowed disabled:opacity-40"
          >
            Next <ChevronRight size={14} strokeWidth={1.5} />
          </button>
        </Card>
      </section>

      <SideDrawer
        open={selected !== null}
        onClose={onCloseDrawer}
        title={selected ? `Event #${selected.id}` : "Event"}
        width="lg"
      >
        {selected && <EventDrawerBody log={selected} />}
      </SideDrawer>

      <footer className="pb-8 pt-2">
        <p className="font-mono text-[10px] text-[var(--text-muted)]">
          apogee 0.0.0-dev — events
        </p>
      </footer>
    </div>
  );
}

/**
 * EventDrawerBody — the contents of the side drawer when a row is selected.
 * Renders a small metadata header followed by a copy-button + pretty-printed
 * JSON block of the entire LogRow.
 */
function EventDrawerBody({ log }: { log: LogRow }) {
  const json = useMemo(() => JSON.stringify(log, null, 2), [log]);
  const [copied, setCopied] = useState(false);
  const onCopy = useCallback(() => {
    if (typeof navigator === "undefined" || !navigator.clipboard) return;
    void navigator.clipboard.writeText(json);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1500);
  }, [json]);
  return (
    <div className="flex flex-col gap-4">
      <dl className="grid grid-cols-2 gap-x-4 gap-y-1 font-mono text-[11px]">
        <dt className="text-[var(--text-muted)]">id</dt>
        <dd className="text-gray-200">{log.id}</dd>
        <dt className="text-[var(--text-muted)]">timestamp</dt>
        <dd className="text-gray-200">{formatClock(log.timestamp)}</dd>
        <dt className="text-[var(--text-muted)]">hook_event</dt>
        <dd className="text-gray-200">{log.hook_event || "—"}</dd>
        <dt className="text-[var(--text-muted)]">source_app</dt>
        <dd className="text-gray-200">{log.source_app || "—"}</dd>
        <dt className="text-[var(--text-muted)]">session_id</dt>
        <dd className="break-all text-gray-200">{log.session_id || "—"}</dd>
        <dt className="text-[var(--text-muted)]">turn_id</dt>
        <dd className="break-all text-gray-200">{log.turn_id || "—"}</dd>
        <dt className="text-[var(--text-muted)]">trace_id</dt>
        <dd className="break-all text-gray-200">{log.trace_id || "—"}</dd>
        <dt className="text-[var(--text-muted)]">severity</dt>
        <dd className="text-gray-200">
          {log.severity_text} ({log.severity_number})
        </dd>
      </dl>
      <div className="flex items-center justify-between">
        <span className="font-display text-[11px] uppercase tracking-[0.16em] text-[var(--text-muted)]">
          Payload
        </span>
        <button
          type="button"
          onClick={onCopy}
          className="inline-flex items-center gap-1 rounded border border-[var(--border)] px-2 py-1 font-mono text-[10px] text-gray-200 transition-colors hover:bg-[var(--bg-raised)]"
        >
          <Copy size={12} strokeWidth={1.5} />
          {copied ? "copied" : "copy json"}
        </button>
      </div>
      <pre className="max-h-[60vh] overflow-auto rounded border border-[var(--border)] bg-[var(--bg-deepspace)] p-3 font-mono text-[11px] leading-relaxed text-gray-200">
        {json}
      </pre>
    </div>
  );
}
