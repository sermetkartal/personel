/**
 * Endpoint fleet API query functions.
 * Used with TanStack Query.
 */

import { apiClient } from "./client";
import type {
  Endpoint,
  EndpointList,
  EndpointStatus,
  EnrollRequest,
  EnrollmentToken,
  PaginationParams,
  UUID,
} from "./types";

export interface ListEndpointsParams extends PaginationParams {
  status?: EndpointStatus;
  os?: string;
  department?: string;
  hostname?: string;
}

export type EndpointBulkOperation = "deactivate" | "revoke" | "wipe";

export interface EndpointCommand {
  id: UUID;
  endpoint_id: UUID;
  command_type: string;
  status: "queued" | "sent" | "acked" | "failed" | "expired";
  queued_at: string;
  sent_at?: string | null;
  acked_at?: string | null;
  error?: string | null;
  issued_by?: UUID | null;
  reason?: string | null;
}

export interface EndpointCommandList {
  items: EndpointCommand[];
}

export interface EndpointRefreshTokenResponse {
  certificate_pem: string;
  serial_hex: string;
  expires_at: string;
  rate_limit_remaining?: number;
}

export interface EndpointBulkResult {
  operation: EndpointBulkOperation;
  accepted: number;
  rejected: number;
  rejections?: { endpoint_id: UUID; reason: string }[];
}

/**
 * Maximum number of endpoints that can be targeted in a single bulk operation.
 * Mirrors the backend guardrail.
 */
export const BULK_ENDPOINT_LIMIT = 500;

export const endpointKeys = {
  all: ["endpoints"] as const,
  list: (params: ListEndpointsParams) => ["endpoints", "list", params] as const,
  detail: (id: string) => ["endpoints", "detail", id] as const,
  commands: (id: string) => ["endpoints", "commands", id] as const,
};

export async function listEndpoints(
  params: ListEndpointsParams = {},
  opts: { token?: string } = {},
): Promise<EndpointList> {
  const qs = apiClient.buildQuery(params);
  return apiClient.get<EndpointList>(`/v1/endpoints${qs}`, opts);
}

export async function getEndpoint(
  id: string,
  opts: { token?: string } = {},
): Promise<Endpoint> {
  return apiClient.get<Endpoint>(`/v1/endpoints/${id}`, opts);
}

export async function enrollEndpoint(
  req: EnrollRequest,
  opts: { token?: string } = {},
): Promise<EnrollmentToken> {
  return apiClient.post<EnrollmentToken>("/v1/endpoints/enroll", req, opts);
}

export async function revokeEndpoint(
  id: string,
  opts: { token?: string } = {},
): Promise<void> {
  return apiClient.post<void>(`/v1/endpoints/${id}/revoke`, undefined, opts);
}

export async function deleteEndpoint(
  id: string,
  opts: { token?: string } = {},
): Promise<void> {
  return apiClient.delete<void>(`/v1/endpoints/${id}`, opts);
}

// ── Phase 2: lifecycle operations ─────────────────────────────────────────────

export async function deactivateEndpoint(
  id: string,
  reason: string,
  opts: { token?: string } = {},
): Promise<void> {
  return apiClient.post<void>(
    `/v1/endpoints/${id}/deactivate`,
    { reason },
    opts,
  );
}

export async function wipeEndpoint(
  id: string,
  reason: string,
  opts: { token?: string } = {},
): Promise<void> {
  return apiClient.post<void>(`/v1/endpoints/${id}/wipe`, { reason }, opts);
}

export async function refreshEndpointToken(
  id: string,
  csrPEM: string,
  opts: { token?: string } = {},
): Promise<EndpointRefreshTokenResponse> {
  return apiClient.post<EndpointRefreshTokenResponse>(
    `/v1/endpoints/${id}/refresh-token`,
    { csr_pem: csrPEM },
    opts,
  );
}

export async function listEndpointCommands(
  id: string,
  opts: { token?: string } = {},
): Promise<EndpointCommandList> {
  return apiClient.get<EndpointCommandList>(
    `/v1/endpoints/${id}/commands`,
    opts,
  );
}

export async function bulkEndpointOp(
  operation: EndpointBulkOperation,
  endpointIDs: string[],
  reason: string,
  opts: { token?: string } = {},
): Promise<EndpointBulkResult> {
  return apiClient.post<EndpointBulkResult>(
    "/v1/endpoints/bulk",
    { operation, endpoint_ids: endpointIDs, reason },
    opts,
  );
}

/**
 * Client-side heuristic for whether an endpoint is currently active based
 * on `last_seen_at`. Mirrors backend gateway heartbeat classifier.
 */
export function isCurrentlyActive(endpoint: Endpoint): boolean {
  if (endpoint.status !== "active") return false;
  if (!endpoint.last_seen_at) return false;
  const lastSeenMs = new Date(endpoint.last_seen_at).getTime();
  const ageSec = (Date.now() - lastSeenMs) / 1000;
  // Treat anything within 120s as "currently active" (heartbeat cadence 30s).
  return ageSec <= 120;
}
