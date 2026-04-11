/**
 * DLP state API query functions.
 * Per ADR 0013: DLP is OFF by default. This module surfaces the state.
 * The console NEVER provides an "enable" button — only shows state and ceremony
 * instructions.
 */

import { apiClient } from "./client";
import type { DLPStateResponse } from "./types";

export const dlpStateKeys = {
  all: ["dlp-state"] as const,
  current: ["dlp-state", "current"] as const,
};

/**
 * Fetch the current DLP service state from the admin API.
 * Endpoint: GET /api/v1/system/dlp-state
 */
export async function getDLPState(): Promise<DLPStateResponse> {
  return apiClient.get<DLPStateResponse>("/v1/system/dlp-state");
}
