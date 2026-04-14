/**
 * Personel Admin API fetch wrapper.
 *
 * Features:
 * - Bearer token injection from session cookie
 * - RFC 7807 error parsing into typed ApiError
 * - Automatic silent token refresh on 401 (Keycloak refresh_token grant)
 * - OpenTelemetry W3C trace context propagation (traceparent header)
 * - Typed response parsing
 * - Abort signal passthrough
 */

import type { ProblemDetail } from "./types";

/**
 * Client-side requests are routed through a Next.js API proxy at
 * `/api/proxy/*`. The proxy reads the httpOnly session cookie server-side
 * and injects the Bearer token, so the token never enters the browser
 * bundle. Server components still talk to the real API directly via
 * the `token` option + NEXT_PUBLIC_API_BASE_URL fallback.
 */
const API_BASE =
  typeof window === "undefined"
    ? process.env.NEXT_PUBLIC_API_BASE_URL ?? "http://localhost:8080"
    : "/api/proxy";

// ── Typed API error ───────────────────────────────────────────────────────────

export class ApiError extends Error {
  readonly status: number;
  readonly problem: ProblemDetail;

  constructor(status: number, problem: ProblemDetail) {
    super(problem.detail ?? problem.title);
    this.name = "ApiError";
    this.status = status;
    this.problem = problem;
  }

  get isUnauthorized(): boolean {
    return this.status === 401;
  }

  get isForbidden(): boolean {
    return this.status === 403;
  }

  get isNotFound(): boolean {
    return this.status === 404;
  }

  get isConflict(): boolean {
    return this.status === 409;
  }

  get isValidation(): boolean {
    return this.status === 400;
  }
}

export class NetworkError extends Error {
  constructor(cause?: unknown) {
    super("Network request failed");
    this.name = "NetworkError";
    this.cause = cause;
  }
}

// ── Token store (browser-side in-memory; server-side from cookie) ─────────────

let _accessToken: string | null = null;
let _refreshToken: string | null = null;
let _refreshPromise: Promise<boolean> | null = null;

export function setTokens(access: string, refresh: string): void {
  _accessToken = access;
  _refreshToken = refresh;
}

export function clearTokens(): void {
  _accessToken = null;
  _refreshToken = null;
}

// ── Silent refresh ────────────────────────────────────────────────────────────

async function attemptSilentRefresh(): Promise<boolean> {
  if (_refreshPromise) {
    return _refreshPromise;
  }

  _refreshPromise = (async () => {
    try {
      const res = await fetch("/api/auth/session", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ action: "refresh" }),
      });

      if (!res.ok) return false;

      const data = (await res.json()) as {
        access_token: string;
        refresh_token: string;
      };
      setTokens(data.access_token, data.refresh_token);
      return true;
    } catch {
      return false;
    } finally {
      _refreshPromise = null;
    }
  })();

  return _refreshPromise;
}

// ── Trace context ─────────────────────────────────────────────────────────────

function generateTraceId(): string {
  const arr = new Uint8Array(16);
  if (typeof crypto !== "undefined") {
    crypto.getRandomValues(arr);
  }
  return Array.from(arr)
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}

function generateSpanId(): string {
  const arr = new Uint8Array(8);
  if (typeof crypto !== "undefined") {
    crypto.getRandomValues(arr);
  }
  return Array.from(arr)
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}

function buildTraceparent(): string {
  return `00-${generateTraceId()}-${generateSpanId()}-01`;
}

// ── Core fetch ────────────────────────────────────────────────────────────────

export interface FetchOptions extends Omit<RequestInit, "body"> {
  body?: unknown;
  /**
   * When true, the server-side session endpoint is used to obtain the Bearer
   * token instead of the in-memory store. Used from Server Components.
   */
  serverSide?: boolean;
  /**
   * Explicit bearer token override. Server Components pass the session
   * user's access_token directly because the /api/auth/session indirection
   * cannot forward cookies through a server-side fetch.
   */
  token?: string;
  /** Pass an AbortSignal for request cancellation. */
  signal?: AbortSignal;
}

async function getToken(serverSide: boolean): Promise<string | null> {
  if (serverSide) {
    // Server components should pass the session access token directly
    // via the `token` option on each call. The historical serverSide
    // path fetched /api/auth/session but Next.js server-side fetches
    // do NOT forward cookies by default, so that path returned null.
    // The cleanest fix is explicit forwarding from the caller, not
    // implicit cookie magic.
    return null;
  }
  return _accessToken;
}

async function apiFetch<T>(
  path: string,
  options: FetchOptions = {},
  retried = false,
): Promise<T> {
  const {
    body,
    serverSide = false,
    token: explicitToken,
    signal,
    ...init
  } = options;

  const token = explicitToken ?? (await getToken(serverSide));

  const headers: Record<string, string> = {
    Accept: "application/json",
    traceparent: buildTraceparent(),
    ...(init.headers as Record<string, string> | undefined),
  };

  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }

  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
  }

  let response: Response;
  try {
    response = await fetch(`${API_BASE}${path}`, {
      ...init,
      headers,
      body: body !== undefined ? JSON.stringify(body) : undefined,
      signal,
    });
  } catch (err) {
    throw new NetworkError(err);
  }

  // 401 — attempt silent token refresh once
  if (response.status === 401 && !retried && !serverSide) {
    const refreshed = await attemptSilentRefresh();
    if (refreshed) {
      return apiFetch<T>(path, options, true);
    }
    // Redirect to login
    if (typeof window !== "undefined") {
      window.location.href = "/tr/login";
    }
  }

  // No content
  if (response.status === 204) {
    return undefined as T;
  }

  const contentType = response.headers.get("Content-Type") ?? "";

  if (!response.ok) {
    // RFC 7807 error
    if (contentType.includes("application/problem+json")) {
      const problem = (await response.json()) as ProblemDetail;
      throw new ApiError(response.status, problem);
    }
    // Fallback
    throw new ApiError(response.status, {
      type: "about:blank",
      title: `HTTP ${response.status}`,
      status: response.status,
      detail: await response.text().catch(() => undefined),
    });
  }

  if (contentType.includes("application/json")) {
    return response.json() as Promise<T>;
  }

  // Redirect or other non-JSON (e.g. 302 presigned download)
  return response as unknown as T;
}

// ── Convenience methods ───────────────────────────────────────────────────────

export const apiClient = {
  get: <T>(path: string, opts?: FetchOptions & { token?: string }) =>
    apiFetch<T>(path, { ...opts, method: "GET" }),

  post: <T>(path: string, body?: unknown, opts?: FetchOptions) =>
    apiFetch<T>(path, { ...opts, method: "POST", body }),

  patch: <T>(path: string, body?: unknown, opts?: FetchOptions) =>
    apiFetch<T>(path, { ...opts, method: "PATCH", body }),

  delete: <T = void>(path: string, opts?: FetchOptions) =>
    apiFetch<T>(path, { ...opts, method: "DELETE" }),

  /**
   * Build a query string from an object, omitting undefined/null values.
   */
  buildQuery: <T extends object>(params: T): string => {
    const qs = new URLSearchParams();
    for (const [key, value] of Object.entries(params as Record<string, unknown>)) {
      if (value !== undefined && value !== null && value !== "") {
        qs.set(key, String(value));
      }
    }
    const str = qs.toString();
    return str ? `?${str}` : "";
  },
} as const;
