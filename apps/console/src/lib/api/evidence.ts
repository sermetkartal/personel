/**
 * Evidence locker API queries for the SOC 2 Type II coverage dashboard.
 *
 * Phase 3.0 — ADR 0023. DPO/Auditor only; the pack endpoint is DPO only.
 * The coverage matrix lists item counts per expected TSC control and
 * identifies zero-coverage controls so the DPO can surface gaps before
 * they become audit findings.
 */

import { apiClient } from "./client";

export const evidenceKeys = {
  all: ["evidence"] as const,
  coverage: (period: string) => ["evidence", "coverage", period] as const,
};

export interface CoverageEntry {
  control: string;
  count: number;
}

export interface CoverageResponse {
  tenant_id: string;
  collection_period: string;
  generated_at: string;
  total_items: number;
  by_control: CoverageEntry[];
  gap_controls: string[];
}

/**
 * Fetch the evidence coverage matrix for the given period (YYYY-MM).
 * Omit period to get the current month.
 */
export async function getEvidenceCoverage(period?: string): Promise<CoverageResponse> {
  const qs = period ? `?period=${encodeURIComponent(period)}` : "";
  return apiClient.get<CoverageResponse>(`/v1/system/evidence-coverage${qs}`);
}

/**
 * Build the URL for the evidence pack ZIP download. The browser follows
 * this link directly so the ZIP stream is piped straight from the API to
 * the user's disk — no buffering in the Next.js process. The download is
 * protected by the same session cookie path as every other API call.
 */
export function buildEvidencePackURL(period: string, controls?: string[]): string {
  const base = process.env.NEXT_PUBLIC_API_BASE_URL ?? "http://localhost:8080";
  const params = new URLSearchParams({ period });
  if (controls && controls.length > 0) {
    params.set("controls", controls.join(","));
  }
  return `${base}/v1/dpo/evidence-packs?${params.toString()}`;
}
