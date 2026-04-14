"use client";

import useSWR, { SWRConfiguration } from "swr";
import { apiUrl } from "./api";
import { useRefresh } from "./refresh";

/**
 * useApi — thin wrapper around SWR that points at the apogee collector.
 *
 * Ported from aperion's pattern. Pass `null` as the path to disable fetching
 * (useful for conditional queries). The hook participates in the global
 * refresh context — bumping the refresh token through the TopRibbon's
 * refresh button triggers a revalidation across every useApi consumer.
 */

const fetcher = ([path]: [string, number]) =>
  fetch(apiUrl(path)).then((res) => {
    if (!res.ok) throw new Error(`apogee API error: ${res.status}`);
    return res.json();
  });

export function useApi<T>(path: string | null, config?: SWRConfiguration) {
  const { token } = useRefresh();
  const key = path ? ([path, token] as const) : null;
  return useSWR<T>(key, fetcher, {
    revalidateOnFocus: false,
    ...config,
  });
}
