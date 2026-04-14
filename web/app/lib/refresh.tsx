"use client";

import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
} from "react";

/**
 * refresh.tsx — global revalidation counter shared across SWR-backed hooks.
 *
 * The top ribbon's RefreshButton calls `bump()`, which increments the
 * counter. SWR hooks that want to participate include the counter in their
 * key so the bump invalidates the cache and refetches immediately.
 *
 * Intentionally lightweight — no SWR imports, so this module can be
 * referenced from server components without pulling SWR into the layout
 * bundle.
 */

interface RefreshContextShape {
  token: number;
  bump: () => void;
}

const RefreshContext = createContext<RefreshContextShape>({
  token: 0,
  bump: () => {},
});

export function RefreshProvider({ children }: { children: React.ReactNode }) {
  const [token, setToken] = useState(0);
  const bump = useCallback(() => setToken((t) => t + 1), []);
  const value = useMemo(() => ({ token, bump }), [token, bump]);
  return <RefreshContext.Provider value={value}>{children}</RefreshContext.Provider>;
}

export function useRefresh(): RefreshContextShape {
  return useContext(RefreshContext);
}
