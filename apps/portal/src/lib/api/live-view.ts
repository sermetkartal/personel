import { apiClient } from "./client";
import type { MyLiveViewHistory } from "./types";

/**
 * GET /v1/me/live-view-history
 * Returns live-view sessions where the authenticated employee was the observed subject.
 *
 * Per live-view-protocol.md: only role labels are returned (requester_role, approver_role),
 * never user names or IDs. If the DPO has restricted visibility, returns an empty list
 * with restricted: true.
 *
 * Default visibility: ON (per Phase 0 compliance review revision round).
 */
export async function getMyLiveViewHistory(
  accessToken: string,
  page = 1,
  pageSize = 20
): Promise<MyLiveViewHistory> {
  const params = new URLSearchParams({
    page: String(page),
    page_size: String(pageSize),
  });
  return apiClient.get<MyLiveViewHistory>(
    `/v1/me/live-view-history?${params.toString()}`,
    accessToken
  );
}
