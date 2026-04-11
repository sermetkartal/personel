/**
 * Legal hold API query functions. DPO-only.
 */

import { apiClient } from "./client";
import type {
  LegalHold,
  LegalHoldCreate,
  LegalHoldList,
  LegalHoldRelease,
  PaginationParams,
} from "./types";

export interface ListLegalHoldsParams extends PaginationParams {
  active_only?: boolean;
}

export const legalHoldKeys = {
  all: ["legal-holds"] as const,
  list: (params: ListLegalHoldsParams) =>
    ["legal-holds", "list", params] as const,
  detail: (id: string) => ["legal-holds", "detail", id] as const,
};

export async function listLegalHolds(
  params: ListLegalHoldsParams = { active_only: true },
): Promise<LegalHoldList> {
  const qs = apiClient.buildQuery(params);
  return apiClient.get<LegalHoldList>(`/v1/legal-holds${qs}`);
}

export async function getLegalHold(id: string): Promise<LegalHold> {
  return apiClient.get<LegalHold>(`/v1/legal-holds/${id}`);
}

export async function placeLegalHold(req: LegalHoldCreate): Promise<LegalHold> {
  return apiClient.post<LegalHold>("/v1/legal-holds", req);
}

export async function releaseLegalHold(
  id: string,
  req: LegalHoldRelease,
): Promise<LegalHold> {
  return apiClient.post<LegalHold>(`/v1/legal-holds/${id}/release`, req);
}
