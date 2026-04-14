/**
 * Policy management API query functions.
 */

import { apiClient, type FetchOptions } from "./client";
import type {
  PaginationParams,
  Policy,
  PolicyCreate,
  PolicyList,
  PolicyPushRequest,
  PolicyPushResult,
  PolicyRules,
  PolicyUpdate,
} from "./types";

export const policyKeys = {
  all: ["policies"] as const,
  list: (params: PaginationParams) => ["policies", "list", params] as const,
  detail: (id: string) => ["policies", "detail", id] as const,
  assignments: (endpointID: string) =>
    ["policies", "assignments", endpointID] as const,
};

export async function listPolicies(
  params: PaginationParams = {},
  opts?: FetchOptions,
): Promise<PolicyList> {
  const qs = apiClient.buildQuery(params);
  return apiClient.get<PolicyList>(`/v1/policies${qs}`, opts);
}

export async function getPolicy(
  id: string,
  opts?: FetchOptions,
): Promise<Policy> {
  return apiClient.get<Policy>(`/v1/policies/${id}`, opts);
}

export async function createPolicy(
  req: PolicyCreate,
  opts?: FetchOptions,
): Promise<Policy> {
  return apiClient.post<Policy>("/v1/policies", req, opts);
}

export async function updatePolicy(
  id: string,
  req: PolicyUpdate,
  opts?: FetchOptions,
): Promise<Policy> {
  return apiClient.patch<Policy>(`/v1/policies/${id}`, req, opts);
}

export async function deletePolicy(
  id: string,
  opts?: FetchOptions,
): Promise<void> {
  return apiClient.delete<void>(`/v1/policies/${id}`, opts);
}

export async function pushPolicy(
  id: string,
  req: PolicyPushRequest,
  opts?: FetchOptions,
): Promise<PolicyPushResult> {
  return apiClient.post<PolicyPushResult>(`/v1/policies/${id}/push`, req, opts);
}

/**
 * Publish a policy: server signs with control-plane key and broadcasts to all
 * active endpoints for the tenant. Uses the push endpoint with an empty
 * endpoint_ids array to denote "all" (server short-circuits).
 */
export async function publishPolicy(
  id: string,
  opts?: FetchOptions,
): Promise<PolicyPushResult> {
  return apiClient.post<PolicyPushResult>(
    `/v1/policies/${id}/publish`,
    {},
    opts,
  );
}

/**
 * List policies currently assigned to a given endpoint (active + historical).
 */
export async function listPolicyAssignments(
  endpointID: string,
  opts?: FetchOptions,
): Promise<PolicyList> {
  return apiClient.get<PolicyList>(
    `/v1/endpoints/${endpointID}/policies`,
    opts,
  );
}

// ── UI helpers ───────────────────────────────────────────────────────────────

/**
 * Visual rule kinds the Policy Editor exposes. Each maps to a slice of
 * PolicyRules when serialised. The editor keeps an in-memory "rule list"
 * and projects it to the backend PolicyRules shape on save.
 */
export type VisualRuleKind =
  | "app_allowlist"
  | "app_distracting"
  | "path_sensitive"
  | "url_blocklist"
  | "screenshot_exclude"
  | "keystroke_dlp_opt_in";

export interface VisualRule {
  id: string;
  kind: VisualRuleKind;
  values: string[];
  note?: string;
}

export function emptyRules(): PolicyRules {
  return {
    screenshot_enabled: true,
    screenshot_interval_seconds: 120,
    app_allow_list: [],
    app_block_list: [],
    sensitivity_guard: {
      window_title_sensitive_regex: [],
      sensitive_host_globs: [],
      screenshot_exclude_apps: [],
      auto_flag_on_m6_dlp_match: true,
    },
  };
}

/**
 * Project a flat VisualRule[] into the backend PolicyRules shape, preserving
 * any scalar fields already on `base` (screenshot interval etc).
 */
