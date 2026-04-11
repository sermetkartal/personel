/**
 * API types for the Personel Mobile Admin app.
 * Derived from apps/api/api/openapi.yaml schemas.
 * Mobile-BFF endpoints are marked with the TODO for backend-developer.
 */

// ── RFC 7807 Problem Details ────────────────────────────────────────────────

export interface ProblemDetail {
  type?: string;
  title: string;
  status: number;
  detail?: string;
  instance?: string;
}

export class ApiError extends Error {
  readonly status: number;
  readonly problem: ProblemDetail;

  constructor(status: number, problem: ProblemDetail) {
    super(problem.detail ?? problem.title);
    this.status = status;
    this.problem = problem;
    this.name = "ApiError";
  }
}

// ── Pagination ───────────────────────────────────────────────────────────────

export interface PaginationMeta {
  page: number;
  page_size: number;
  total: number;
}

export interface PaginatedResponse<T> {
  data: T[];
  meta: PaginationMeta;
}

// ── Mobile Summary ────────────────────────────────────────────────────────────
// TODO (backend-developer): Add GET /v1/mobile/summary to mobile-bff
// that aggregates: pending live-view count, pending DSR count,
// silence alerts in last 24h, last 5 audit entries.

export interface MobileSummary {
  pending_live_view_approvals: number;
  pending_dsrs: number;
  silence_alerts_24h: number;
  recent_audit_entries: AuditEntry[];
}

// ── Live View ────────────────────────────────────────────────────────────────

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

export interface LiveViewRequest {
  id: string;
  tenant_id: string;
  endpoint_id: string;
  endpoint_hostname: string;
  requester_id: string;
  requester_username: string;
  reason_code: string;
  duration_minutes: number;
  state: LiveViewState;
  requested_at: string;
  approved_at?: string;
  approver_id?: string;
  denied_at?: string;
  denier_id?: string;
  deny_reason?: string;
}

export interface LiveViewApproveRequest {
  notes?: string;
}

export interface LiveViewRejectRequest {
  reason?: string;
}

// ── DSR (KVKK m.11) ──────────────────────────────────────────────────────────

export type DSRType =
  | "access"
  | "rectify"
  | "erase"
  | "object"
  | "restrict"
  | "portability";

export type DSRState = "open" | "at_risk" | "overdue" | "resolved" | "rejected";

export interface DSRRequest {
  id: string;
  tenant_id: string;
  employee_id: string;
  employee_name?: string;
  request_type: DSRType;
  state: DSRState;
  scope?: string;
  justification?: string;
  submitted_at: string;
  sla_deadline: string;
  extended_deadline?: string;
  assigned_to?: string;
  artifact_ref?: string;
  notes?: string;
}

export interface DSRRespondRequest {
  artifact_ref: string;
  notes?: string;
}

// ── Silence (Flow 7) ──────────────────────────────────────────────────────────

export interface SilenceGap {
  id: string;
  tenant_id: string;
  endpoint_id: string;
  endpoint_hostname: string;
  started_at: string;
  ended_at?: string;
  duration_seconds?: number;
  acknowledged: boolean;
  acknowledged_by?: string;
  acknowledged_at?: string;
  reason?: string;
}

export interface SilenceAcknowledgeRequest {
  reason: string;
}

// ── Audit ──────────────────────────────────────────────────────────────────────

export interface AuditEntry {
  id: string;
  seq: number;
  action: string;
  actor_id: string;
  subject_id?: string;
  timestamp: string;
  hash: string;
  prev_hash: string;
}

// ── Push Notification Payload ─────────────────────────────────────────────────
// KVKK: Push payloads MUST NOT contain PII.
// Only ticket IDs and counts are sent. Details are fetched after user taps.
// See ADR 0019 §Push Privacy and c4-container-phase-2.md mobile-bff notes.

export type PushNotificationType =
  | "live_view_request"
  | "dsr_new"
  | "silence_alert"
  | "audit_spike";

export interface PushNotificationPayload {
  type: PushNotificationType;
  count: number;
  deep_link: string;
}

// ── Push Token Registration ───────────────────────────────────────────────────
// TODO (backend-developer): Add POST /v1/mobile/push-tokens to mobile-bff

export interface PushTokenRegistration {
  token: string;
  platform: "ios" | "android";
  device_id: string;
}
