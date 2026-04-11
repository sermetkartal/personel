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

export function canViewEmployees(role: Role): boolean {
  return (
    role === "admin" ||
    role === "dpo" ||
    role === "hr" ||
    role === "manager"
  );
}

export function canManagePolicies(role: Role): boolean {
  return role === "admin";
}

export function canViewPolicies(role: Role): boolean {
  return role === "admin" || role === "dpo" || role === "manager";
}

export function canRequestLiveView(role: Role): boolean {
  return role === "admin" || role === "manager";
}

export function canApproveLiveView(role: Role): boolean {
  return role === "hr";
}

export function canTerminateLiveView(role: Role): boolean {
  return role === "hr" || role === "dpo";
}

export function canViewLiveViewSessions(role: Role): boolean {
  return (
    role === "admin" ||
    role === "manager" ||
    role === "hr" ||
    role === "dpo"
  );
}

export function canWatchLiveView(role: Role): boolean {
  return role === "admin" || role === "manager" || role === "dpo";
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

export function canViewDLPSettings(role: Role): boolean {
  return role === "admin" || role === "dpo";
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
  | "view:screenshots"
  | "view:audit-log"
  | "view:audit-trail"
  | "verify:audit-chain"
  | "manage:users"
  | "manage:tenants"
  | "view:dlp-settings"
  | "view:silence"
  | "view:silence-gaps"
  | "view:reports"
  | "view:settings";

const ACTION_CHECKS: Record<Action, (role: Role) => boolean> = {
  "view:endpoints": canViewEndpoints,
  "manage:endpoints": canManageEndpoints,
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
  "view:screenshots": canViewScreenshots,
  "view:audit-log": canViewAuditLog,
  "view:audit-trail": canViewAuditTrail,
  "verify:audit-chain": canVerifyAuditChain,
  "manage:users": canManageUsers,
  "manage:tenants": canManageTenants,
  "view:dlp-settings": canViewDLPSettings,
  "view:silence": canViewSilence,
  "view:silence-gaps": canViewSilenceGaps,
  "view:reports": canViewReports,
  "view:settings": canViewSettings,
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
