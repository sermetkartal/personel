import { apiClient } from "./client";
import type { MyDataResponse } from "./types";

/**
 * GET /v1/me
 * Returns a list of data categories collected about the authenticated employee,
 * with KVKK legal basis and retention periods in Turkish.
 */
export async function getMyData(accessToken: string): Promise<MyDataResponse> {
  return apiClient.get<MyDataResponse>("/v1/me", accessToken);
}
