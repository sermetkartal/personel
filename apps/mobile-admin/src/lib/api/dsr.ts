/**
 * DSR (KVKK m.11) queries and mutations.
 * Routes through mobile-bff which proxies to Admin API:
 *   GET  /v1/dsr?state=open,at_risk,overdue  — filtered queue
 *   GET  /v1/dsr/{id}                         — detail
 *   POST /v1/dsr/{id}/respond                  — respond (closes DSR)
 *
 * Mobile scope is intentionally limited to "new + overdue" triage.
 * Assign, extend, reject, and erase remain on the web console.
 */

import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { apiGet, apiPost } from "@/lib/api/client";
import type {
  DSRRequest,
  DSRRespondRequest,
  PaginatedResponse,
} from "@/lib/api/types";

// ── Query keys ────────────────────────────────────────────────────────────────

export const DSR_KEYS = {
  all: ["dsr"] as const,
  queue: () => [...DSR_KEYS.all, "queue"] as const,
  detail: (id: string) => [...DSR_KEYS.all, "detail", id] as const,
};

// ── Query options ─────────────────────────────────────────────────────────────

// Mobile shows only actionable DSRs: open, at_risk, overdue
export const dsrQueueQueryOptions = () =>
  queryOptions({
    queryKey: DSR_KEYS.queue(),
    queryFn: ({ signal }) =>
      // TODO (backend-developer): mobile-bff should expose a
      // /v1/mobile/dsr?states=open,at_risk,overdue endpoint that merges
      // results from the Admin API and returns a sanitised list.
      // For now the mobile-bff proxies the Admin API endpoint directly.
      apiGet<PaginatedResponse<DSRRequest>>(
        "/v1/dsr?state=open&page_size=100",
        signal,
      ),
    staleTime: 30_000,
    refetchInterval: 60_000,
  });

export const dsrDetailQueryOptions = (id: string) =>
  queryOptions({
    queryKey: DSR_KEYS.detail(id),
    queryFn: ({ signal }) =>
      apiGet<DSRRequest>(`/v1/dsr/${id}`, signal),
    staleTime: 15_000,
  });

// ── Mutations ─────────────────────────────────────────────────────────────────

export function useRespondDSR(dsrId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: DSRRespondRequest) =>
      apiPost<DSRRequest>(`/v1/dsr/${dsrId}/respond`, body),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: DSR_KEYS.queue() });
      void qc.invalidateQueries({ queryKey: DSR_KEYS.detail(dsrId) });
    },
  });
}
