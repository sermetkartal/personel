/**
 * TypeScript types derived from the Personel Admin API OpenAPI 3.1 contract.
 * Hand-written for Phase 1. Keep in sync with apps/api/api/openapi.yaml.
 */

// ── Primitives ────────────────────────────────────────────────────────────────

export type UUID = string;
export type ISODateString = string; // RFC 3339

export type Role =
  | "admin"
  | "manager"
  | "hr"
  | "dpo"
  | "investigator"
  | "auditor"
  | "employee"
  | "it_operator"
  | "it_manager";

export type EndpointStatus = "active" | "revoked" | "offline";

export type DSRState =
  | "open"
  | "at_risk"
  | "overdue"
  | "resolved"
  | "rejected";

export type DSRType =
  | "access"
  | "rectify"
  | "erase"
  | "object"
  | "restrict"
  | "portability";

export type LiveViewState =
  | "REQUESTED"
  | "APPROVED"
  | "ACTIVE"
  | "ENDED"
  | "DENIED"
  | "EXPIRED"
  | "FAILED"
  | "TERMINATED_BY_HR"
  | "TERMINATED_BY_DPO";

export type DLPState = "disabled" | "active";

// ── RFC 7807 Problem Detail ───────────────────────────────────────────────────

export interface FieldError {
  field: string;
  code: string;
  message?: string;
}

export interface ProblemDetail {
  type: string;
  title: string;
  status: number;
  detail?: string;
  instance?: string;
  trace_id?: string;
  request_id?: string;
  code?: string;
  errors?: FieldError[];
}

// ── Pagination ────────────────────────────────────────────────────────────────

export interface Pagination {
  page: number;
  page_size: number;
  total: number;
}

export interface PaginatedList<T> {
  items: T[];
  pagination: Pagination;
}

// ── Tenant ────────────────────────────────────────────────────────────────────

export interface TenantSettings {
  live_view_history_restricted?: boolean;
  max_screenshot_retention_days?: number;
}

export interface Tenant {
  id: UUID;
  slug: string;
  display_name: string;
  settings?: TenantSettings;
  created_at: ISODateString;
  updated_at?: ISODateString | null;
}

export interface TenantCreate {
  slug: string;
  display_name: string;
  settings?: TenantSettings;
}

export interface TenantUpdate {
  display_name?: string;
  settings?: TenantSettings;
}

export type TenantList = PaginatedList<Tenant>;

// ── User ──────────────────────────────────────────────────────────────────────

export interface User {
  id: UUID;
  tenant_id: UUID;
  username: string;
  email: string;
  role: Role;
  disabled: boolean;
  created_at: ISODateString;
  updated_at?: ISODateString | null;
}

export interface UserCreate {
  username: string;
  email: string;
  role: Role;
}

export interface UserUpdate {
  username?: string;
  email?: string;
}

export interface RoleChange {
  role: Role;
}

export type UserList = PaginatedList<User>;

// ── Endpoint ──────────────────────────────────────────────────────────────────

export interface Endpoint {
  id: UUID;
  tenant_id: UUID;
  hostname: string;
  os_version?: string | null;
  agent_version?: string | null;
  status: EndpointStatus;
  last_seen_at?: ISODateString | null;
  enrolled_at: ISODateString;
  policy_id?: UUID | null;
}

export interface EnrollRequest {
  hostname: string;
  os_version?: string;
  policy_id?: UUID;
}

export interface EnrollmentToken {
  endpoint_id: UUID;
  secret_id: string;
  role_id: string;
  vault_addr: string;
  expires_at: ISODateString;
}

export type EndpointList = PaginatedList<Endpoint>;

// ── Policy ────────────────────────────────────────────────────────────────────

export interface SensitivityGuard {
  window_title_sensitive_regex?: string[];
  sensitive_host_globs?: string[];
  screenshot_exclude_apps?: string[];
  auto_flag_on_m6_dlp_match?: boolean;
}

export interface PolicyRules {
  screenshot_enabled?: boolean;
  screenshot_interval_seconds?: number;
  app_block_list?: string[];
  app_allow_list?: string[];
  sensitivity_guard?: SensitivityGuard;
}

