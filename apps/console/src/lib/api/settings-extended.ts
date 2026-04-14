/**
 * Extended settings API — Wave 9 Sprint 3B.
 *
 * Covers five alt-domains of /v1/settings/* exposed by the admin API:
 *
 * 1. External integrations (MaxMind, Cloudflare, PagerDuty, Slack, Sentry)
 *    — configuration blobs encrypted at rest via Vault transit; GET responses
 *    are masked server-side so the secret material never leaves Vault.
 *
 * 2. Certificate authority mode (letsencrypt / internal / commercial) —
 *    operator-level decision that drives the 18-service TLS rotation.
 *
 * 3. Retention policy — KVKK floors are enforced both client-side (hints
 *    and validation) and server-side (authoritative). Floors are re-stated
 *    here as `DEFAULT_KVKK_RETENTION` for fast reset.
 *
 * 4. Backup targets — in-site and 6 off-site storage backends, each with a
 *    backend-specific config shape that travels as an opaque record.
 *
 * 5. Backup runs — historical executions against a given target, returned
 *    newest-first for display.
 *
 * See apps/api/internal/settings/* for the backend implementation and the
 * Wave 9 Sprint 3A commit 0e46ad5 for handler/service wiring.
 */

import { apiClient, ApiError, NetworkError } from "./client";

const API_BASE =
  process.env.NEXT_PUBLIC_API_BASE_URL ?? "http://localhost:8080";

/**
 * Direct-fetch PUT helper.
 *
 * The shared apiClient exposes convenience helpers for GET/POST/PATCH/DELETE
 * but not PUT — the Sprint 3A backend uses PUT for integration upserts, so
 * we hand-roll a tiny helper here that mirrors the core client's error
 * handling contract (RFC 7807 → ApiError, network fail → NetworkError).
 */
async function putJson<T>(
  path: string,
  body: unknown,
  opts: { token?: string; signal?: AbortSignal } = {},
): Promise<T> {
  const headers: Record<string, string> = {
    Accept: "application/json",
    "Content-Type": "application/json",
  };
  if (opts.token) {
    headers["Authorization"] = `Bearer ${opts.token}`;
  }

  let response: Response;
  try {
    response = await fetch(`${API_BASE}${path}`, {
      method: "PUT",
      headers,
      body: JSON.stringify(body),
      signal: opts.signal,
    });
  } catch (err) {
    throw new NetworkError(err);
  }

  if (response.status === 204) return undefined as T;

  const contentType = response.headers.get("Content-Type") ?? "";
  if (!response.ok) {
    if (contentType.includes("application/problem+json")) {
      throw new ApiError(response.status, await response.json());
    }
    throw new ApiError(response.status, {
      type: "about:blank",
      title: `HTTP ${response.status}`,
      status: response.status,
      detail: await response.text().catch(() => undefined),
    });
  }

  if (contentType.includes("application/json")) {
    return (await response.json()) as T;
  }
  return undefined as T;
}

// ────────────────────────────────────────────────────────────────────────────
// External integrations
// ────────────────────────────────────────────────────────────────────────────

/** Enumerated external services the console can configure. */
export const SERVICE_NAMES = [
  "maxmind",
  "cloudflare",
  "pagerduty",
  "slack",
  "sentry",
] as const;

export type ServiceName = (typeof SERVICE_NAMES)[number];

/**
 * Single integration record returned by the API.
 *
 * `config` is a free-form object because each service has a different
 * shape:
 *   - maxmind: { account_id, license_key }
 *   - cloudflare: { api_token, zone_id }
 *   - pagerduty: { integration_key, service_id }
 *   - slack: { webhook_url, channel }
 *   - sentry: { dsn, environment }
 *
 * Sensitive fields are returned masked (e.g. `"sk_live_••••1234"`) and
 * must not be displayed without indicating that they are masked.
 */
export interface IntegrationRecord {
  service: ServiceName;
  enabled: boolean;
  config: Record<string, string>;
  updated_at: string; // RFC3339
  updated_by: string;
}

export interface IntegrationList {
  items: IntegrationRecord[];
}

export interface UpsertIntegrationRequest {
  enabled: boolean;
  config: Record<string, string>;
}

