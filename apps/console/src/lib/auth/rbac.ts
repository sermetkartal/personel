/**
 * Client-side RBAC helpers.
 * Mirrors the server-side RBAC enforced by the Admin API.
 *
 * These helpers are INFORMATIONAL and used to:
 * 1. Hide/show UI elements based on role
 * 2. Show descriptive "unauthorized" messages
 *
 * They do NOT replace server-side enforcement. The API always enforces RBAC.
 */

import type { Role } from "@/lib/api/types";

// ── Role hierarchy ────────────────────────────────────────────────────────────

/**
 * Ordered role set — higher index = higher privilege in general.
 * Note: this is a simplification; actual RBAC is per-action.
 */
const ROLE_LEVELS: Record<Role, number> = {
  employee: 0,
  auditor: 1,
  investigator: 2,
  hr: 3,
  manager: 3,
  it_operator: 3,
  it_manager: 4,
  dpo: 4,
  admin: 5,
};

export function hasHigherOrEqualRole(userRole: Role, requiredRole: Role): boolean {
  return ROLE_LEVELS[userRole] >= ROLE_LEVELS[requiredRole];
}

// ── Feature-level permission helpers ─────────────────────────────────────────

export function canViewEndpoints(role: Role): boolean {
  return (
    role === "admin" ||
    role === "dpo" ||
    role === "manager" ||
    role === "auditor"
  );
}

export function canManageEndpoints(role: Role): boolean {
  return role === "admin" || role === "dpo";
}

/**
 * Who can operationally deactivate / refresh tokens / wipe endpoints.
 * These are day-to-day IT fleet ops — not KVKK-scoped moves.
 */
export function canDeactivateEndpoint(role: Role): boolean {
  return role === "admin" || role === "it_manager";
}

export function canWipeEndpoint(role: Role): boolean {
  return role === "admin" || role === "it_manager" || role === "investigator";
}

export function canRefreshEndpointToken(role: Role): boolean {
  return role === "admin" || role === "it_manager";
}

export function canRevokeEndpointCert(role: Role): boolean {
  return role === "admin" || role === "dpo";
}

/**
 * Erasure requests (KVKK m.11/f) can only be finally executed by a DPO.
 * Non-DPO roles (including admin) can inspect dry-run projections but not
 * commit destruction.
 */
export function canExecuteDSRErasure(role: Role): boolean {
  return role === "dpo";
}

export function canViewEmployees(role: Role): boolean {
  return (
    role === "admin" ||
    role === "dpo" ||
    role === "hr" ||
    role === "manager" ||
    role === "it_manager" ||
    role === "it_operator" ||
    role === "investigator" ||
    role === "auditor"
  );
}

export function canManagePolicies(role: Role): boolean {
  return role === "admin";
}

export function canViewPolicies(role: Role): boolean {
  return role === "admin" || role === "dpo" || role === "manager";
}

// Live view authority is IT-department-owned. HR has no authority
// over technical device access in the Turkish enterprise model.
//
//   it_operator → requests
//   it_manager  → approves + terminates (dual-control vs requester)
//   admin       → approves + terminates (ultimate IT authority)
//   dpo         → compliance-override termination only (KVKK scope)
//
// Other roles (hr, manager, investigator) can only watch existing
// sessions as observers where permitted, never approve or terminate.
export function canRequestLiveView(role: Role): boolean {
  return role === "admin" || role === "it_manager" || role === "it_operator";
}

export function canApproveLiveView(role: Role): boolean {
  return role === "admin" || role === "it_manager";
}

export function canTerminateLiveView(role: Role): boolean {
  return role === "admin" || role === "it_manager" || role === "dpo";
}

export function canViewLiveViewSessions(role: Role): boolean {
  return (
    role === "admin" ||
    role === "it_manager" ||
    role === "it_operator" ||
    role === "manager" ||
    role === "dpo" ||
    role === "investigator"
  );
}

export function canWatchLiveView(role: Role): boolean {
  return (
    role === "admin" ||
    role === "it_manager" ||
    role === "it_operator" ||
    role === "dpo"
  );
}

export function canViewDSR(role: Role): boolean {
  return role === "dpo" || role === "admin";
}

export function canCreateDSR(role: Role): boolean {
  // Employees submit their own DSRs; admin/dpo can submit on behalf
  return (
    role === "employee" ||
    role === "dpo" ||
    role === "admin"
  );
}

export function canManageDSR(role: Role): boolean {
  return role === "dpo";
}

export function canPlaceLegalHold(role: Role): boolean {
  return role === "dpo";
}

export function canViewDestructionReports(role: Role): boolean {
  return role === "dpo" || role === "admin";
}

// SOC 2 evidence locker — DPO and auditor roles only. Coverage and pack
// export are sensitive compliance posture indicators; ordinary admins
// should not see gap state or pull the signed ZIP.
export function canViewEvidence(role: Role): boolean {
  return role === "dpo" || role === "auditor";
}

export function canDownloadEvidencePack(role: Role): boolean {
  return role === "dpo";
}

export function canDownloadDestructionReports(role: Role): boolean {
  return role === "dpo";
}

export function canViewScreenshots(role: Role): boolean {
  return role === "investigator" || role === "dpo";
}

export function canViewAuditTrail(role: Role): boolean {
  return (
    role === "admin" ||
    role === "dpo" ||
    role === "auditor"
  );
}

