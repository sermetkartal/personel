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
