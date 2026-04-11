/**
 * Fetch wrapper for Personel Mobile Admin.
 * - Adds Bearer token from zustand session store
 * - Parses RFC 7807 error responses
 * - Automatic session refresh on 401 (single retry)
 * - Points at mobile-bff base URL from app.config.ts extras
 */

import Constants from "expo-constants";
import { useSessionStore } from "@/lib/auth/session";
import { ApiError, type ProblemDetail } from "@/lib/api/types";

const getBffUrl = (): string => {
  const url =
    (Constants.expoConfig?.extra as Record<string, unknown> | undefined)
      ?.mobileBffUrl;
  if (typeof url !== "string" || !url) {
    throw new Error(
      "MOBILE_BFF_URL is not configured in app.config.ts extras",
    );
  }
  return url.replace(/\/$/, "");
};

type RequestMethod = "GET" | "POST" | "PATCH" | "DELETE" | "PUT";

interface FetchOptions {
  method?: RequestMethod;
  body?: unknown;
  signal?: AbortSignal;
  skipAuthRetry?: boolean;
}

async function parseResponseBody<T>(response: Response): Promise<T> {
  const contentType = response.headers.get("content-type") ?? "";

  if (contentType.includes("application/problem+json")) {
    const problem: ProblemDetail = await response.json() as ProblemDetail;
    throw new ApiError(response.status, problem);
  }

  if (!response.ok) {
    let problem: ProblemDetail;
    try {
      problem = await response.json() as ProblemDetail;
    } catch {
      problem = {
        title: `HTTP ${response.status}`,
        status: response.status,
        detail: response.statusText,
      };
    }
    throw new ApiError(response.status, problem);
  }

  if (response.status === 204) {
    return undefined as T;
  }

  return response.json() as Promise<T>;
}

export async function apiFetch<T>(
  path: string,
  options: FetchOptions = {},
): Promise<T> {
  const { method = "GET", body, signal, skipAuthRetry = false } = options;
  const baseUrl = getBffUrl();
  const url = `${baseUrl}${path}`;

  const store = useSessionStore.getState();
  const token = store.accessToken;

  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    Accept: "application/json",
  };

  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }

  const response = await fetch(url, {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
    signal,
  });

  // Automatic token refresh on 401 — single retry only
  if (response.status === 401 && !skipAuthRetry) {
    const refreshed = await store.refreshTokens();
    if (refreshed) {
      const newToken = useSessionStore.getState().accessToken;
      const retryHeaders = {
        ...headers,
        Authorization: `Bearer ${newToken ?? ""}`,
      };
      const retryResponse = await fetch(url, {
        method,
        headers: retryHeaders,
        body: body !== undefined ? JSON.stringify(body) : undefined,
        signal,
      });
      return parseResponseBody<T>(retryResponse);
    } else {
      // Refresh failed — clear session so the root layout redirects to sign-in
      store.clearSession();
      throw new ApiError(401, {
        title: "Oturum sona erdi",
        status: 401,
        detail: "Oturumunuz sona erdi. Lütfen tekrar giriş yapın.",
      });
    }
  }

  return parseResponseBody<T>(response);
}

// Convenience wrappers

export const apiGet = <T>(
  path: string,
  signal?: AbortSignal,
): Promise<T> => apiFetch<T>(path, { method: "GET", signal });

export const apiPost = <T>(
  path: string,
  body?: unknown,
  signal?: AbortSignal,
): Promise<T> => apiFetch<T>(path, { method: "POST", body, signal });

export const apiPatch = <T>(
  path: string,
  body?: unknown,
  signal?: AbortSignal,
): Promise<T> => apiFetch<T>(path, { method: "PATCH", body, signal });
