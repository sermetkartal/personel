/**
 * Policy management API query functions.
 */

import { apiClient } from "./client";
import type {
  PaginationParams,
  Policy,
  PolicyCreate,
  PolicyList,
  PolicyPushRequest,
  PolicyPushResult,
  PolicyUpdate,
} from "./types";

export const policyKeys = {
  all: ["policies"] as const,
  list: (params: PaginationParams) => ["policies", "list", params] as const,
  detail: (id: string) => ["policies", "detail", id] as const,
};

export async function listPolicies(
  params: PaginationParams = {},
): Promise<PolicyList> {
  const qs = apiClient.buildQuery(params);
  return apiClient.get<PolicyList>(`/v1/policies${qs}`);
}

export async function getPolicy(id: string): Promise<Policy> {
  return apiClient.get<Policy>(`/v1/policies/${id}`);
}

export async function createPolicy(req: PolicyCreate): Promise<Policy> {
  return apiClient.post<Policy>("/v1/policies", req);
}

export async function updatePolicy(
  id: string,
  req: PolicyUpdate,
): Promise<Policy> {
  return apiClient.patch<Policy>(`/v1/policies/${id}`, req);
}

export async function deletePolicy(id: string): Promise<void> {
  return apiClient.delete<void>(`/v1/policies/${id}`);
}

export async function pushPolicy(
  id: string,
  req: PolicyPushRequest,
): Promise<PolicyPushResult> {
  return apiClient.post<PolicyPushResult>(`/v1/policies/${id}/push`, req);
}
