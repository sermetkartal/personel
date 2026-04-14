import { apiClient } from "./client";
import { ApiError } from "./types";
import type {
  MyDataResponse,
  MyDataSummaryResponse,
  TransparencyAcknowledgeRequest,
  TransparencyAcknowledgeResponse,
} from "./types";

/**
 * GET /v1/me
 * Returns a list of data categories collected about the authenticated employee,
 * with KVKK legal basis and retention periods in Turkish.
 */
export async function getMyData(accessToken: string): Promise<MyDataResponse> {
  return apiClient.get<MyDataResponse>("/v1/me", accessToken);
}

/**
 * GET /v1/me/data-summary
 * Returns per-category event counts for the authenticated employee's own
 * endpoint(s), used by the Verilerim page to show "X kategorisinde son 30
 * günde N olay kaydedildi".
 *
 * NOTE: scaffold endpoint — backend TODO. Returns null on 404 so callers can
 * fall back to the static category cards without breaking.
 */
export async function getMyDataSummary(
  accessToken: string
): Promise<MyDataSummaryResponse | null> {
  try {
    return await apiClient.get<MyDataSummaryResponse>(
      "/v1/me/data-summary",
      accessToken
    );
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) return null;
    throw err;
  }
}

/**
 * POST /v1/transparency/acknowledge
 * Records that the employee has read + accepted the aydınlatma metni.
 * Server-side writes to the hash-chained audit log.
 *
 * NOTE: scaffold endpoint — backend TODO. 404 is treated as accepted so the
 * UI can proceed without blocking the user when the backend has not yet
 * deployed this handler.
 */
export async function acknowledgeAydinlatma(
  accessToken: string,
  version: string,
  locale: "tr" | "en"
): Promise<TransparencyAcknowledgeResponse | null> {
  const body: TransparencyAcknowledgeRequest = { version, locale };
  try {
    return await apiClient.post<TransparencyAcknowledgeResponse>(
      "/v1/transparency/acknowledge",
      body,
      accessToken
    );
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) return null;
    throw err;
  }
}

/**
 * Builds the URL for the aydınlatma metni PDF export.
 * NOTE: scaffold endpoint — returns null + toast "yakında" if the backend
 * does not yet serve this. The actual download is triggered via a browser
 * navigation so the Bearer token must be attached via a download action
 * that the client handles explicitly.
 */
export function aydinlatmaPdfUrl(locale: "tr" | "en", version: string): string {
  return `/v1/transparency/aydinlatma.pdf?locale=${locale}&version=${encodeURIComponent(version)}`;
}
