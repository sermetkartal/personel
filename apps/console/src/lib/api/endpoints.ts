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
} from "./types";

export interface ListEndpointsParams extends PaginationParams {
  status?: EndpointStatus;
}

export const endpointKeys = {
  all: ["endpoints"] as const,
  list: (params: ListEndpointsParams) => ["endpoints", "list", params] as const,
  detail: (id: string) => ["endpoints", "detail", id] as const,
};

export async function listEndpoints(
  params: ListEndpointsParams = {},
  opts: { token?: string } = {},
): Promise<EndpointList> {
  const qs = apiClient.buildQuery(params);
  return apiClient.get<EndpointList>(`/v1/endpoints${qs}`, opts);
}

export async function getEndpoint(id: string): Promise<Endpoint> {
  return apiClient.get<Endpoint>(`/v1/endpoints/${id}`);
}

export async function enrollEndpoint(
  req: EnrollRequest,
): Promise<EnrollmentToken> {
  return apiClient.post<EnrollmentToken>("/v1/endpoints/enroll", req);
}

export async function revokeEndpoint(id: string): Promise<void> {
  return apiClient.post<void>(`/v1/endpoints/${id}/revoke`);
}

export async function deleteEndpoint(id: string): Promise<void> {
  return apiClient.delete<void>(`/v1/endpoints/${id}`);
}
