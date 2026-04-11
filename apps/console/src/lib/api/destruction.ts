/**
 * Destruction reports API query functions.
 */

import { apiClient } from "./client";
import type {
  DestructionReport,
  DestructionReportGenerate,
  DestructionReportList,
  PaginationParams,
} from "./types";

export const destructionKeys = {
  all: ["destruction-reports"] as const,
  list: (params: PaginationParams) =>
    ["destruction-reports", "list", params] as const,
  detail: (id: string) => ["destruction-reports", "detail", id] as const,
};

export async function listDestructionReports(
  params: PaginationParams = {},
): Promise<DestructionReportList> {
  const qs = apiClient.buildQuery(params);
  return apiClient.get<DestructionReportList>(`/v1/destruction-reports${qs}`);
}

export async function getDestructionReport(
  id: string,
): Promise<DestructionReport> {
  return apiClient.get<DestructionReport>(`/v1/destruction-reports/${id}`);
}

export async function generateDestructionReport(
  req: DestructionReportGenerate,
): Promise<DestructionReport> {
  return apiClient.post<DestructionReport>("/v1/destruction-reports", req);
}

/**
 * Returns the presigned PDF download URL.
 * The API issues a 302 redirect to a short-lived MinIO presigned URL.
 * We capture the Location header rather than following the redirect.
 */
export async function getDestructionReportDownloadUrl(
  id: string,
): Promise<string> {
  const res = await fetch(
    `${process.env.NEXT_PUBLIC_API_BASE_URL ?? "http://localhost:8080"}/v1/destruction-reports/${id}/download`,
    {
      method: "GET",
      redirect: "manual",
      headers: {
        Accept: "application/json",
      },
    },
  );

  if (res.status === 302 || res.type === "opaqueredirect") {
    const location = res.headers.get("Location");
    if (location) return location;
  }

  throw new Error("Failed to get download URL");
}