export function canVerifyAuditChain(role: Role): boolean {
  return role === "admin" || role === "dpo";
}

export function canManageUsers(role: Role): boolean {
  return role === "admin";
}

export function canManageTenants(role: Role): boolean {
  return role === "admin";
}

/** Manage per-tenant screenshot capture preset — admin + IT manager. */
export function canManageScreenshotPreset(role: Role): boolean {
  return role === "admin" || role === "it_manager";
}

export function canViewDLPSettings(role: Role): boolean {
  return role === "admin" || role === "dpo";
}

/**
 * KVKK compliance area — VERBIS kayıt, aydınlatma metni, DPA, DPIA,
 * açık rıza formu. DPO is primary owner, admin has full access,
 * auditor can read-only.
 */
export function canManageKVKK(role: Role): boolean {
  return role === "admin" || role === "dpo";
}

export function canViewKVKK(role: Role): boolean {
  return role === "admin" || role === "dpo" || role === "auditor";
}

export function canViewSilence(role: Role): boolean {
  return role === "admin" || role === "manager" || role === "dpo";
}

export function canViewSettings(role: Role): boolean {
  return role === "admin" || role === "dpo";
}

export function canViewSilenceGaps(role: Role): boolean {
  return role === "admin" || role === "manager" || role === "dpo" || role === "auditor";
}

export function canViewAuditLog(role: Role): boolean {
  return role === "admin" || role === "dpo" || role === "auditor";
}

export function canViewReports(role: Role): boolean {
  return (
    role === "admin" ||
    role === "manager" ||
    role === "dpo" ||
    role === "auditor"
  );
}

// ── Generic can() helper ──────────────────────────────────────────────────────

export type Action =
  | "view:endpoints"
  | "manage:endpoints"
  | "deactivate:endpoint"
  | "wipe:endpoint"
  | "refresh:endpoint-token"
  | "revoke:endpoint-cert"
  | "execute:dsr-erasure"
  | "view:employees"
  | "view:policies"
  | "manage:policies"
  | "request:live-view"
  | "approve:live-view"
  | "terminate:live-view"
  | "watch:live-view"
  | "view:live-view-sessions"
  | "view:dsr"
  | "create:dsr"
  | "manage:dsr"
  | "place:legal-hold"
  | "view:destruction-reports"
  | "download:destruction-reports"
  | "view:evidence"
  | "download:evidence-pack"
  | "view:screenshots"
  | "view:audit-log"
  | "view:audit-trail"
  | "verify:audit-chain"
  | "manage:users"
  | "manage:tenants"
  | "manage:screenshot-preset"
  | "view:dlp-settings"
  | "view:silence"
  | "view:silence-gaps"
  | "view:reports"
  | "view:settings"
  | "manage:kvkk"
  | "view:kvkk";

const ACTION_CHECKS: Record<Action, (role: Role) => boolean> = {
  "view:endpoints": canViewEndpoints,
  "manage:endpoints": canManageEndpoints,
  "deactivate:endpoint": canDeactivateEndpoint,
  "wipe:endpoint": canWipeEndpoint,
  "refresh:endpoint-token": canRefreshEndpointToken,
  "revoke:endpoint-cert": canRevokeEndpointCert,
  "execute:dsr-erasure": canExecuteDSRErasure,
  "view:employees": canViewEmployees,
  "view:policies": canViewPolicies,
  "manage:policies": canManagePolicies,
  "request:live-view": canRequestLiveView,
  "approve:live-view": canApproveLiveView,
  "terminate:live-view": canTerminateLiveView,
  "watch:live-view": canWatchLiveView,
  "view:live-view-sessions": canViewLiveViewSessions,
  "view:dsr": canViewDSR,
  "create:dsr": canCreateDSR,
  "manage:dsr": canManageDSR,
  "place:legal-hold": canPlaceLegalHold,
  "view:destruction-reports": canViewDestructionReports,
  "download:destruction-reports": canDownloadDestructionReports,
  "view:evidence": canViewEvidence,
  "download:evidence-pack": canDownloadEvidencePack,
  "view:screenshots": canViewScreenshots,
  "view:audit-log": canViewAuditLog,
  "view:audit-trail": canViewAuditTrail,
  "verify:audit-chain": canVerifyAuditChain,
  "manage:users": canManageUsers,
  "manage:tenants": canManageTenants,
  "manage:screenshot-preset": canManageScreenshotPreset,
  "view:dlp-settings": canViewDLPSettings,
  "view:silence": canViewSilence,
  "view:silence-gaps": canViewSilenceGaps,
  "view:reports": canViewReports,
  "view:settings": canViewSettings,
  "manage:kvkk": canManageKVKK,
  "view:kvkk": canViewKVKK,
};

/**
 * Generic permission check.
 * Usage: can(user.role, "manage:dsr")
 */
export function can(role: Role, action: Action): boolean {
  const check = ACTION_CHECKS[action];
  return check ? check(role) : false;
}

/**
 * Returns the minimum role required for a given action (for display purposes).
 */
export function requiredRoleForAction(action: Action): Role {
  const candidates: Role[] = [
    "employee",
    "auditor",
    "investigator",
    "hr",
    "manager",
    "dpo",
    "admin",
  ];
  for (const role of candidates) {
    if (can(role, action)) return role;
  }
  return "admin";
}
