/**
 * KVKK m.11 Data Subject Request API query functions.
 */

import { apiClient } from "./client";
import type {
  DSRCreate,
  DSRExtend,
  DSRList,
  DSRReject,
  DSRRequest,
  DSRRespond,
  DSRAssign,
  DSRState,
  DSRType,
  PaginationParams,
} from "./types";

export interface ListDSRsParams extends PaginationParams {
  state?: DSRState;
  type?: DSRType;
}

export const dsrKeys = {
  all: ["dsr"] as const,
  list: (params: ListDSRsParams) => ["dsr", "list", params] as const,
  detail: (id: string) => ["dsr", "detail", id] as const,
};

export async function listDSRs(
  params: ListDSRsParams = {},
  opts: { token?: string } = {},
): Promise<DSRList> {
  const qs = apiClient.buildQuery(params);
  return apiClient.get<DSRList>(`/v1/dsr${qs}`, opts);
}

export async function getDSR(
  id: string,
  opts: { token?: string } = {},
): Promise<DSRRequest> {
  return apiClient.get<DSRRequest>(`/v1/dsr/${id}`, opts);
}

export async function submitDSR(
  req: DSRCreate,
  opts: { token?: string } = {},
): Promise<DSRRequest> {
  return apiClient.post<DSRRequest>("/v1/dsr", req, opts);
}

export async function assignDSR(
  id: string,
  req: DSRAssign,
  opts: { token?: string } = {},
): Promise<DSRRequest> {
  return apiClient.post<DSRRequest>(`/v1/dsr/${id}/assign`, req, opts);
}

export async function respondDSR(
  id: string,
  req: DSRRespond,
  opts: { token?: string } = {},
): Promise<DSRRequest> {
  return apiClient.post<DSRRequest>(`/v1/dsr/${id}/respond`, req, opts);
}

export async function extendDSR(
  id: string,
  req: DSRExtend,
  opts: { token?: string } = {},
): Promise<DSRRequest> {
  return apiClient.post<DSRRequest>(`/v1/dsr/${id}/extend`, req, opts);
}

export async function rejectDSR(
  id: string,
  req: DSRReject,
  opts: { token?: string } = {},
): Promise<DSRRequest> {
  return apiClient.post<DSRRequest>(`/v1/dsr/${id}/reject`, req, opts);
}

// ── Fulfillment (Phase 2 — real KVKK m.11/b and m.11/f workflows) ────────────

export interface DSRAccessFulfillment {
  artifact_ref: string;
  artifact_url: string;
  artifact_sha256: string;
  artifact_size_bytes: number;
  expires_at: string;
  record_count: number;
}

/**
 * DSR erasure report shape. Either returned fully populated for a real run,
 * or with `dry_run: true` and only the projected counts for a simulation.
 */
export interface DSRErasureReport {
  dry_run: boolean;
  postgres_rows_deleted: number;
  clickhouse_rows_deleted: number;
  minio_keys_erased: number;
  vault_keys_destroyed: number;
  completed_at?: string | null;
  audit_log_id?: string | null;
  blocking_legal_holds?: { id: string; reason_code: string }[];
}

export async function fulfillDSRAccess(
  id: string,
  opts: { token?: string } = {},
): Promise<DSRAccessFulfillment> {
  return apiClient.post<DSRAccessFulfillment>(
    `/v1/dsr/${id}/fulfill-access`,
    undefined,
    opts,
  );
}

export async function fulfillDSRErasure(
  id: string,
  dryRun: boolean,
  opts: { token?: string } = {},
): Promise<DSRErasureReport> {
  return apiClient.post<DSRErasureReport>(
    `/v1/dsr/${id}/fulfill-erasure`,
    { dry_run: dryRun },
    opts,
  );
}

/**
 * Compute days elapsed since a DSR was submitted.
 */
export function dsrDaysElapsed(dsr: DSRRequest): number {
  const submitted = new Date(dsr.created_at);
  const now = new Date();
  return Math.floor(
    (now.getTime() - submitted.getTime()) / (1000 * 60 * 60 * 24),
  );
}

/**
 * Compute days remaining until the SLA deadline.
 * Negative means overdue.
 */
export function dsrDaysRemaining(dsr: DSRRequest): number {
  const deadline = new Date(dsr.sla_deadline);
  const now = new Date();
  return Math.ceil(
    (deadline.getTime() - now.getTime()) / (1000 * 60 * 60 * 24),
  );
}
