import useSWR, { SWRConfiguration } from "swr";
import { apiUrl } from "./api";

/**
 * useApi — thin wrapper around SWR that points at the apogee collector.
 *
 * Ported from aperion's pattern. Pass `null` as the path to disable fetching
 * (useful for conditional queries).
 */

const fetcher = (path: string) =>
  fetch(apiUrl(path)).then((res) => {
    if (!res.ok) throw new Error(`apogee API error: ${res.status}`);
    return res.json();
  });

export function useApi<T>(path: string | null, config?: SWRConfiguration) {
  return useSWR<T>(path, fetcher, {
    revalidateOnFocus: false,
    ...config,
  });
}
