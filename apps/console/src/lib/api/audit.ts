/**
 * Audit trail API query functions.
 */

import { apiClient } from "./client";
import type {
  AuditChainStatus,
  AuditList,
  AuditRecord,
  PaginationParams,
} from "./types";

export interface ListAuditParams extends PaginationParams {
  action?: string;
  actor_id?: string;
  from?: string;
  to?: string;
}

export const auditKeys = {
  all: ["audit"] as const,
  list: (params: ListAuditParams) => ["audit", "list", params] as const,
  chainStatus: ["audit", "chain-status"] as const,
};

export async function listAuditRecords(
  params: ListAuditParams = {},
): Promise<AuditList> {
  const qs = apiClient.buildQuery(params);
  return apiClient.get<AuditList>(`/v1/audit${qs}`);
}

export async function getAuditChainStatus(): Promise<AuditChainStatus> {
  return apiClient.get<AuditChainStatus>("/v1/audit/chain-status");
}

export async function getAuditRecord(id: number): Promise<AuditRecord> {
  return apiClient.get<AuditRecord>(`/v1/audit/${id}`);
}
