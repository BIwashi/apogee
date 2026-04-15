"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";

import type { ApogeeEvent } from "./api-types";
import { apiUrl } from "./api";

/**
 * sse.tsx — layout-scoped, long-lived Server-Sent Events provider.
 *
 * A single `EventSource` is opened at mount time by `<SSEProvider>` (which
 * lives directly under the root `layout.tsx`) and survives every subsequent
 * client-side navigation. Before PR #26 every route mounted its own
 * `useEventStream`, which closed and re-opened the stream on navigation and
 * caused the top-ribbon LIVE indicator to flash "connecting" on every click.
 * The provider owns:
 *
 *   - The native `EventSource` connection against `/v1/events/stream`.
 *     Filtering used to happen server-side via `?session_id=` — that
 *     parameter is still honoured by the backend, we just stopped using it
 *     from the web UI in favour of a client-side filter (see below).
 *   - A capped ring buffer of every event seen (newest first, 500 items).
 *   - An imperative `subscribe()` fan-out for components that want to
 *     react to each event without waiting for a re-render.
 *   - Reconnect with exponential backoff (500 ms → 10 s), unchanged from
 *     the old hook.
 *
 * Consumers call `useEventStream(filter?)`, a thin wrapper around
 * `useContext` that narrows `history` / `lastEvent` by an optional
 * `{ sessionId?, types? }` filter. Same return shape as the old hook so
 * existing call sites need minimal rewrites.
 */

export type StreamStatus = "connecting" | "open" | "closed" | "error";

/** Subscribe-level filter passed to `useEventStream`. */
export interface EventFilter {
  /**
   * Only keep events whose payload references this session id. Matches
   * `data.turn.session_id`, `data.span.session_id`, `data.session.session_id`,
   * `data.hitl.session_id`, or `data.intervention.session_id` — whichever
   * the payload happens to carry.
   */
  sessionId?: string;
  /** Restrict to these SSE event types (e.g. `["turn.started", "turn.ended"]`). */
  types?: readonly string[];
}

export interface UseEventStreamResult<T> {
  status: StreamStatus;
  lastEvent: T | null;
  history: T[];
  subscribe: (cb: (event: T) => void) => () => void;
}

interface SSEContextValue {
  status: StreamStatus;
  lastEvent: ApogeeEvent | null;
  history: readonly ApogeeEvent[];
  subscribe: (cb: (event: ApogeeEvent) => void) => () => void;
}

const HISTORY_LIMIT = 500;
const BACKOFF_START_MS = 500;
const BACKOFF_MAX_MS = 10_000;
const STREAM_PATH = "/v1/events/stream";

const EMPTY_HISTORY: readonly ApogeeEvent[] = Object.freeze([]);

const SSEContext = createContext<SSEContextValue>({
  status: "connecting",
  lastEvent: null,
  history: EMPTY_HISTORY,
  subscribe: () => () => {},
});

/**
 * SSEProvider — opens a single long-lived EventSource and fans every event
 * out to context consumers. Must be mounted inside the root layout so it
 * survives `router.push()` navigations. Renders `children` unchanged during
 * SSR; the EventSource is only created in `useEffect`.
 */
export function SSEProvider({ children }: { children: React.ReactNode }) {
  const [status, setStatus] = useState<StreamStatus>("connecting");
  const [lastEvent, setLastEvent] = useState<ApogeeEvent | null>(null);
  const [history, setHistory] = useState<ApogeeEvent[]>([]);

  // Imperative subscribers stored in a ref so adding/removing one does not
  // trigger a re-render of the whole provider.
  const subscribersRef = useRef<Set<(event: ApogeeEvent) => void>>(
    new Set<(event: ApogeeEvent) => void>(),
  );

  const subscribe = useCallback((cb: (event: ApogeeEvent) => void) => {
    const set = subscribersRef.current;
    set.add(cb);
    return () => {
      set.delete(cb);
    };
  }, []);

  useEffect(() => {
    // Guard against SSR / static export — EventSource is a browser API.
    if (typeof window === "undefined" || typeof EventSource === "undefined") {
      return;
    }

    let source: EventSource | null = null;
    let backoff = BACKOFF_START_MS;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
    let closed = false;

    const handleRaw = (raw: string) => {
      let parsed: ApogeeEvent;
      try {
        parsed = JSON.parse(raw) as ApogeeEvent;
      } catch {
        return;
      }
      setLastEvent(parsed);
      setHistory((prev) => {
        const next = [parsed, ...prev];
        if (next.length > HISTORY_LIMIT) next.length = HISTORY_LIMIT;
        return next;
      });
      // Fan out to imperative subscribers synchronously — this is the whole
      // point of the ref-backed set; consumers don't wait for a re-render.
      for (const cb of subscribersRef.current) {
        try {
          cb(parsed);
        } catch {
          // Subscriber errors must not break the stream.
        }
      }
    };

    const connect = () => {
      setStatus("connecting");
      try {
        source = new EventSource(apiUrl(STREAM_PATH));
      } catch {
        setStatus("error");
        scheduleReconnect();
        return;
      }

      source.onopen = () => {
        backoff = BACKOFF_START_MS;
        setStatus("open");
      };

      source.onmessage = (ev: MessageEvent<string>) => {
        handleRaw(ev.data);
      };

      source.onerror = () => {
        setStatus("error");
        source?.close();
        source = null;
        scheduleReconnect();
      };
    };

    const scheduleReconnect = () => {
      if (closed) return;
      reconnectTimer = setTimeout(() => {
        backoff = Math.min(backoff * 2, BACKOFF_MAX_MS);
        connect();
      }, backoff);
    };

    connect();

    const subscribers = subscribersRef.current;
    return () => {
      closed = true;
      if (reconnectTimer) clearTimeout(reconnectTimer);
      source?.close();
      subscribers.clear();
    };
  }, []);

  const value = useMemo<SSEContextValue>(
    () => ({ status, lastEvent, history, subscribe }),
    [status, lastEvent, history, subscribe],
  );

  return <SSEContext.Provider value={value}>{children}</SSEContext.Provider>;
}

