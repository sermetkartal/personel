/**
 * Audit trail API query functions.
 */

import { apiClient, ApiError } from "./client";
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
  search: (params: AuditSearchQuery) => ["audit", "search", params] as const,
  chainStatus: ["audit", "chain-status"] as const,
};

export async function listAuditRecords(
  params: ListAuditParams = {},
  opts: { token?: string } = {},
): Promise<AuditList> {
  const qs = apiClient.buildQuery(params);
  return apiClient.get<AuditList>(`/v1/audit${qs}`, opts);
}

export async function getAuditChainStatus(): Promise<AuditChainStatus> {
  return apiClient.get<AuditChainStatus>("/v1/audit/chain-status");
}

export async function getAuditRecord(id: number): Promise<AuditRecord> {
  return apiClient.get<AuditRecord>(`/v1/audit/${id}`);
}

// ── Full-text search (#67) ────────────────────────────────────────────────────

export interface AuditSearchQuery {
  q?: string;
  from?: string;
  to?: string;
  action?: string;
  actor_id?: string;
  page?: number;
  page_size?: number;
}

export interface AuditHit {
  id: string;
  timestamp: string;
  action: string;
  actor_id: string;
  actor_username?: string;
  target: string;
  payload: unknown;
  tenant_id: string;
}

export interface AuditSearchResult {
  hits: AuditHit[];
  total: number;
  took_ms: number;
  /**
   * True when the response was synthesized from the basic /v1/audit fallback
   * because OpenSearch returned 503 or similar. Empty means "search backend
   * is online".
   */
  degraded?: boolean;
}

/**
 * Full-text audit search via OpenSearch. Falls back to the basic list
 * endpoint (and a client-side filter) when the search backend returns
 * 5xx — this lets the UI stay useful while OpenSearch is degraded.
 */
export async function searchAudit(
  query: AuditSearchQuery,
  opts: { token?: string } = {},
): Promise<AuditSearchResult> {
  const qs = apiClient.buildQuery(query);
  try {
    return await apiClient.get<AuditSearchResult>(`/v1/search/audit${qs}`, opts);
  } catch (err) {
    // Search backend offline — fall back to /v1/audit list so the page is
    // still usable. We flag the result as degraded so the UI can render a
    // banner.
    if (err instanceof ApiError && err.status >= 500) {
      const fallback = await listAuditRecords(
        {
          page: query.page ?? 1,
          page_size: query.page_size ?? 25,
          action: query.action,
          actor_id: query.actor_id,
          from: query.from,
          to: query.to,
        },
        opts,
      );
      const q = (query.q ?? "").trim().toLowerCase();
      const filtered = q
        ? fallback.items.filter((r) => {
            const hay = `${r.type} ${r.actor_id ?? ""} ${r.subject_id ?? ""} ${JSON.stringify(
              r.payload_json ?? {},
            )}`.toLowerCase();
            return hay.includes(q);
          })
        : fallback.items;
      return {
        hits: filtered.map((r) => ({
          id: String(r.id),
          timestamp: r.created_at,
          action: r.type,
          actor_id: r.actor_id ?? "",
          target: r.subject_id ?? "",
          payload: r.payload_json,
          tenant_id: r.tenant_id,
        })),
        total: fallback.pagination.total,
        took_ms: 0,
        degraded: true,
      };
    }
    throw err;
  }
}
