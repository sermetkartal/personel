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

export const employeesKeys = {
  all: ["employees"] as const,
  overview: (day?: string) => ["employees", "overview", day ?? "today"] as const,
};

export async function getEmployeesOverview(
  day?: string,
  opts: { token?: string } = {},
): Promise<EmployeeOverviewResponse> {
  const qs = day ? `?day=${encodeURIComponent(day)}` : "";
  return apiClient.get<EmployeeOverviewResponse>(`/v1/employees/overview${qs}`, opts);
}
