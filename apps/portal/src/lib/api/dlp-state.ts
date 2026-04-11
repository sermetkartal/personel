import { apiClient } from "./client";
import type { DLPStateResponse, AcknowledgeNotificationResponse, AcknowledgeNotificationRequest } from "./types";

/**
 * GET /v1/system/dlp-state
 * Returns the current DLP state for the tenant.
 *
 * NOTE: This endpoint is referenced in ADR 0013 and mvp-scope.md as the single source
 * of truth consumed by the Console header badge, Settings panel, and Transparency Portal
 * banner. However, it is NOT present in the current openapi.yaml.
 * FEEDBACK: The backend-developer must add GET /v1/system/dlp-state to openapi.yaml.
 * Expected response: { status: "disabled"|"enabled", enabled_at?: string, ceremony_reference?: string }
 */
export async function getDLPState(accessToken: string): Promise<DLPStateResponse> {
  return apiClient.get<DLPStateResponse>("/v1/system/dlp-state", accessToken);
}

/**
 * POST /v1/me/acknowledge-notification
 * Records the employee's acknowledgement of the first-login disclosure.
 * This is a legally audited action per calisan-bilgilendirme-akisi.md Aşama 5.
 *
 * NOTE: This endpoint is NOT present in openapi.yaml.
 * FEEDBACK: The backend-developer must add POST /v1/me/acknowledge-notification
 * with body { notification_type: "first_login_disclosure" }.
 * The server must write this to the hash-chained audit log.
 */
export async function acknowledgeFirstLoginNotification(
  accessToken: string
): Promise<AcknowledgeNotificationResponse> {
  const body: AcknowledgeNotificationRequest = {
    notification_type: "first_login_disclosure",
  };
  return apiClient.post<AcknowledgeNotificationResponse>(
    "/v1/me/acknowledge-notification",
    body,
    accessToken
  );
}
