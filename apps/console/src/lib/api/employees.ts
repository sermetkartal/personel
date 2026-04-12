/**
 * Employee monitoring overview API.
 *
 * GET /v1/employees/overview — returns today's roll-up for every
 * employee: active/idle minutes, top apps, productivity score,
 * screenshot count, last 7-day totals, assigned endpoint count.
 */

import { apiClient } from "./client";

export interface TopApp {
  name: string;
  minutes: number;
  category: "productive" | "neutral" | "distracting";
}

export interface DailyStats {
  day: string;
  active_minutes: number;
  idle_minutes: number;
  screenshot_count: number;
  keystroke_count: number;
  productivity_score: number;
  top_apps: TopApp[];
  first_activity_at: string;
  last_activity_at: string;
}

export interface EmployeeOverviewRow {
  user_id: string;
  username: string;
  full_name: string;
  email: string;
  department: string;
  job_title: string;
  today: DailyStats;
  last_7_days_active_minutes: number;
  last_7_days_avg_score: number;
  is_currently_active: boolean;
  assigned_endpoint_count: number;
}

export interface EmployeeOverviewResponse {
  items: EmployeeOverviewRow[];
  day: string;
  pagination: { page: number; page_size: number; total: number };
}

export interface EmployeeProfile {
  user_id: string;
  username: string;
  email: string;
  department: string;
  job_title: string;
  role: string;
}

export interface HourlyBucket {
  hour: number;
  active_minutes: number;
  idle_minutes: number;
  top_app: string;
  screenshot_count: number;
}

export interface DailyStatsCompact {
  day: string;
  active_minutes: number;
  idle_minutes: number;
  productivity_score: number;
}

export interface AssignedEndpoint {
  endpoint_id: string;
  hostname: string;
  last_seen_at: string;
  is_online: boolean;
}

export interface EmployeeDetail {
  profile: EmployeeProfile;
  today: DailyStats;
  hourly: HourlyBucket[];
  last_7_days: DailyStatsCompact[];
  assigned_endpoints: AssignedEndpoint[];
  is_currently_active: boolean;
}

export const employeesKeys = {
  all: ["employees"] as const,
  overview: (day?: string) => ["employees", "overview", day ?? "today"] as const,
  detail: (userId: string, day?: string) =>
    ["employees", "detail", userId, day ?? "today"] as const,
};

export async function getEmployeesOverview(
  day?: string,
  opts: { token?: string } = {},
): Promise<EmployeeOverviewResponse> {
  const qs = day ? `?day=${encodeURIComponent(day)}` : "";
  return apiClient.get<EmployeeOverviewResponse>(`/v1/employees/overview${qs}`, opts);
}

export async function getEmployeeDetail(
  userId: string,
  day?: string,
  opts: { token?: string } = {},
): Promise<EmployeeDetail> {
  const qs = day ? `?day=${encodeURIComponent(day)}` : "";
  return apiClient.get<EmployeeDetail>(`/v1/employees/${userId}/detail${qs}`, opts);
}