export interface Policy {
  id: UUID;
  tenant_id: UUID;
  name: string;
  description?: string | null;
  version: number;
  rules: PolicyRules;
  created_at: ISODateString;
  updated_at?: ISODateString | null;
}

export interface PolicyCreate {
  name: string;
  description?: string;
  rules: PolicyRules;
}

export interface PolicyUpdate {
  name?: string;
  description?: string;
  rules?: PolicyRules;
}

export interface PolicyPushRequest {
  endpoint_ids: UUID[];
}

export interface PolicyPushResult {
  queued: number;
  endpoint_ids: UUID[];
}

export type PolicyList = PaginatedList<Policy>;

// ── DSR ───────────────────────────────────────────────────────────────────────

export interface DSRRequest {
  id: UUID;
  tenant_id: UUID;
  employee_user_id: UUID;
  request_type: DSRType;
  scope_json?: Record<string, unknown>;
  state: DSRState;
  created_at: ISODateString;
  sla_deadline: ISODateString;
  assigned_to?: UUID | null;
  response_artifact_ref?: string | null;
  audit_chain_ref?: string | null;
}

export interface DSRCreate {
  request_type: DSRType;
  scope_json?: Record<string, unknown>;
  justification?: string;
}

export interface DSRAssign {
  user_id: UUID;
}

export interface DSRRespond {
  artifact_ref: string;
  notes?: string;
}

export interface DSRExtend {
  justification: string;
}

export interface DSRReject {
  reason: string;
}

export type DSRList = PaginatedList<DSRRequest>;

// ── Legal Hold ────────────────────────────────────────────────────────────────

export interface LegalHoldScope {
  endpoint_id?: UUID;
  user_id?: UUID;
  date_from?: ISODateString;
  date_to?: ISODateString;
  event_types?: string[];
}

export interface LegalHold {
  id: UUID;
  tenant_id: UUID;
  reason_code: string;
  ticket_id: string;
  justification: string;
  scope: LegalHoldScope;
  placed_by: UUID;
  placed_at: ISODateString;
  max_duration_days: number;
  expires_at: ISODateString;
  released_at?: ISODateString | null;
  released_by?: UUID | null;
  release_justification?: string | null;
  affected_row_count_approx?: number;
}

export interface LegalHoldCreate {
  reason_code: string;
  ticket_id: string;
  justification: string;
  scope: LegalHoldScope;
  max_duration_days: number;
}

export interface LegalHoldRelease {
  justification: string;
}

export type LegalHoldList = PaginatedList<LegalHold>;

// ── Destruction Reports ───────────────────────────────────────────────────────

export type DestructionReportPeriod = "H1" | "H2";

export interface DestructionReport {
  id: UUID;
  tenant_id: UUID;
  period_year: number;
  period_half: DestructionReportPeriod;
  generated_at: ISODateString;
  generated_by?: UUID | null;
  signature_valid: boolean;
  blob_ref: string;
  summary: DestructionReportSummary;
}

export interface DestructionReportSummary {
  deleted_row_counts: Record<string, number>;
  minio_deletions: Record<string, number>;
  key_destructions: number;
  legal_hold_placements: number;
  legal_hold_releases: number;
  dsr_triggered_deletions: number;
  outstanding_holds_count: number;
}

export interface DestructionReportGenerate {
  period_year: number;
  period_half: DestructionReportPeriod;
}

export type DestructionReportList = PaginatedList<DestructionReport>;

// ── Live View ─────────────────────────────────────────────────────────────────

export interface LiveViewRequest {
  id: UUID;
  tenant_id: UUID;
  endpoint_id: UUID;
  requester_id: UUID;
  approver_id?: UUID | null;
  reason_code: string;
  duration_minutes: number;
  state: LiveViewState;
  requested_at: ISODateString;
  approved_at?: ISODateString | null;
  denied_at?: ISODateString | null;
  deny_reason?: string | null;
  /**
   * ADR 0026 — true when the session was created by an admin and
   * auto-approved, bypassing the HR/IT dual-control gate. Present in
   * every audit log entry under details.admin_bypass as well.
   */
  admin_bypass?: boolean;
}

export interface LiveViewSession {
  id: UUID;
  request_id: UUID;
  tenant_id: UUID;
  endpoint_id: UUID;
  livekit_room: string;
  viewer_token: string;
  state: LiveViewState;
  started_at: ISODateString;
  ended_at?: ISODateString | null;
  time_cap_seconds: number;
}

