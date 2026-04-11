/**
 * Silence (Flow 7 heartbeat gap) queries.
 * Routes through mobile-bff which proxies to Admin API:
 *   GET  /v1/silence                          — list all gaps (admin/manager/dpo)
 *   GET  /v1/silence/{endpointID}/timeline     — timeline for specific endpoint
 *   POST /v1/silence/{endpointID}/acknowledge  — acknowledge with reason
 */

import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { apiGet, apiPost } from "@/lib/api/client";
import type { SilenceGap, SilenceAcknowledgeRequest, PaginatedResponse } from "@/lib/api/types";

// ── Query keys ────────────────────────────────────────────────────────────────

export const SILENCE_KEYS = {
  all: ["silence"] as const,
  list: () => [...SILENCE_KEYS.all, "list"] as const,
  timeline: (endpointId: string) =>
    [...SILENCE_KEYS.all, "timeline", endpointId] as const,
};

// ── Query options ─────────────────────────────────────────────────────────────

export const silenceListQueryOptions = () =>
  queryOptions({
    queryKey: SILENCE_KEYS.list(),
    queryFn: ({ signal }) => {
      // Filter to last 24 hours for on-call relevance
      const to = new Date().toISOString();
      const from = new Date(Date.now() - 24 * 60 * 60 * 1000).toISOString();
      return apiGet<PaginatedResponse<SilenceGap>>(
        `/v1/silence?date_from=${encodeURIComponent(from)}&date_to=${encodeURIComponent(to)}&page_size=50`,
        signal,
      );
    },
    staleTime: 20_000,
    refetchInterval: 60_000,
  });

// ── Mutations ─────────────────────────────────────────────────────────────────

export function useAcknowledgeSilence(endpointId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: SilenceAcknowledgeRequest) =>
      apiPost<void>(`/v1/silence/${endpointId}/acknowledge`, body),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: SILENCE_KEYS.list() });
      void qc.invalidateQueries({
        queryKey: SILENCE_KEYS.timeline(endpointId),
      });
    },
  });
}
