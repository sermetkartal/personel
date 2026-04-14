/**
 * User management API client.
 *
 * Users themselves live in Keycloak; this layer surfaces the read + role-change
 * slice exposed by the Admin API. Write paths are intentionally narrow — we
 * never edit Keycloak profile fields from the console; that is delegated to the
 * identity provider itself.
 */

import { apiClient } from "./client";
import type {
  PaginatedList,
  PaginationParams,
  Role,
  User,
  UserList,
} from "./types";

export interface ListUsersParams extends PaginationParams {
  search?: string;
  role?: Role;
  department?: string;
  disabled?: boolean;
}

export const userKeys = {
  all: ["users"] as const,
  list: (params: ListUsersParams) => ["users", "list", params] as const,
  detail: (id: string) => ["users", "detail", id] as const,
};

export async function listUsers(
  params: ListUsersParams = {},
  opts: { token?: string } = {},
): Promise<UserList> {
  const qs = apiClient.buildQuery(params);
  return apiClient.get<UserList>(`/v1/users${qs}`, opts);
}

export async function getUser(
  id: string,
  opts: { token?: string } = {},
): Promise<User> {
  return apiClient.get<User>(`/v1/users/${id}`, opts);
}

export interface UpdateUserRolePayload {
  role: Role;
}

export async function updateUserRole(
  id: string,
  payload: UpdateUserRolePayload,
): Promise<User> {
  return apiClient.patch<User>(`/v1/users/${id}`, payload);
}

export async function deactivateUser(id: string): Promise<User> {
  return apiClient.post<User>(`/v1/users/${id}/deactivate`);
}

export async function reactivateUser(id: string): Promise<User> {
  return apiClient.post<User>(`/v1/users/${id}/reactivate`);
}

/**
 * Trigger a Keycloak sync pass. Scaffolded for future HRIS integration —
 * the Admin API may return 501 Not Implemented until Phase 2.5 wires real
 * adapter calls. Callers should treat a 5xx gracefully.
 */
export async function syncFromKeycloak(): Promise<{
  synced: number;
  created: number;
  updated: number;
  deactivated: number;
}> {
  return apiClient.post<{
    synced: number;
    created: number;
    updated: number;
    deactivated: number;
  }>(`/v1/users/sync`);
}

export type UserListResult = PaginatedList<User>;
