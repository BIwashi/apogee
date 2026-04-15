"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { ChevronLeft, ChevronRight, Copy } from "lucide-react";
import Breadcrumb from "../components/Breadcrumb";
import Card from "../components/Card";
import EventList from "../components/EventList";
import FacetPanel, { type FacetSelections } from "../components/FacetPanel";
import LogHistogram from "../components/LogHistogram";
import SideDrawer from "../components/SideDrawer";
import VersionTag from "../components/VersionTag";
import type {
  EventFacetsResponse,
  EventTimeseriesResponse,
  EventsRecentResponse,
  LogRow,
} from "../lib/api-types";
import { useApi } from "../lib/swr";
import { formatClock } from "../lib/time";

/**
 * `/events` — PR #37 rewrites the old single-column event browser into a
 * Datadog-style triple-panel view:
 *
 *   ┌ breadcrumb + query input + time range ──┐
 *   ├ LogHistogram (80px, stacked, brushable) ─┤
 *   ├ FacetPanel ┬ EventList ─────────────────┤
 *   │  src app   │  newest first              │
 *   │  hook ev   │  click → SideDrawer        │
 *   │  severity  │                            │
 *   │  session   │                            │
 *   └ pagination Prev / Page N / Next ────────┘
 *
 * URL-backed state:
 *   ?q=                — free-text body filter
 *   ?window=           — one of 15m / 1h / 24h / 7d (default 1h)
 *   ?since= / ?until=  — explicit time range (overrides window)
 *   ?facets.<dim>=a,b  — selected facet values, comma separated
 *   ?page=N            — 1-indexed page for forward pagination
 *
 * Data fetching:
 *   - GET /v1/events/facets       facet values + counts (SWR key includes selections)
 *   - GET /v1/events/timeseries   stacked-bar histogram + total count
 *   - GET /v1/events/recent       paginated table (cursor-based, unchanged from PR #30)
 *
 * All three endpoints share the same filter serialiser so a given URL
 * always produces a consistent facet panel, histogram, and table.
 */

const PAGE_SIZE = 50;
const DEFAULT_WINDOW = "1h";
const WINDOW_OPTIONS = ["15m", "1h", "6h", "24h", "7d"] as const;

const FACET_PREFIX = "facets.";
const FACET_KEYS = [
  "source_app",
  "hook_event",
  "severity_text",
  "session_id",
] as const;

function selectionsFromSearch(sp: URLSearchParams): FacetSelections {
  const out: FacetSelections = {};
  for (const key of FACET_KEYS) {
    const raw = sp.get(FACET_PREFIX + key);
    if (!raw) continue;
    const set = new Set<string>();
    for (const v of raw.split(",")) {
      const trimmed = v.trim();
      if (trimmed) set.add(trimmed);
    }
    if (set.size > 0) out[key] = set;
  }
  return out;
}

function appendSelectionsToParams(
  params: URLSearchParams,
  selections: FacetSelections,
) {
  for (const key of FACET_KEYS) {
    const set = selections[key];
    if (!set || set.size === 0) continue;
    const csv = Array.from(set).join(",");
    params.set(FACET_PREFIX + key, csv);
  }
}

function appendSelectionsToBackendParams(
  params: URLSearchParams,
  selections: FacetSelections,
) {
  // Backend accepts the `facets.*` form directly so we can pass the URL
  // through unchanged; the multiQuery helper parses comma-separated values.
  appendSelectionsToParams(params, selections);
}