function extractSessionId(event: ApogeeEvent): string | undefined {
  const data = event.data as Record<string, unknown> | null | undefined;
  if (!data || typeof data !== "object") return undefined;

  const pickFromContainer = (container: unknown): string | undefined => {
    if (!container || typeof container !== "object") return undefined;
    const maybe = (container as Record<string, unknown>).session_id;
    return typeof maybe === "string" ? maybe : undefined;
  };

  return (
    pickFromContainer(data.turn) ??
    pickFromContainer(data.span) ??
    pickFromContainer(data.session) ??
    pickFromContainer(data.hitl) ??
    pickFromContainer(data.intervention)
  );
}

function matchesFilter(event: ApogeeEvent, filter?: EventFilter): boolean {
  if (!filter) return true;
  if (filter.types && filter.types.length > 0) {
    if (!filter.types.includes(event.type)) return false;
  }
  if (filter.sessionId) {
    // `initial` events are broadcast once on connect and carry no session
    // id; they are intentionally visible to every consumer regardless of
    // filter so the initial hydration payload still lands.
    if (event.type === "initial") return true;
    const eventSession = extractSessionId(event);
    if (eventSession !== filter.sessionId) return false;
  }
  return true;
}

/**
 * useEventStream — thin selector over the layout-scoped SSE context.
 *
 * Before PR #26 this hook opened its own EventSource. Now it reads from
 * `<SSEProvider>` and filters client-side. The return shape is unchanged:
 *
 *   const { status, lastEvent, history, subscribe } =
 *     useEventStream<ApogeeEvent>({ sessionId: "sess-abc" });
 *
 * Pass no argument to subscribe to the unfiltered firehose (used by
 * `LiveIndicator`, `EventTicker`, and the top-ribbon status chip).
 *
 * The `subscribe` callback mirrors the provider's imperative fan-out but
 * only fires for events that match the filter — convenient for hooks like
 * `InterventionQueue` that want to `mutate()` a SWR query on the matching
 * row without waiting for a re-render.
 */
export function useEventStream<T extends ApogeeEvent = ApogeeEvent>(
  filter?: EventFilter,
): UseEventStreamResult<T> {
  const ctx = useContext(SSEContext);

  // Keep the latest filter in a ref so the returned `subscribe` wrapper is
  // stable across renders yet always honours the freshest filter. This
  // matches the semantics of the old hook's `onEvent` ref trick.
  const filterRef = useRef(filter);
  useEffect(() => {
    filterRef.current = filter;
  }, [filter]);

  // Stringify the filter so callers can pass an inline object literal
  // without retriggering the `useMemo` below on every render.
  const filterKey = useMemo(() => {
    if (!filter) return "";
    const types = filter.types ? [...filter.types].sort().join("|") : "";
    return `${filter.sessionId ?? ""}::${types}`;
  }, [filter]);

  const history = useMemo<readonly T[]>(() => {
    const filtered = filterKey
      ? ctx.history.filter((ev) => matchesFilter(ev, filter))
      : ctx.history;
    return filtered as unknown as readonly T[];
    // `filter` is only read when `filterKey` changes, so we intentionally
    // exclude it from the dep list.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ctx.history, filterKey]);

  const lastEvent = useMemo<T | null>(() => {
    if (!ctx.lastEvent) return null;
    if (!filterKey) return ctx.lastEvent as T;
    return matchesFilter(ctx.lastEvent, filter)
      ? (ctx.lastEvent as T)
      : (history[0] ?? null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ctx.lastEvent, filterKey, history]);

  const subscribe = useCallback(
    (cb: (event: T) => void) => {
      return ctx.subscribe((event) => {
        if (matchesFilter(event, filterRef.current)) {
          cb(event as T);
        }
      });
    },
    [ctx],
  );

  return {
    status: ctx.status,
    lastEvent,
    history: history as T[],
    subscribe,
  };
}
