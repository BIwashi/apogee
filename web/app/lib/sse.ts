"use client";

import { useCallback, useEffect, useRef, useState } from "react";

import { apiUrl } from "./api";

/**
 * useEventStream — a minimal SSE React hook backed by the browser's native
 * `EventSource`. It opens a connection on mount, parses each `message` event
 * as JSON, and keeps a capped ring-buffer history plus the latest event for
 * imperative consumers.
 *
 * Reconnects use exponential backoff capped at 10 seconds. The hook does
 * *not* dedupe events — callers that care about ordering should use the
 * `lastEvent` handle or the optional `onEvent` callback.
 */

export type StreamStatus = "connecting" | "open" | "closed" | "error";

export interface UseEventStreamOptions<T> {
  /** Cap on the internal ring buffer. Defaults to 100. */
  historyLimit?: number;
  /** Fired synchronously when a new event arrives. */
  onEvent?: (event: T) => void;
  /** Disable the stream without unmounting the component. */
  enabled?: boolean;
}

export interface UseEventStreamResult<T> {
  status: StreamStatus;
  lastEvent: T | null;
  history: T[];
}

const BACKOFF_START_MS = 500;
const BACKOFF_MAX_MS = 10_000;

export function useEventStream<T = unknown>(
  path: string,
  options: UseEventStreamOptions<T> = {},
): UseEventStreamResult<T> {
  const { historyLimit = 100, onEvent, enabled = true } = options;

  const [status, setStatus] = useState<StreamStatus>(() =>
    enabled ? "connecting" : "closed",
  );
  const [lastEvent, setLastEvent] = useState<T | null>(null);
  const [history, setHistory] = useState<T[]>([]);

  // onEvent in a ref so we don't re-run the effect when the caller passes a
  // fresh closure on every render.
  const onEventRef = useRef(onEvent);
  useEffect(() => {
    onEventRef.current = onEvent;
  }, [onEvent]);

  const handleMessage = useCallback(
    (raw: string) => {
      let parsed: T;
      try {
        parsed = JSON.parse(raw) as T;
      } catch {
        return;
      }
      setLastEvent(parsed);
      setHistory((prev) => {
        const next = [parsed, ...prev];
        if (next.length > historyLimit) next.length = historyLimit;
        return next;
      });
      onEventRef.current?.(parsed);
    },
    [historyLimit],
  );

  useEffect(() => {
    if (!enabled) {
      return;
    }

    let source: EventSource | null = null;
    let backoff = BACKOFF_START_MS;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
    let closed = false;

    const connect = () => {
      try {
        source = new EventSource(apiUrl(path));
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
        handleMessage(ev.data);
      };

      source.onerror = () => {
        setStatus("error");
        // The browser already auto-closes on fatal errors; be defensive.
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

    return () => {
      closed = true;
      if (reconnectTimer) clearTimeout(reconnectTimer);
      source?.close();
    };
  }, [path, enabled, handleMessage]);

  return { status, lastEvent, history };
}