export interface LiveViewCreate {
  endpoint_id: UUID;
  reason_code: string;
  duration_minutes: number;
}

export interface LiveViewApprove {
  notes?: string;
}

export interface LiveViewReject {
  reason: string;
}

export interface LiveViewTerminate {
  reason: string;
}

export type LiveViewRequestList = PaginatedList<LiveViewRequest>;
export type LiveViewSessionList = PaginatedList<LiveViewSession>;

// ── Reports ───────────────────────────────────────────────────────────────────

export interface ProductivityDataPoint {
  hour: ISODateString;
  active_seconds: number;
  idle_seconds: number;
  endpoint_id: UUID;
}

export interface ProductivityReport {
  endpoint_id?: UUID;
  from: ISODateString;
  to: ISODateString;
  data: ProductivityDataPoint[];
}

export interface TopApp {
  app_name: string;
  total_active_seconds: number;
  percentage: number;
}

export interface TopAppsReport {
  endpoint_id?: UUID;
  from: ISODateString;
  to: ISODateString;
  items: TopApp[];
}

export interface IdleActivePoint {
  date: string;
  active_seconds: number;
  idle_seconds: number;
}

export interface IdleActiveReport {
  endpoint_id?: UUID;
  from: ISODateString;
  to: ISODateString;
  data: IdleActivePoint[];
  total_active_seconds: number;
  total_idle_seconds: number;
}

export interface AppBlockEvent {
  app_name: string;
  count: number;
  last_occurrence: ISODateString;
}

export interface AppBlocksReport {
  endpoint_id?: UUID;
  from: ISODateString;
  to: ISODateString;
  events: AppBlockEvent[];
}

// ── Screenshots ───────────────────────────────────────────────────────────────

export interface Screenshot {
  id: UUID;
  endpoint_id: UUID;
  captured_at: ISODateString;
  session_id?: UUID | null;
  is_sensitive: boolean;
}

export interface PresignedURL {
  url: string;
  expires_at: ISODateString;
}

export type ScreenshotList = PaginatedList<Screenshot>;

// ── Audit ─────────────────────────────────────────────────────────────────────

export interface AuditRecord {
  id: number;
  tenant_id: UUID;
  seq: number;
  type: string;
  actor_id?: UUID | null;
  subject_id?: UUID | null;
  payload_json: Record<string, unknown>;
  payload_hash: string;
  prev_hash: string;
  this_hash: string;
  created_at: ISODateString;
}

export type AuditList = PaginatedList<AuditRecord>;

export interface AuditChainStatus {
  valid: boolean;
  last_verified_at: ISODateString;
  chain_head_hash: string;
  total_records: number;
  broken_at_seq?: number | null;
}

// ── Silence ───────────────────────────────────────────────────────────────────

export interface SilenceGap {
  id: UUID;
  endpoint_id: UUID;
  gap_started_at: ISODateString;
  gap_ended_at?: ISODateString | null;
  duration_seconds: number;
  acknowledged: boolean;
  acknowledged_by?: UUID | null;
  acknowledged_at?: ISODateString | null;
  reason?: string | null;
}

export interface SilenceTimeline {
  endpoint_id: UUID;
  from: ISODateString;
  to: ISODateString;
  gaps: SilenceGap[];
}

export interface SilenceAcknowledge {
  reason: string;
  gap_id: UUID;
}

export type SilenceList = PaginatedList<SilenceGap>;

// ── DLP State ─────────────────────────────────────────────────────────────────

export interface DLPStateResponse {
  state: DLPState;
  enabled_at?: ISODateString | null;
  enabled_by?: UUID | null;
  disabled_at?: ISODateString | null;
  secret_id_issued: boolean;
  audit_ref?: string | null;
}

// ── Health ────────────────────────────────────────────────────────────────────

export interface HealthResponse {
  status: "ok" | "ready" | "degraded";
}

// ── Query params helpers ──────────────────────────────────────────────────────

export interface PaginationParams {
  page?: number;
  page_size?: number;
}

export interface DateRangeParams {
  from: ISODateString;
  to: ISODateString;
}
