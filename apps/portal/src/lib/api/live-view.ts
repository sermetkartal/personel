import { apiClient } from "./client";
import { ApiError } from "./types";
import type { MyLiveViewHistory, ActiveLiveViewResponse } from "./types";

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

/**
 * GET /v1/me/live-view-active
 * Returns the currently active live-view session observing the authenticated
 * employee, or { active: false } if there is none.
 *
 * NOTE: scaffold endpoint — backend TODO. Returns { active: false } on 404
 * so the banner can render the safe default without throwing.
 */
export async function getActiveLiveViewSession(
  accessToken: string
): Promise<ActiveLiveViewResponse> {
  try {
    return await apiClient.get<ActiveLiveViewResponse>(
      "/v1/me/live-view-active",
      accessToken
    );
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) {
      return { active: false, session: null };
    }
    throw err;
  }
}