/**
 * Human-readable service metadata shared between the card list and the
 * form fields. Field types with `password: true` are masked on display
 * and accept re-entry via a password input.
 */
export interface ServiceSchema {
  label: string;
  description: string;
  fields: Array<{
    key: string;
    label: string;
    placeholder?: string;
    password?: boolean;
    multiline?: boolean;
  }>;
}

export const SERVICE_SCHEMAS: Record<ServiceName, ServiceSchema> = {
  maxmind: {
    label: "MaxMind GeoLite2",
    description:
      "IP-geolocation lookups for the network collector. License key is decrypted at agent boot time and loaded into the GeoIP reader.",
    fields: [
      { key: "account_id", label: "Account ID", placeholder: "891169" },
      { key: "license_key", label: "License Key", password: true },
    ],
  },
  cloudflare: {
    label: "Cloudflare",
    description: "WAF rules and DNS automation for the admin ingress.",
    fields: [
      { key: "api_token", label: "API Token", password: true },
      { key: "zone_id", label: "Zone ID" },
    ],
  },
  pagerduty: {
    label: "PagerDuty",
    description: "Alert routing for SOC 2 evidence-gap and KVKK SLA breaches.",
    fields: [
      { key: "integration_key", label: "Integration Key", password: true },
      { key: "service_id", label: "Service ID" },
    ],
  },
  slack: {
    label: "Slack",
    description: "Notification channel for DSR, incident and policy events.",
    fields: [
      { key: "webhook_url", label: "Webhook URL", password: true },
      { key: "channel", label: "Channel", placeholder: "#personel-alerts" },
    ],
  },
  sentry: {
    label: "Sentry",
    description: "Error tracking for the admin API and console.",
    fields: [
      { key: "dsn", label: "DSN", password: true },
      { key: "environment", label: "Environment", placeholder: "production" },
    ],
  },
};

// ────────────────────────────────────────────────────────────────────────────
// CA mode
// ────────────────────────────────────────────────────────────────────────────

export const CA_MODES = ["letsencrypt", "internal", "commercial"] as const;
export type CaMode = (typeof CA_MODES)[number];

/** Free-form CA config blob; shape depends on `mode`. */
export type CaConfig = Record<string, string>;

export interface CaModeInfo {
  mode: CaMode;
  config: CaConfig;
  updated_at: string;
  updated_by: string;
}

export interface UpdateCaModeRequest {
  mode: CaMode;
  config: CaConfig;
}

// ────────────────────────────────────────────────────────────────────────────
// Retention policy
// ────────────────────────────────────────────────────────────────────────────

/**
 * Retention policy as stored by the backend. Numbers are wall-clock
 * durations in the unit named by the field suffix.
 */
export interface RetentionPolicy {
  audit_years: number;
  event_days: number;
  screenshot_days: number;
  keystroke_days: number;
  live_view_days: number;
  dsr_days: number;
  updated_at?: string;
  updated_by?: string;
}

/**
 * KVKK minimum floors — enforced by the backend but repeated here for
 * the "reset to minimum" button and for inline validation hints.
 *
 *   - Audit log: 5 years (KVKK m.26 + SOC 2 retention)
 *   - Event data: 1 year (pilot policy)
 *   - Screenshot: 30 days (ADR 0013 footprint guard)
 *   - Keystroke encrypted: 180 days (DLP investigation window)
 *   - Live view session: 30 days (dual-control audit trail)
 *   - DSR artifact: 10 years (statutory archive)
 */
export const DEFAULT_KVKK_RETENTION: RetentionPolicy = {
  audit_years: 5,
  event_days: 365,
  screenshot_days: 30,
  keystroke_days: 180,
  live_view_days: 30,
  dsr_days: 3650,
};

export type UpdateRetentionRequest = Pick<
  RetentionPolicy,
  | "audit_years"
  | "event_days"
  | "screenshot_days"
  | "keystroke_days"
  | "live_view_days"
  | "dsr_days"
>;

// ────────────────────────────────────────────────────────────────────────────
// Backup targets + runs
// ────────────────────────────────────────────────────────────────────────────

