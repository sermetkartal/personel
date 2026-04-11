/**
 * Live view hooks — thin wrappers over TanStack Query options
 * defined in src/lib/api/live-view.ts.
 */

import { useQuery } from "@tanstack/react-query";
import {
  pendingLiveViewQueryOptions,
  liveViewDetailQueryOptions,
  useApproveLiveView,
  useRejectLiveView,
} from "@/lib/api/live-view";

export function usePendingLiveViewRequests() {
  return useQuery(pendingLiveViewQueryOptions());
}

export function useLiveViewDetail(id: string) {
  return useQuery(liveViewDetailQueryOptions(id));
}

// Re-export mutations for convenience
export { useApproveLiveView, useRejectLiveView };
