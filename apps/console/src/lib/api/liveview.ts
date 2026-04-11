/**
 * Live view API query functions.
 * Implements the dual-control enforcement on the client side.
 */

import { apiClient } from "./client";
import type {
  LiveViewApprove,
  LiveViewCreate,
  LiveViewReject,
  LiveViewRequest,
  LiveViewRequestList,
  LiveViewSession,
  LiveViewSessionList,
  LiveViewState,
  LiveViewTerminate,
  PaginationParams,
} from "./types";

export interface ListLiveViewRequestsParams extends PaginationParams {
  state?: LiveViewState;
}

export interface ListLiveViewSessionsParams extends PaginationParams {
  state?: LiveViewState;
  endpoint_id?: string;
}

export const liveViewKeys = {
  all: ["live-view"] as const,
  requests: (params: ListLiveViewRequestsParams) =>
    ["live-view", "requests", params] as const,
  request: (id: string) => ["live-view", "request", id] as const,
  sessions: (params: ListLiveViewSessionsParams) =>
    ["live-view", "sessions", params] as const,
  session: (id: string) => ["live-view", "session", id] as const,
};

export async function listLiveViewRequests(
  params: ListLiveViewRequestsParams = {},
  opts: { token?: string } = {},
): Promise<LiveViewRequestList> {
  const qs = apiClient.buildQuery(params);
  return apiClient.get<LiveViewRequestList>(`/v1/live-view/requests${qs}`, opts);
}

export async function getLiveViewRequest(id: string): Promise<LiveViewRequest> {
  return apiClient.get<LiveViewRequest>(`/v1/live-view/requests/${id}`);
}

export async function requestLiveView(
  req: LiveViewCreate,
): Promise<LiveViewRequest> {
  return apiClient.post<LiveViewRequest>("/v1/live-view/requests", req);
}

export async function approveLiveView(
  requestId: string,
  req: LiveViewApprove,
): Promise<LiveViewSession> {
  return apiClient.post<LiveViewSession>(
    `/v1/live-view/requests/${requestId}/approve`,
    req,
  );
}

export async function rejectLiveView(
  requestId: string,
  req: LiveViewReject,
): Promise<LiveViewRequest> {
  return apiClient.post<LiveViewRequest>(
    `/v1/live-view/requests/${requestId}/reject`,
    req,
  );
}

export async function listLiveViewSessions(
  params: ListLiveViewSessionsParams = {},
): Promise<LiveViewSessionList> {
  const qs = apiClient.buildQuery(params);
  return apiClient.get<LiveViewSessionList>(`/v1/live-view/sessions${qs}`);
}

export async function getLiveViewSession(id: string): Promise<LiveViewSession> {
  return apiClient.get<LiveViewSession>(`/v1/live-view/sessions/${id}`);
}

export async function endLiveViewSession(sessionId: string): Promise<void> {
  return apiClient.post<void>(`/v1/live-view/sessions/${sessionId}/end`);
}

export async function terminateLiveViewSession(
  sessionId: string,
  req: LiveViewTerminate,
): Promise<void> {
  return apiClient.post<void>(
    `/v1/live-view/sessions/${sessionId}/terminate`,
    req,
  );
}

/**
 * Client-side dual-control guard.
 * Returns true if the current user is the requester of the given request.
 * If true, the Approve button must be disabled.
 *
 * NOTE: The API also enforces this server-side. This is an additional UX guard,
 * not a security boundary.
 */
export function isRequester(
  request: LiveViewRequest,
  currentUserId: string,
): boolean {
  return request.requester_id === currentUserId;
}