export const BACKUP_KINDS = [
  "in_site_local",
  "offsite_s3",
  "offsite_azure",
  "offsite_gcs",
  "offsite_sftp",
  "offsite_nfs",
  "offsite_minio_peer",
] as const;

export type BackupKind = (typeof BACKUP_KINDS)[number];

export interface BackupTarget {
  id: string;
  name: string;
  kind: BackupKind;
  enabled: boolean;
  config: Record<string, string>;
  retention_days: number;
  last_run_at: string | null;
  last_run_status: "success" | "failure" | "running" | null;
  created_at: string;
  updated_at: string;
}

export interface BackupTargetList {
  items: BackupTarget[];
}

export interface CreateTargetRequest {
  name: string;
  kind: BackupKind;
  enabled: boolean;
  config: Record<string, string>;
  retention_days: number;
}

export interface UpdateTargetRequest {
  name?: string;
  enabled?: boolean;
  config?: Record<string, string>;
  retention_days?: number;
}

export interface TriggerRunRequest {
  kind: BackupKind;
}

export interface BackupRun {
  id: string;
  target_id: string;
  started_at: string;
  finished_at: string | null;
  status: "success" | "failure" | "running";
  size_bytes: number;
  sha256: string | null;
  error: string | null;
}

export interface BackupRunList {
  items: BackupRun[];
}

/**
 * Config schema for each backup backend kind. Used by the "Add storage"
 * modal to render dynamic field sets.
 */
export const BACKUP_SCHEMAS: Record<
  BackupKind,
  Array<{
    key: string;
    label: string;
    placeholder?: string;
    password?: boolean;
    multiline?: boolean;
  }>
> = {
  in_site_local: [
    { key: "path", label: "Path", placeholder: "/var/backups/personel" },
  ],
  offsite_s3: [
    { key: "endpoint", label: "Endpoint", placeholder: "https://s3.amazonaws.com" },
    { key: "region", label: "Region", placeholder: "eu-central-1" },
    { key: "bucket", label: "Bucket", placeholder: "personel-backups" },
    { key: "access_key", label: "Access Key" },
    { key: "secret_access_key", label: "Secret Access Key", password: true },
  ],
  offsite_azure: [
    { key: "account_name", label: "Account Name" },
    { key: "account_key", label: "Account Key", password: true },
    { key: "container", label: "Container", placeholder: "personel" },
  ],
  offsite_gcs: [
    { key: "project_id", label: "Project ID" },
    { key: "bucket", label: "Bucket" },
    {
      key: "service_account_json",
      label: "Service Account JSON",
      multiline: true,
    },
  ],
  offsite_sftp: [
    { key: "host", label: "Host", placeholder: "backup.example.com" },
    { key: "port", label: "Port", placeholder: "22" },
    { key: "user", label: "Username" },
    { key: "password", label: "Password / Key", password: true },
    { key: "remote_path", label: "Remote Path", placeholder: "/backups" },
  ],
  offsite_nfs: [
    { key: "mount_path", label: "Mount Path", placeholder: "/mnt/nfs/personel" },
  ],
  offsite_minio_peer: [
    { key: "endpoint", label: "Endpoint", placeholder: "https://minio-peer:9000" },
    { key: "access_key", label: "Access Key" },
    { key: "secret_key", label: "Secret Key", password: true },
    { key: "bucket", label: "Bucket", placeholder: "personel-peer" },
  ],
};

// ────────────────────────────────────────────────────────────────────────────
// TanStack Query keys
// ────────────────────────────────────────────────────────────────────────────

export const settingsKeys = {
  all: ["settings"] as const,
  integrations: ["settings", "integrations"] as const,
  integration: (service: ServiceName) =>
    ["settings", "integrations", service] as const,
  caMode: ["settings", "ca-mode"] as const,
  retention: ["settings", "retention"] as const,
  backupTargets: ["settings", "backup", "targets"] as const,
  backupTarget: (id: string) => ["settings", "backup", "targets", id] as const,
  backupRuns: (id: string) =>
    ["settings", "backup", "targets", id, "runs"] as const,
} as const;

// ────────────────────────────────────────────────────────────────────────────
// Integrations — fetchers
// ────────────────────────────────────────────────────────────────────────────

