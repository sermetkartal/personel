/**
 * ClickHouse analytics report API query functions.
 */

import { apiClient } from "./client";
import type {
  AppBlocksReport,
  DateRangeParams,
  IdleActiveReport,
  ProductivityReport,
  TopAppsReport,
} from "./types";

export interface ReportParams extends DateRangeParams {
  endpoint_id?: string;
}

export interface TopAppsParams extends ReportParams {
  limit?: number;
}

export const reportKeys = {
  all: ["reports"] as const,
  productivity: (params: ReportParams) =>
    ["reports", "productivity", params] as const,
  topApps: (params: TopAppsParams) => ["reports", "top-apps", params] as const,
  idleActive: (params: ReportParams) =>
    ["reports", "idle-active", params] as const,
  appBlocks: (params: ReportParams) =>
    ["reports", "app-blocks", params] as const,
};

export async function getProductivityReport(
  params: ReportParams,
): Promise<ProductivityReport> {
  const qs = apiClient.buildQuery(params);
  return apiClient.get<ProductivityReport>(`/v1/reports/productivity${qs}`);
}

export async function getTopAppsReport(
  params: TopAppsParams,
): Promise<TopAppsReport> {
  const qs = apiClient.buildQuery(params);
  return apiClient.get<TopAppsReport>(`/v1/reports/top-apps${qs}`);
}

export async function getIdleActiveReport(
  params: ReportParams,
): Promise<IdleActiveReport> {
  const qs = apiClient.buildQuery(params);
  return apiClient.get<IdleActiveReport>(`/v1/reports/idle-active${qs}`);
}

export async function getAppBlocksReport(
  params: ReportParams,
): Promise<AppBlocksReport> {
  const qs = apiClient.buildQuery(params);
  return apiClient.get<AppBlocksReport>(`/v1/reports/app-blocks${qs}`);
}

// ---------------------------------------------------------------------------
// Reports preview — Postgres-backed. Phase 1 MVP stand-in for the ClickHouse
// pipeline. Identical shape on the read side so Phase 2 can swap backends
// without touching the UI.
// ---------------------------------------------------------------------------

export interface ProductivityPreviewRow {
  hour: string;
  active_seconds: number;
  idle_seconds: number;
}

export interface TopAppPreviewRow {
  app_name: string;
  category: "productive" | "neutral" | "distracting";
  focus_seconds: number;
  focus_pct: number;
}

export interface IdleActivePreviewRow {
  date: string;
  active_seconds: number;
  idle_seconds: number;
  active_ratio: number;
  employee_count: number;
}

export interface AppBlockPreviewRow {
  occurred_at: string;
  app_name: string;
  count: number;
}

export interface PreviewEnvelope<T> {
  items: T[];
  from: string;
  to: string;
  notice_code?: string;
  notice_hint?: string;
}

export interface PreviewParams {
  from?: string;
  to?: string;
  limit?: number;
}

export async function getProductivityPreview(
  params: PreviewParams = {},
  opts: { token?: string } = {},
): Promise<PreviewEnvelope<ProductivityPreviewRow>> {
  const qs = apiClient.buildQuery(params);
  return apiClient.get<PreviewEnvelope<ProductivityPreviewRow>>(
    `/v1/reports-preview/productivity${qs}`,
    opts,
  );
}

export async function getTopAppsPreview(
  params: PreviewParams = {},
  opts: { token?: string } = {},
): Promise<PreviewEnvelope<TopAppPreviewRow>> {
  const qs = apiClient.buildQuery(params);
  return apiClient.get<PreviewEnvelope<TopAppPreviewRow>>(
    `/v1/reports-preview/top-apps${qs}`,
    opts,
  );
}

export async function getIdleActivePreview(
  params: PreviewParams = {},
  opts: { token?: string } = {},
): Promise<PreviewEnvelope<IdleActivePreviewRow>> {
  const qs = apiClient.buildQuery(params);
  return apiClient.get<PreviewEnvelope<IdleActivePreviewRow>>(
    `/v1/reports-preview/idle-active${qs}`,
    opts,
  );
}

export async function getAppBlocksPreview(
  params: PreviewParams = {},
  opts: { token?: string } = {},
): Promise<PreviewEnvelope<AppBlockPreviewRow>> {
  const qs = apiClient.buildQuery(params);
  return apiClient.get<PreviewEnvelope<AppBlockPreviewRow>>(
    `/v1/reports-preview/app-blocks${qs}`,
    opts,
  );
}