export default function EventsPage() {
  const router = useRouter();
  const searchParams = useSearchParams();

  const q = searchParams.get("q") ?? "";
  const windowParam = searchParams.get("window") ?? DEFAULT_WINDOW;
  const since = searchParams.get("since") ?? "";
  const until = searchParams.get("until") ?? "";
  const pageRaw = searchParams.get("page") ?? "1";
  const page = Math.max(1, Number.parseInt(pageRaw, 10) || 1);

  const selections = useMemo<FacetSelections>(
    () => selectionsFromSearch(searchParams),
    [searchParams],
  );

  // Cursor stack for forward pagination. Matches the PR #30 flow so
  // deep-linking to ?page=N replays Next N-1 times from the front.
  const [cursorStack, setCursorStack] = useState<number[]>([0]);

  // Stable signature of the filter so the cursor stack resets whenever
  // any facet / query / window changes.
  const filterKey = useMemo(() => {
    const parts: string[] = [q, windowParam, since, until];
    for (const key of FACET_KEYS) {
      parts.push(
        `${key}=${Array.from(selections[key] ?? [])
          .sort()
          .join(",")}`,
      );
    }
    return parts.join("|");
  }, [q, windowParam, since, until, selections]);
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
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filterKey]);

  const stackIndex = Math.min(page - 1, cursorStack.length - 1);
  const before = cursorStack[stackIndex] ?? 0;

  // -- URL helpers ---------------------------------------------------------

  const replaceSearch = useCallback(
    (mutator: (params: URLSearchParams) => void) => {
      const next = new URLSearchParams(searchParams.toString());
      mutator(next);
      router.replace(`?${next.toString()}`, { scroll: false });
    },
    [router, searchParams],
  );

  const onQueryChange = useCallback(
    (value: string) => {
      replaceSearch((p) => {
        if (value) p.set("q", value);
        else p.delete("q");
        p.set("page", "1");
      });
    },
    [replaceSearch],
  );

  const onWindowChange = useCallback(
    (value: string) => {
      replaceSearch((p) => {
        p.set("window", value);
        p.delete("since");
        p.delete("until");
        p.set("page", "1");
      });
    },
    [replaceSearch],
  );

  const onSelectionChange = useCallback(
    (next: FacetSelections) => {
      replaceSearch((p) => {
        for (const key of FACET_KEYS) {
          p.delete(FACET_PREFIX + key);
        }
        appendSelectionsToParams(p, next);
        p.set("page", "1");
      });
    },
    [replaceSearch],
  );

  const onBrush = useCallback(
    (sinceIso: string, untilIso: string) => {
      replaceSearch((p) => {
        p.set("since", sinceIso);
        p.set("until", untilIso);
        p.delete("window");
        p.set("page", "1");
      });
    },
    [replaceSearch],
  );

  // -- Backend query URLs --------------------------------------------------

  const baseFilterParams = useMemo(() => {
    const p = new URLSearchParams();
    if (q) p.set("q", q);
    if (since) p.set("since", since);
    if (until) p.set("until", until);
    if (!since && !until) p.set("window", windowParam || DEFAULT_WINDOW);
    appendSelectionsToBackendParams(p, selections);
    return p;
  }, [q, since, until, windowParam, selections]);

  const facetsUrl = `/v1/events/facets?${baseFilterParams.toString()}`;
  const timeseriesUrl = `/v1/events/timeseries?${baseFilterParams.toString()}`;

  const recentParams = useMemo(() => {
    const p = new URLSearchParams(baseFilterParams.toString());
    p.set("limit", String(PAGE_SIZE));
    if (before > 0) p.set("before", String(before));
    return p;
  }, [baseFilterParams, before]);
  const recentUrl = `/v1/events/recent?${recentParams.toString()}`;

  const { data: facetsData, isLoading: facetsLoading } =
    useApi<EventFacetsResponse>(facetsUrl);
  const { data: tsData, isLoading: tsLoading } =
    useApi<EventTimeseriesResponse>(timeseriesUrl);
  const {
    data: eventsData,
    error: eventsError,
    isLoading: eventsLoading,
  } = useApi<EventsRecentResponse>(recentUrl, { keepPreviousData: true });

  const events = useMemo<LogRow[]>(
    () => eventsData?.events ?? [],
    [eventsData],
  );
  const hasMore = eventsData?.has_more ?? false;
  const nextBefore = eventsData?.next_before ?? null;

  const goToPage = useCallback(
    (newPage: number) => {
      replaceSearch((p) => p.set("page", String(newPage)));
    },
    [replaceSearch],
  );

  const onNext = useCallback(() => {
    if (!hasMore || nextBefore === null) return;
    setCursorStack((prev) => {
      const trimmed = prev.slice(0, page);
      return [...trimmed, nextBefore];
    });
    goToPage(page + 1);
  }, [hasMore, nextBefore, page, goToPage]);

  const onPrev = useCallback(() => {
    if (page <= 1) return;
    goToPage(page - 1);
  }, [page, goToPage]);

  // -- SideDrawer ----------------------------------------------------------

  const [selected, setSelected] = useState<LogRow | null>(null);
  const onRowClick = useCallback((log: LogRow) => setSelected(log), []);
  const onCloseDrawer = useCallback(() => setSelected(null), []);
  useEffect(() => () => setSelected(null), []);

  // -- First-paint timing probe -------------------------------------------

  // Log a single first-paint number the first time all three queries
  // resolve. Captured in PR #37 before/after benchmarks.
  const firstPaintLoggedRef = useRef(false);
  useEffect(() => {
    if (firstPaintLoggedRef.current) return;
    if (facetsData && tsData && eventsData) {
      firstPaintLoggedRef.current = true;
      if (typeof performance !== "undefined") {
        console.info(
          `[apogee] /events first paint at ${performance.now().toFixed(0)}ms (PR #37 Datadog rewrite)`,
        );
      }
    }
  }, [facetsData, tsData, eventsData]);

  const totalEvents = tsData?.total ?? 0;

  return (
    <div className="mx-auto flex max-w-7xl flex-col gap-4">
      <header className="flex flex-col gap-3 pt-6">
        <Breadcrumb segments={[{ label: "Events" }]} />
        <div className="flex flex-wrap items-end justify-between gap-4">
          <div>
            <h1 className="font-display-accent text-4xl tracking-[0.16em] text-[var(--artemis-white)]">
              EVENTS
            </h1>
            <div className="accent-gradient-bar mt-3 h-[3px] w-32 rounded-full" />
            <p className="mt-3 max-w-2xl text-[13px] text-[var(--text-muted)]">
              {totalEvents.toLocaleString()} events ·{" "}
              {since && until
                ? `custom range`
                : `last ${windowParam || DEFAULT_WINDOW}`}
            </p>
          </div>
        </div>
      </header>

      <section>
        <Card className="flex flex-wrap items-center gap-2 px-4 py-3">
          <input
            type="search"
            value={q}
            onChange={(e) => onQueryChange(e.target.value)}
            placeholder="search body…"
            className="min-w-[220px] flex-1 rounded border border-[var(--border)] bg-[var(--bg-raised)] px-2 py-1 font-mono text-[12px] text-[var(--artemis-white)] outline-none placeholder:text-[var(--artemis-space)]"
            aria-label="Free-text body search"
          />
          <select
            value={since || until ? "custom" : windowParam}
            onChange={(e) => {
              if (e.target.value === "custom") return;
              onWindowChange(e.target.value);
            }}
            className="rounded border border-[var(--border)] bg-[var(--bg-raised)] px-2 py-1 font-mono text-[12px] text-[var(--artemis-white)]"
            aria-label="Time range"
          >
            {WINDOW_OPTIONS.map((w) => (
              <option key={w} value={w}>
                last {w}
              </option>
            ))}
            {(since || until) && <option value="custom">custom range</option>}
          </select>
          {(since || until) && (
            <button
              type="button"
              onClick={() =>
                replaceSearch((p) => {
                  p.delete("since");
                  p.delete("until");
                  p.set("window", DEFAULT_WINDOW);
                })
              }
              className="rounded border border-[var(--border)] px-2 py-1 font-mono text-[11px] text-[var(--text-muted)] hover:text-[var(--artemis-white)]"
            >
              clear range
            </button>
          )}
        </Card>
      </section>

      <section>
        <LogHistogram
          buckets={tsData?.buckets ?? []}
          total={totalEvents}
          loading={tsLoading}
          onBrush={onBrush}
        />
      </section>

      <section className="flex flex-col gap-3 md:flex-row">
        <FacetPanel
          facets={facetsData?.facets}
          loading={facetsLoading}
          selections={selections}
          onSelectionChange={onSelectionChange}
        />
        <Card className="flex-1 p-0">
          <EventList
            logs={events}
            loading={eventsLoading}
            error={eventsError ? "Failed to load events." : null}
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
            className="inline-flex items-center gap-1 rounded border border-[var(--border)] px-3 py-1 font-mono text-[11px] text-[var(--artemis-white)] transition-colors hover:bg-[var(--bg-raised)] disabled:cursor-not-allowed disabled:opacity-40"
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
            className="inline-flex items-center gap-1 rounded border border-[var(--border)] px-3 py-1 font-mono text-[11px] text-[var(--artemis-white)] transition-colors hover:bg-[var(--bg-raised)] disabled:cursor-not-allowed disabled:opacity-40"
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
        <VersionTag suffix="events" />
      </footer>
    </div>
  );
}

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
        <dd className="text-[var(--text-primary)]">{log.id}</dd>
        <dt className="text-[var(--text-muted)]">timestamp</dt>
        <dd className="text-[var(--text-primary)]">
          {formatClock(log.timestamp)}
        </dd>
        <dt className="text-[var(--text-muted)]">hook_event</dt>
        <dd className="text-[var(--text-primary)]">{log.hook_event || "—"}</dd>
        <dt className="text-[var(--text-muted)]">source_app</dt>
        <dd className="text-[var(--text-primary)]">{log.source_app || "—"}</dd>
        <dt className="text-[var(--text-muted)]">session_id</dt>
        <dd className="break-all text-[var(--artemis-white)]">
          {log.session_id || "—"}
        </dd>
        <dt className="text-[var(--text-muted)]">turn_id</dt>
        <dd className="break-all text-[var(--artemis-white)]">
          {log.turn_id || "—"}
        </dd>
        <dt className="text-[var(--text-muted)]">trace_id</dt>
        <dd className="break-all text-[var(--artemis-white)]">
          {log.trace_id || "—"}
        </dd>
        <dt className="text-[var(--text-muted)]">severity</dt>
        <dd className="text-[var(--text-primary)]">
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
          className="inline-flex items-center gap-1 rounded border border-[var(--border)] px-2 py-1 font-mono text-[10px] text-[var(--artemis-white)] transition-colors hover:bg-[var(--bg-raised)]"
        >
          <Copy size={12} strokeWidth={1.5} />
          {copied ? "copied" : "copy json"}
        </button>
      </div>
      <pre className="max-h-[60vh] overflow-auto rounded border border-[var(--border)] bg-[var(--bg-deepspace)] p-3 font-mono text-[11px] leading-relaxed text-[var(--artemis-white)]">
        {json}
      </pre>
    </div>
  );
}
