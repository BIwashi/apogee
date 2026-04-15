"use client";

import { useCallback, useMemo } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";

/**
 * drawer.tsx — URL-driven state for the cross-cutting SideDrawer.
 *
 * PR #36 introduces a Datadog-style row-click → side drawer pattern across
 * every dashboard table. The drawer's identity lives in the query string so
 * deep links work, the browser back button closes the drawer, and clicking
 * through to a related entity (a parent span, a sibling turn) updates the URL
 * in place without remounting the panel.
 *
 * Query-param contract (all optional; absent `drawer` = no drawer). Every
 * param is namespaced under `drawer_*` so the cross-cutting drawer can live
 * on routes that already use `id` / `sess` / etc. for their own purposes
 * (e.g. `/session?id=…&drawer=agent&drawer_id=…`):
 *
 *   ?drawer=agent&drawer_id=<agent_id>
 *   ?drawer=session&drawer_id=<session_id>
 *   ?drawer=turn&drawer_sess=<session_id>&drawer_turn=<turn_id>
 *   ?drawer=span&drawer_trace_id=<trace_id>&drawer_span_id=<span_id>
 *
 * The hook is a pure reader over `useSearchParams()` + `useRouter().replace()`
 * so there is no local state to lose on navigation.
 */

export type DrawerSpec =
  | { kind: "agent"; id: string }
  | { kind: "session"; id: string }
  | { kind: "turn"; sess: string; turn: string }
  | { kind: "span"; traceID: string; spanID: string }
  | null;

export interface UseDrawerStateResult {
  drawer: DrawerSpec;
  open: (spec: DrawerSpec) => void;
  close: () => void;
}

const DRAWER_PARAMS = [
  "drawer",
  "drawer_id",
  "drawer_sess",
  "drawer_turn",
  "drawer_span_id",
  "drawer_trace_id",
] as const;

function parseDrawer(params: URLSearchParams): DrawerSpec {
  const kind = params.get("drawer");
  if (!kind) return null;
  switch (kind) {
    case "agent": {
      const id = params.get("drawer_id");
      if (!id) return null;
      return { kind: "agent", id };
    }
    case "session": {
      const id = params.get("drawer_id");
      if (!id) return null;
      return { kind: "session", id };
    }
    case "turn": {
      const sess = params.get("drawer_sess");
      const turn = params.get("drawer_turn");
      if (!sess || !turn) return null;
      return { kind: "turn", sess, turn };
    }
    case "span": {
      const traceID = params.get("drawer_trace_id");
      const spanID = params.get("drawer_span_id");
      if (!traceID || !spanID) return null;
      return { kind: "span", traceID, spanID };
    }
    default:
      return null;
  }
}

function applyDrawer(params: URLSearchParams, spec: DrawerSpec): void {
  for (const key of DRAWER_PARAMS) params.delete(key);
  if (!spec) return;
  params.set("drawer", spec.kind);
  switch (spec.kind) {
    case "agent":
      params.set("drawer_id", spec.id);
      break;
    case "session":
      params.set("drawer_id", spec.id);
      break;
    case "turn":
      params.set("drawer_sess", spec.sess);
      params.set("drawer_turn", spec.turn);
      break;
    case "span":
      params.set("drawer_trace_id", spec.traceID);
      params.set("drawer_span_id", spec.spanID);
      break;
  }
}

export function useDrawerState(): UseDrawerStateResult {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();

  const drawer = useMemo(
    () => parseDrawer(new URLSearchParams(searchParams.toString())),
    [searchParams],
  );

  const replaceWith = useCallback(
    (spec: DrawerSpec) => {
      const next = new URLSearchParams(searchParams.toString());
      applyDrawer(next, spec);
      const qs = next.toString();
      router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false });
    },
    [pathname, router, searchParams],
  );

  const open = useCallback(
    (spec: DrawerSpec) => {
      replaceWith(spec);
    },
    [replaceWith],
  );

  const close = useCallback(() => {
    replaceWith(null);
  }, [replaceWith]);

  return { drawer, open, close };
}

/**
 * drawerLinkProps — shared helper for table rows that want to behave like a
 * Datadog row: plain click opens the drawer in place, Cmd/Ctrl/Shift/middle
 * click or right click falls through to the anchor so the operator can still
 * open the full page in a new tab.
 */
export function drawerLinkProps(
  href: string,
  openDrawer: () => void,
): {
  href: string;
  onClick: (event: React.MouseEvent<HTMLElement>) => void;
} {
  return {
    href,
    onClick: (event: React.MouseEvent<HTMLElement>) => {
      if (event.defaultPrevented) return;
      if (event.button !== 0) return;
      if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) {
        return;
      }
      event.preventDefault();
      openDrawer();
    },
  };
}
