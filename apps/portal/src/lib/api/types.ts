// ─────────────────────────────────────────────────────────────────────────────
// API Types — aligned to openapi.yaml /v1/me/* endpoints
// ─────────────────────────────────────────────────────────────────────────────

export interface Pagination {
  page: number;
  page_size: number;
  total: number;
  total_pages: number;
}

// ── /v1/me ────────────────────────────────────────────────────────────────────

export interface DataCategory {
  id: string;
  name_tr: string;
  description_tr: string;
  legal_basis_tr: string;
  retention_period_tr: string;
}

export interface MyDataResponse {
  user_id: string;
  categories: DataCategory[];
  collected_since: string;
}

// ── /v1/me/live-view-history ──────────────────────────────────────────────────

export type LiveViewState =
  | "REQUESTED"
  | "APPROVED"
  | "ACTIVE"
  | "ENDED"
  | "TERMINATED_BY_HR"
  | "TERMINATED_BY_DPO"
  | "EXPIRED"
  | "DENIED"
  | "FAILED";

export type RequesterRole = "yonetici" | "mudur";
export type ApproverRole = "ik";
export type ReasonCategory =
  | "performans_degerlendirme"
  | "guvenlik_incelemesi"
  | "is_sureci_denetimi"
  | "diger";

export interface MyLiveViewEntry {
  session_id: string;
  state: LiveViewState;
  requester_role: RequesterRole;
  approver_role: ApproverRole;
  reason_category: ReasonCategory;
  duration_seconds: number | null;
  started_at: string;
  ended_at: string | null;
}

export interface MyLiveViewHistory {
  items: MyLiveViewEntry[];
  restricted: boolean;
  pagination: Pagination;
}

// ── /v1/me/dsr ────────────────────────────────────────────────────────────────

export type DSRRequestType =
  | "access"
  | "rectify"
  | "erase"
  | "object"
  | "restrict"
  | "portability";

export type DSRState = "open" | "at_risk" | "overdue" | "closed" | "rejected";

export interface DSRRequest {
  id: string;
  employee_user_id: string;
  request_type: DSRRequestType;
  scope?: string | null;
  justification?: string | null;
  state: DSRState;
  created_at: string;
  sla_deadline: string;
  assigned_to?: string | null;
  response_artifact_ref?: string | null;
}

export interface DSRList {
  items: DSRRequest[];
  pagination: Pagination;
}

export interface MyDSRCreate {
  request_type: DSRRequestType;
  scope?: string;
  justification?: string;
}

// ── DLP State ─────────────────────────────────────────────────────────────────
// NOTE: /v1/system/dlp-state is referenced in ADR 0013 and mvp-scope.md
// but is NOT yet present in openapi.yaml — flagged as missing API endpoint.

export type DLPStatus = "disabled" | "enabled";

export interface DLPStateResponse {
  status: DLPStatus;
  enabled_at?: string | null;
  ceremony_reference?: string | null;
  disabled_at?: string | null;
}

// ── First-login acknowledgement ───────────────────────────────────────────────
// NOTE: /v1/me/acknowledge-notification is NOT in openapi.yaml — flagged as missing.

export interface AcknowledgeNotificationRequest {
  notification_type: "first_login_disclosure";
}

export interface AcknowledgeNotificationResponse {
  acknowledged: boolean;
  acknowledged_at: string;
  audit_chain_ref: string;
}

// ── Generic API error (RFC 7807) ──────────────────────────────────────────────

export interface ApiProblem {
  type?: string;
  title?: string;
  status: number;
  detail?: string;
  instance?: string;
}

export class ApiError extends Error {
  public readonly status: number;
  public readonly detail: string;

  constructor(problem: ApiProblem) {
    super(problem.detail ?? problem.title ?? "API error");
    this.name = "ApiError";
    this.status = problem.status;
    this.detail = problem.detail ?? problem.title ?? "API error";
  }
}
