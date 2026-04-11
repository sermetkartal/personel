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

export async function listDSRs(params: ListDSRsParams = {}): Promise<DSRList> {
  const qs = apiClient.buildQuery(params);
  return apiClient.get<DSRList>(`/v1/dsr${qs}`);
}

export async function getDSR(id: string): Promise<DSRRequest> {
  return apiClient.get<DSRRequest>(`/v1/dsr/${id}`);
}

export async function submitDSR(req: DSRCreate): Promise<DSRRequest> {
  return apiClient.post<DSRRequest>("/v1/dsr", req);
}

export async function assignDSR(id: string, req: DSRAssign): Promise<DSRRequest> {
  return apiClient.post<DSRRequest>(`/v1/dsr/${id}/assign`, req);
}

export async function respondDSR(
  id: string,
  req: DSRRespond,
): Promise<DSRRequest> {
  return apiClient.post<DSRRequest>(`/v1/dsr/${id}/respond`, req);
}

export async function extendDSR(
  id: string,
  req: DSRExtend,
): Promise<DSRRequest> {
  return apiClient.post<DSRRequest>(`/v1/dsr/${id}/extend`, req);
}

export async function rejectDSR(
  id: string,
  req: DSRReject,
): Promise<DSRRequest> {
  return apiClient.post<DSRRequest>(`/v1/dsr/${id}/reject`, req);
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