export function rulesFromVisual(
  visual: VisualRule[],
  base: PolicyRules = emptyRules(),
): PolicyRules {
  const next: PolicyRules = {
    screenshot_enabled: base.screenshot_enabled ?? true,
    screenshot_interval_seconds: base.screenshot_interval_seconds ?? 120,
    app_allow_list: [],
    app_block_list: [],
    sensitivity_guard: {
      window_title_sensitive_regex:
        base.sensitivity_guard?.window_title_sensitive_regex ?? [],
      sensitive_host_globs: [],
      screenshot_exclude_apps: [],
      auto_flag_on_m6_dlp_match:
        base.sensitivity_guard?.auto_flag_on_m6_dlp_match ?? true,
    },
  };
  for (const r of visual) {
    switch (r.kind) {
      case "app_allowlist":
        next.app_allow_list = [...(next.app_allow_list ?? []), ...r.values];
        break;
      case "app_distracting":
        next.app_block_list = [...(next.app_block_list ?? []), ...r.values];
        break;
      case "path_sensitive":
        next.sensitivity_guard!.window_title_sensitive_regex = [
          ...(next.sensitivity_guard!.window_title_sensitive_regex ?? []),
          ...r.values,
        ];
        break;
      case "url_blocklist":
        next.sensitivity_guard!.sensitive_host_globs = [
          ...(next.sensitivity_guard!.sensitive_host_globs ?? []),
          ...r.values,
        ];
        break;
      case "screenshot_exclude":
        next.sensitivity_guard!.screenshot_exclude_apps = [
          ...(next.sensitivity_guard!.screenshot_exclude_apps ?? []),
          ...r.values,
        ];
        break;
      case "keystroke_dlp_opt_in":
        // Policy editor cannot *enable* DLP — ADR 0013 gates that through
        // the Vault ceremony. Visual rule here is an advisory marker only;
        // it is NOT projected into PolicyRules so that a malicious edit of
        // the editor cannot bypass DLP-off-by-default invariants.
        break;
    }
  }
  return next;
}

/**
 * Recover a VisualRule[] from a PolicyRules blob (used when opening an
 * existing policy for editing). Loses rule-group identity — we rebuild a
 * flat list with one group per kind.
 */
export function visualFromRules(rules: PolicyRules): VisualRule[] {
  const out: VisualRule[] = [];
  const push = (kind: VisualRuleKind, values: string[] | undefined) => {
    if (values && values.length > 0) {
      out.push({ id: crypto.randomUUID(), kind, values: [...values] });
    }
  };
  push("app_allowlist", rules.app_allow_list);
  push("app_distracting", rules.app_block_list);
  push(
    "path_sensitive",
    rules.sensitivity_guard?.window_title_sensitive_regex,
  );
  push("url_blocklist", rules.sensitivity_guard?.sensitive_host_globs);
  push("screenshot_exclude", rules.sensitivity_guard?.screenshot_exclude_apps);
  return out;
}

/**
 * Validate a glob pattern. Returns null on success or a short error message.
 * Glob supports: * ** ? and literal path chars. Invalid constructs: `**` not
 * bounded by path separators, empty segments, unescaped brackets.
 */
export function validateGlob(pattern: string): string | null {
  if (!pattern || pattern.trim() === "") return "empty";
  if (/\*{3,}/.test(pattern)) return "too_many_stars";
  // Allow the full URL + Windows-path alphabet that SensitivityGuard
  // patterns actually need: letters, digits, *, ?, slashes (forward
  // and back), dots, dashes, underscores, colon (http://, C:), equals,
  // ampersand, percent, hash, tilde.
  if (!/^[\w\-*?./\\:=&%#~]+$/i.test(pattern)) return "bad_chars";
  return null;
}

/**
 * Validate a regex pattern by attempting to compile it.
 */
export function validateRegex(pattern: string): string | null {
  if (!pattern || pattern.trim() === "") return "empty";
  try {
    new RegExp(pattern);
    return null;
  } catch {
    return "invalid_regex";
  }
}
