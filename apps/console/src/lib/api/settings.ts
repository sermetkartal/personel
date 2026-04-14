/**
 * Settings API query functions — tenants, users.
 */

import { apiClient, type FetchOptions } from "./client";
import type {
  PaginationParams,
  Role,
  RoleChange,
  Tenant,
  TenantCreate,
  TenantList,
  TenantSettings,
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
  settings: (id: string) => ["tenants", "settings", id] as const,
};

export async function listTenants(
  params: PaginationParams = {},
  opts?: FetchOptions,
): Promise<TenantList> {
  const qs = apiClient.buildQuery(params);
  return apiClient.get<TenantList>(`/v1/tenants${qs}`, opts);
}

export async function getTenant(
  id: string,
  opts?: FetchOptions,
): Promise<Tenant> {
  return apiClient.get<Tenant>(`/v1/tenants/${id}`, opts);
}

export async function createTenant(
  req: TenantCreate,
  opts?: FetchOptions,
): Promise<Tenant> {
  return apiClient.post<Tenant>("/v1/tenants", req, opts);
}

export async function updateTenant(
  id: string,
  req: TenantUpdate,
  opts?: FetchOptions,
): Promise<Tenant> {
  return apiClient.patch<Tenant>(`/v1/tenants/${id}`, req, opts);
}

export async function getTenantSettings(
  id: string,
  opts?: FetchOptions,
): Promise<TenantSettings> {
  return apiClient.get<TenantSettings>(`/v1/tenants/${id}/settings`, opts);
}

// ── Screenshot preset ────────────────────────────────────────────────────────
// Per-tenant capture preset controlling agent-side screenshot footprint.
// Backed by /v1/tenants/me/screenshot-preset (migration 0037). Written from
// Settings/General; agent reads it at boot via PERSONEL_SCREENSHOT_PRESET env.

export type ScreenshotPreset = "minimal" | "low" | "medium" | "high" | "max";

export interface ScreenshotPresetResponse {
  preset: ScreenshotPreset;
}

export interface ScreenshotPresetPatchResponse {
  preset: ScreenshotPreset;
  previous: ScreenshotPreset;
  valid_presets: ScreenshotPreset[];
}

export async function getTenantScreenshotPreset(
  opts?: FetchOptions,
): Promise<ScreenshotPresetResponse> {
  return apiClient.get<ScreenshotPresetResponse>(
    "/v1/tenants/me/screenshot-preset",
    opts,
  );
}

export async function updateTenantScreenshotPreset(
  preset: ScreenshotPreset,
  opts?: FetchOptions,
): Promise<ScreenshotPresetPatchResponse> {
  return apiClient.patch<ScreenshotPresetPatchResponse>(
    "/v1/tenants/me/screenshot-preset",
    { preset },
    opts,
  );
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
