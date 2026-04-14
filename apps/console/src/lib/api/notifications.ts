/**
 * Notifications API client.
 *
 * The Admin API notification surface is Phase 2 — backends may return 404
 * until `/v1/notifications` lands. Consumers must tolerate 404 as an empty
 * list; see `components/layout/notification-bell.tsx`.
 */

import { apiClient } from "./client";
import type { PaginatedList, PaginationParams, UUID } from "./types";

export type NotificationType =
  | "dsr_new"
  | "dsr_overdue"
  | "live_view_request"
  | "policy_violation"
  | "tamper_alert"
  | "backup_failed";

export interface Notification {
  id: UUID;
  tenant_id: UUID;
  user_id: UUID;
  type: NotificationType;
  title: string;
  body?: string;
  link?: string;
  severity?: "info" | "warning" | "critical";
  read_at?: string | null;
  created_at: string;
}

export type NotificationList = PaginatedList<Notification>;

export interface ListNotificationsParams extends PaginationParams {
  unread?: boolean;
}

export const notificationKeys = {
  all: ["notifications"] as const,
  list: (params: ListNotificationsParams) =>
    ["notifications", "list", params] as const,
};

export async function listNotifications(
  params: ListNotificationsParams = {},
): Promise<NotificationList> {
  const qs = apiClient.buildQuery(params);
  return apiClient.get<NotificationList>(`/v1/notifications${qs}`);
}

export async function markNotificationRead(id: string): Promise<void> {
  return apiClient.post<void>(`/v1/notifications/${id}/read`);
}

export async function markAllNotificationsRead(): Promise<void> {
  return apiClient.post<void>(`/v1/notifications/read-all`);
}
