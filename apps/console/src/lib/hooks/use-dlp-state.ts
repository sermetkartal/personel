/**
 * Hook to fetch and subscribe to the DLP service state.
 * Polls every 60 seconds since DLP state changes are infrequent.
 */

"use client";

import { useQuery } from "@tanstack/react-query";
import { getDLPState, dlpStateKeys } from "@/lib/api/dlp-state";
import type { DLPStateResponse } from "@/lib/api/types";

export function useDLPState(): {
  data: DLPStateResponse | undefined;
  isLoading: boolean;
  isError: boolean;
  refetch: () => void;
} {
  const query = useQuery({
    queryKey: dlpStateKeys.current,
    queryFn: getDLPState,
    // Refresh every 60 seconds — DLP state changes are rare but important
    refetchInterval: 60_000,
    // Keep stale data visible while refetching
    staleTime: 30_000,
  });

  return {
    data: query.data,
    isLoading: query.isLoading,
    isError: query.isError,
    refetch: query.refetch,
  };
}

export function useDLPActive(): boolean {
  const { data } = useDLPState();
  return data?.state === "active";
}
