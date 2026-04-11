/**
 * Live View approval queries and mutations.
 * Routes through mobile-bff which proxies to Admin API:
 *   GET  /v1/live-view/requests?state=REQUESTED  — pending approvals
 *   GET  /v1/live-view/requests/{id}              — detail
 *   POST /v1/live-view/requests/{id}/approve      — approve (hr role, approver ≠ requester)
 *   POST /v1/live-view/requests/{id}/reject        — reject
 *
 * Dual-control invariant: the server enforces approver ≠ requester.
 * The UI additionally disables the Approve button when caller === requester.
 * See approval-card.tsx for the UI guard implementation.
 */

import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { apiGet, apiPost } from "@/lib/api/client";
import type {
  LiveViewRequest,
  LiveViewApproveRequest,
  LiveViewRejectRequest,
  PaginatedResponse,
} from "@/lib/api/types";

// ── Query keys ────────────────────────────────────────────────────────────────

export const LIVE_VIEW_KEYS = {
  all: ["live-view"] as const,
  lists: () => [...LIVE_VIEW_KEYS.all, "list"] as const,
  pending: () => [...LIVE_VIEW_KEYS.lists(), "pending"] as const,
  detail: (id: string) => [...LIVE_VIEW_KEYS.all, "detail", id] as const,
};

// ── Query options ─────────────────────────────────────────────────────────────

export const pendingLiveViewQueryOptions = () =>
  queryOptions({
    queryKey: LIVE_VIEW_KEYS.pending(),
    queryFn: ({ signal }) =>
      apiGet<PaginatedResponse<LiveViewRequest>>(
        "/v1/live-view/requests?state=REQUESTED&page_size=50",
        signal,
      ),
    staleTime: 15_000,
    refetchInterval: 30_000,
  });

export const liveViewDetailQueryOptions = (id: string) =>
  queryOptions({
    queryKey: LIVE_VIEW_KEYS.detail(id),
    queryFn: ({ signal }) =>
      apiGet<LiveViewRequest>(`/v1/live-view/requests/${id}`, signal),
    staleTime: 10_000,
  });

// ── Mutations ─────────────────────────────────────────────────────────────────

export function useApproveLiveView(requestId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: LiveViewApproveRequest) =>
      apiPost<LiveViewRequest>(
        `/v1/live-view/requests/${requestId}/approve`,
        body,
      ),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: LIVE_VIEW_KEYS.pending() });
      void qc.invalidateQueries({
        queryKey: LIVE_VIEW_KEYS.detail(requestId),
      });
    },
  });
}

export function useRejectLiveView(requestId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: LiveViewRejectRequest) =>
      apiPost<LiveViewRequest>(
        `/v1/live-view/requests/${requestId}/reject`,
        body,
      ),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: LIVE_VIEW_KEYS.pending() });
      void qc.invalidateQueries({
        queryKey: LIVE_VIEW_KEYS.detail(requestId),
      });
    },
  });
}
