/**
 * Home / summary queries.
 * TODO (backend-developer): Implement GET /v1/mobile/summary in mobile-bff.
 * The BFF aggregates:
 *   - pending live-view requests count (from Admin API GET /v1/live-view/requests?state=REQUESTED)
 *   - pending + at_risk + overdue DSR count (from Admin API GET /v1/dsr?state=open,at_risk,overdue)
 *   - silence gaps in last 24h count (from Admin API GET /v1/silence with date filter)
 *   - last 5 audit entries (from Admin API GET /v1/audit?page_size=5)
 * All proxied through mobile-bff with no PII in response.
 */

import { queryOptions } from "@tanstack/react-query";
import { apiGet } from "@/lib/api/client";
import type { MobileSummary } from "@/lib/api/types";

export const SUMMARY_QUERY_KEY = ["mobile", "summary"] as const;

export const summaryQueryOptions = () =>
  queryOptions({
    queryKey: SUMMARY_QUERY_KEY,
    queryFn: ({ signal }) =>
      apiGet<MobileSummary>("/v1/mobile/summary", signal),
    staleTime: 30_000, // 30 seconds
    refetchInterval: 60_000, // auto-refresh every 60s for on-call use
  });
