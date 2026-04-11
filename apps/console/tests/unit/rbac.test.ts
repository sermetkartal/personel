/**
 * Unit tests for RBAC permission helpers.
 *
 * These tests verify that the can() function correctly maps roles to actions.
 * The API enforces RBAC server-side; these tests protect against regressions
 * in the UI-side enforcement.
 */

import { describe, it, expect } from "vitest";
import { can, requiredRoleForAction } from "@/lib/auth/rbac";
import type { Role, } from "@/lib/api/types";
import type { Action } from "@/lib/auth/rbac";

const ALL_ROLES: Role[] = [
  "employee",
  "auditor",
  "investigator",
  "hr",
  "manager",
  "dpo",
  "admin",
];

describe("RBAC — can()", () => {
  // ── DLP settings ────────────────────────────────────────────────────────────
  describe("view:dlp-settings", () => {
    it("allows admin and dpo", () => {
      expect(can("admin", "view:dlp-settings")).toBe(true);
      expect(can("dpo", "view:dlp-settings")).toBe(true);
    });

    it("denies all other roles", () => {
      const denied: Role[] = ["employee", "auditor", "investigator", "hr", "manager"];
      for (const role of denied) {
        expect(can(role, "view:dlp-settings")).toBe(false);
      }
    });
  });

  // ── Live view dual-control ───────────────────────────────────────────────────
  describe("approve:live-view", () => {
    it("only HR can approve", () => {
      expect(can("hr", "approve:live-view")).toBe(true);
    });

    it("denies non-HR roles (including manager who can request)", () => {
      const denied: Role[] = ["employee", "auditor", "investigator", "manager", "dpo", "admin"];
      for (const role of denied) {
        expect(can(role, "approve:live-view")).toBe(false);
      }
    });
  });

  describe("request:live-view", () => {
    it("admin and manager can request", () => {
      expect(can("admin", "request:live-view")).toBe(true);
      expect(can("manager", "request:live-view")).toBe(true);
    });

    it("HR cannot request (only approve)", () => {
      expect(can("hr", "request:live-view")).toBe(false);
    });
  });

  describe("terminate:live-view", () => {
    it("HR and DPO can terminate", () => {
      expect(can("hr", "terminate:live-view")).toBe(true);
      expect(can("dpo", "terminate:live-view")).toBe(true);
    });
  });

  // ── DSR ──────────────────────────────────────────────────────────────────────
  describe("manage:dsr", () => {
    it("only DPO can manage DSRs", () => {
      expect(can("dpo", "manage:dsr")).toBe(true);
    });

    it("admin cannot manage (view only)", () => {
      expect(can("admin", "manage:dsr")).toBe(false);
    });
  });

  describe("create:dsr", () => {
    it("employee can create a DSR for themselves", () => {
      expect(can("employee", "create:dsr")).toBe(true);
    });

    it("dpo and admin can create on behalf", () => {
      expect(can("dpo", "create:dsr")).toBe(true);
      expect(can("admin", "create:dsr")).toBe(true);
    });
  });

  // ── Legal hold ───────────────────────────────────────────────────────────────
  describe("place:legal-hold", () => {
    it("only DPO can place a legal hold", () => {
      expect(can("dpo", "place:legal-hold")).toBe(true);
    });

    it("admin cannot place (intentional constraint)", () => {
      expect(can("admin", "place:legal-hold")).toBe(false);
    });
  });

  // ── Audit ─────────────────────────────────────────────────────────────────────
  describe("view:audit-log", () => {
    it("admin, dpo, and auditor can view", () => {
      expect(can("admin", "view:audit-log")).toBe(true);
      expect(can("dpo", "view:audit-log")).toBe(true);
      expect(can("auditor", "view:audit-log")).toBe(true);
    });

    it("employee, hr, manager cannot view audit log", () => {
      const denied: Role[] = ["employee", "hr", "manager", "investigator"];
      for (const role of denied) {
        expect(can(role, "view:audit-log")).toBe(false);
      }
    });
  });

  // ── Screenshots (sensitive) ───────────────────────────────────────────────────
  describe("view:screenshots", () => {
    it("investigator and dpo can view", () => {
      expect(can("investigator", "view:screenshots")).toBe(true);
      expect(can("dpo", "view:screenshots")).toBe(true);
    });

    it("admin cannot view raw screenshots (KVKK proportionality)", () => {
      expect(can("admin", "view:screenshots")).toBe(false);
    });
  });

  // ── Tenant management ─────────────────────────────────────────────────────────
  describe("manage:tenants", () => {
    it("only admin can manage tenants", () => {
      expect(can("admin", "manage:tenants")).toBe(true);
      const nonAdmin: Role[] = ["employee", "auditor", "investigator", "hr", "manager", "dpo"];
      for (const role of nonAdmin) {
        expect(can(role, "manage:tenants")).toBe(false);
      }
    });
  });
});

describe("requiredRoleForAction()", () => {
  it("returns lowest role that satisfies the action", () => {
    expect(requiredRoleForAction("approve:live-view")).toBe("hr");
    expect(requiredRoleForAction("manage:dsr")).toBe("dpo");
    expect(requiredRoleForAction("manage:tenants")).toBe("admin");
  });

  it("returns a role for every action (no action is unreachable)", () => {
    const actions: Action[] = [
      "view:endpoints",
      "manage:endpoints",
      "view:employees",
      "view:policies",
      "manage:policies",
      "request:live-view",
      "approve:live-view",
      "terminate:live-view",
      "watch:live-view",
      "view:live-view-sessions",
      "view:dsr",
      "create:dsr",
      "manage:dsr",
      "place:legal-hold",
      "view:destruction-reports",
      "download:destruction-reports",
      "view:screenshots",
      "view:audit-log",
      "view:audit-trail",
      "verify:audit-chain",
      "manage:users",
      "manage:tenants",
      "view:dlp-settings",
      "view:silence",
      "view:silence-gaps",
      "view:reports",
      "view:settings",
    ];

    for (const action of actions) {
      const role = requiredRoleForAction(action);
      expect(ALL_ROLES).toContain(role);
      // The returned role must actually be able to perform the action
      expect(can(role, action)).toBe(true);
    }
  });
});
