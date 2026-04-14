/**
 * Unit tests for endpoint lifecycle RBAC (Phase 2 — items #90).
 *
 * Covers the new admin-only / IT-scoped operations: deactivate, wipe,
 * refresh-token, revoke-cert. The server API is authoritative but these
 * ensure the UI never surfaces forbidden buttons to the wrong role.
 */

import { describe, it, expect } from "vitest";
import { can } from "@/lib/auth/rbac";
import type { Role } from "@/lib/api/types";

const ALL_ROLES: Role[] = [
  "employee",
  "auditor",
  "investigator",
  "hr",
  "manager",
  "it_operator",
  "it_manager",
  "dpo",
  "admin",
];

describe("Endpoint lifecycle RBAC", () => {
  it("deactivate:endpoint allowed only for admin + it_manager", () => {
    const allowed: Role[] = ["admin", "it_manager"];
    for (const r of ALL_ROLES) {
      expect(can(r, "deactivate:endpoint")).toBe(allowed.includes(r));
    }
  });

  it("wipe:endpoint allowed for admin, it_manager, investigator", () => {
    const allowed: Role[] = ["admin", "it_manager", "investigator"];
    for (const r of ALL_ROLES) {
      expect(can(r, "wipe:endpoint")).toBe(allowed.includes(r));
    }
  });

  it("refresh:endpoint-token allowed only for admin + it_manager", () => {
    const allowed: Role[] = ["admin", "it_manager"];
    for (const r of ALL_ROLES) {
      expect(can(r, "refresh:endpoint-token")).toBe(allowed.includes(r));
    }
  });

  it("revoke:endpoint-cert allowed only for admin + dpo", () => {
    const allowed: Role[] = ["admin", "dpo"];
    for (const r of ALL_ROLES) {
      expect(can(r, "revoke:endpoint-cert")).toBe(allowed.includes(r));
    }
  });
});

describe("DSR erasure RBAC", () => {
  it("execute:dsr-erasure is DPO-only", () => {
    for (const r of ALL_ROLES) {
      expect(can(r, "execute:dsr-erasure")).toBe(r === "dpo");
    }
    // Admins explicitly cannot commit irreversible destruction per
    // KVKK m.11/f dual-control policy.
    expect(can("admin", "execute:dsr-erasure")).toBe(false);
  });
});
