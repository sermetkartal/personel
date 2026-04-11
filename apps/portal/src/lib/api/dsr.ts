import { apiClient } from "./client";
import type { DSRList, DSRRequest, MyDSRCreate } from "./types";

const DSR_BASE = "/v1/me/dsr";

/**
 * POST /v1/me/dsr
 * Submit a Data Subject Request on behalf of the authenticated employee.
 * employee_user_id is inferred from the Bearer token server-side.
 */
export async function submitDSR(
  payload: MyDSRCreate,
  accessToken: string
): Promise<DSRRequest> {
  return apiClient.post<DSRRequest>(DSR_BASE, payload, accessToken);
}

/**
 * GET /v1/me/dsr
 * List the authenticated employee's own DSR submissions.
 */
export async function listMyDSRs(
  accessToken: string,
  page = 1,
  pageSize = 20
): Promise<DSRList> {
  const params = new URLSearchParams({
    page: String(page),
    page_size: String(pageSize),
  });
  return apiClient.get<DSRList>(`${DSR_BASE}?${params.toString()}`, accessToken);
}
