/**
 * Screenshots API query functions.
 * Access is restricted to investigator and dpo roles.
 * Every access is audited before the URL is issued.
 */

import { apiClient } from "./client";
import type {
  DateRangeParams,
  PaginationParams,
  PresignedURL,
  Screenshot,
  ScreenshotList,
} from "./types";

export interface ListScreenshotsParams extends PaginationParams, DateRangeParams {
  endpoint_id?: string;
}

export const screenshotKeys = {
  all: ["screenshots"] as const,
  list: (params: ListScreenshotsParams) =>
    ["screenshots", "list", params] as const,
  url: (id: string, reasonCode: string) =>
    ["screenshots", "url", id, reasonCode] as const,
};

export async function listScreenshots(
  params: ListScreenshotsParams,
): Promise<ScreenshotList> {
  const qs = apiClient.buildQuery(params);
  return apiClient.get<ScreenshotList>(`/v1/screenshots${qs}`);
}

/**
 * Request a presigned URL for screenshot access.
 * The audit record is written BEFORE the URL is returned.
 * reason_code is mandatory per the API contract.
 */
export async function getScreenshotURL(
  screenshotId: string,
  reasonCode: string,
): Promise<PresignedURL> {
  const qs = apiClient.buildQuery({ reason_code: reasonCode });
  return apiClient.get<PresignedURL>(`/v1/screenshots/${screenshotId}/url${qs}`);
}

export async function getScreenshot(id: string): Promise<Screenshot> {
  return apiClient.get<Screenshot>(`/v1/screenshots/${id}`);
}