export async function listIntegrations(
  opts: { token?: string; signal?: AbortSignal } = {},
): Promise<IntegrationList> {
  return apiClient.get<IntegrationList>("/v1/settings/integrations", opts);
}

export async function getIntegration(
  service: ServiceName,
  opts: { token?: string; signal?: AbortSignal } = {},
): Promise<IntegrationRecord> {
  return apiClient.get<IntegrationRecord>(
    `/v1/settings/integrations/${encodeURIComponent(service)}`,
    opts,
  );
}

export async function upsertIntegration(
  service: ServiceName,
  req: UpsertIntegrationRequest,
  opts: { token?: string; signal?: AbortSignal } = {},
): Promise<void> {
  return putJson<void>(
    `/v1/settings/integrations/${encodeURIComponent(service)}`,
    req,
    opts,
  );
}

export async function deleteIntegration(
  service: ServiceName,
  opts: { token?: string; signal?: AbortSignal } = {},
): Promise<void> {
  return apiClient.delete<void>(
    `/v1/settings/integrations/${encodeURIComponent(service)}`,
    opts,
  );
}

// ────────────────────────────────────────────────────────────────────────────
// CA mode — fetchers
// ────────────────────────────────────────────────────────────────────────────

export async function getCaMode(
  opts: { token?: string; signal?: AbortSignal } = {},
): Promise<CaModeInfo> {
  return apiClient.get<CaModeInfo>("/v1/settings/ca-mode", opts);
}

export async function updateCaMode(
  req: UpdateCaModeRequest,
  opts: { token?: string; signal?: AbortSignal } = {},
): Promise<void> {
  return apiClient.patch<void>("/v1/settings/ca-mode", req, opts);
}

// ────────────────────────────────────────────────────────────────────────────
// Retention — fetchers
// ────────────────────────────────────────────────────────────────────────────

export async function getRetention(
  opts: { token?: string; signal?: AbortSignal } = {},
): Promise<RetentionPolicy> {
  return apiClient.get<RetentionPolicy>("/v1/settings/retention", opts);
}

export async function updateRetention(
  req: UpdateRetentionRequest,
  opts: { token?: string; signal?: AbortSignal } = {},
): Promise<void> {
  return apiClient.patch<void>("/v1/settings/retention", req, opts);
}

// ────────────────────────────────────────────────────────────────────────────
// Backup — fetchers
// ────────────────────────────────────────────────────────────────────────────

export async function listBackupTargets(
  opts: { token?: string; signal?: AbortSignal } = {},
): Promise<BackupTargetList> {
  return apiClient.get<BackupTargetList>("/v1/settings/backup/targets", opts);
}

export async function createBackupTarget(
  req: CreateTargetRequest,
  opts: { token?: string; signal?: AbortSignal } = {},
): Promise<BackupTarget> {
  return apiClient.post<BackupTarget>("/v1/settings/backup/targets", req, opts);
}

export async function updateBackupTarget(
  id: string,
  req: UpdateTargetRequest,
  opts: { token?: string; signal?: AbortSignal } = {},
): Promise<void> {
  return apiClient.patch<void>(
    `/v1/settings/backup/targets/${encodeURIComponent(id)}`,
    req,
    opts,
  );
}

export async function deleteBackupTarget(
  id: string,
  opts: { token?: string; signal?: AbortSignal } = {},
): Promise<void> {
  return apiClient.delete<void>(
    `/v1/settings/backup/targets/${encodeURIComponent(id)}`,
    opts,
  );
}

export async function triggerBackupRun(
  id: string,
  req: TriggerRunRequest,
  opts: { token?: string; signal?: AbortSignal } = {},
): Promise<BackupRun> {
  return apiClient.post<BackupRun>(
    `/v1/settings/backup/targets/${encodeURIComponent(id)}/run`,
    req,
    opts,
  );
}

export async function listBackupRuns(
  id: string,
  opts: { token?: string; signal?: AbortSignal } = {},
): Promise<BackupRunList> {
  return apiClient.get<BackupRunList>(
    `/v1/settings/backup/targets/${encodeURIComponent(id)}/runs`,
    opts,
  );
}
