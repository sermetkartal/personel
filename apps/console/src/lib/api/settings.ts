/**
 * Settings API query functions — tenants, users.
 */

import { apiClient } from "./client";
import type {
  PaginationParams,
  Role,
  RoleChange,
  Tenant,
  TenantCreate,
  TenantList,
  TenantUpdate,
  User,
  UserCreate,
  UserList,
  UserUpdate,
} from "./types";

// ── Tenants ───────────────────────────────────────────────────────────────────

export const tenantKeys = {
  all: ["tenants"] as const,
  list: (params: PaginationParams) => ["tenants", "list", params] as const,
  detail: (id: string) => ["tenants", "detail", id] as const,
};

export async function listTenants(
  params: PaginationParams = {},
): Promise<TenantList> {
  const qs = apiClient.buildQuery(params);
  return apiClient.get<TenantList>(`/v1/tenants${qs}`);
}

export async function getTenant(id: string): Promise<Tenant> {
  return apiClient.get<Tenant>(`/v1/tenants/${id}`);
}

export async function createTenant(req: TenantCreate): Promise<Tenant> {
  return apiClient.post<Tenant>("/v1/tenants", req);
}

export async function updateTenant(
  id: string,
  req: TenantUpdate,
): Promise<Tenant> {
  return apiClient.patch<Tenant>(`/v1/tenants/${id}`, req);
}

// ── Users ─────────────────────────────────────────────────────────────────────

export interface ListUsersParams extends PaginationParams {
  role?: Role;
  disabled?: boolean;
}

export const userKeys = {
  all: ["users"] as const,
  list: (params: ListUsersParams) => ["users", "list", params] as const,
  detail: (id: string) => ["users", "detail", id] as const,
};

export async function listUsers(params: ListUsersParams = {}): Promise<UserList> {
  const qs = apiClient.buildQuery(params);
  return apiClient.get<UserList>(`/v1/users${qs}`);
}

export async function getUser(id: string): Promise<User> {
  return apiClient.get<User>(`/v1/users/${id}`);
}

export async function createUser(req: UserCreate): Promise<User> {
  return apiClient.post<User>("/v1/users", req);
}

export async function updateUser(id: string, req: UserUpdate): Promise<User> {
  return apiClient.patch<User>(`/v1/users/${id}`, req);
}

export async function changeUserRole(
  id: string,
  req: RoleChange,
): Promise<User> {
  return apiClient.patch<User>(`/v1/users/${id}/role`, req);
}

export async function disableUser(id: string): Promise<void> {
  return apiClient.post<void>(`/v1/users/${id}/disable`);
}

export async function deleteUser(id: string): Promise<void> {
  return apiClient.delete<void>(`/v1/users/${id}`);
}
