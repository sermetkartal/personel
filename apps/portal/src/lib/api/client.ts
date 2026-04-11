/**
 * API client scoped exclusively to /v1/me/* endpoints.
 * The employee transparency portal ONLY calls its own data endpoints.
 * No admin endpoints are accessible from this client.
 */

import { ApiError, type ApiProblem } from "./types";

const API_BASE_URL =
  process.env["PERSONEL_API_BASE_URL"] ?? "http://localhost:8080";

export interface RequestOptions {
  method?: "GET" | "POST" | "PATCH" | "DELETE";
  body?: unknown;
  accessToken: string;
  signal?: AbortSignal;
}

async function request<T>(
  path: string,
  options: RequestOptions
): Promise<T> {
  const { method = "GET", body, accessToken, signal } = options;

  // Enforce scope: only /v1/me/* and /v1/system/dlp-state are allowed
  if (!path.startsWith("/v1/me") && !path.startsWith("/v1/system/dlp-state")) {
    throw new Error(
      `[portal-api-client] Access denied: path "${path}" is not in the allowed employee scope.`
    );
  }

  const headers: HeadersInit = {
    "Content-Type": "application/json",
    Accept: "application/json",
    Authorization: `Bearer ${accessToken}`,
  };

  const response = await fetch(`${API_BASE_URL}${path}`, {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
    signal,
    // No credentials mode — Bearer token only; this is a server-to-server call
    cache: "no-store",
  });

  if (!response.ok) {
    let problem: ApiProblem;
    try {
      problem = (await response.json()) as ApiProblem;
    } catch {
      problem = { status: response.status, detail: response.statusText };
    }
    throw new ApiError(problem);
  }

  // Handle 204 No Content
  if (response.status === 204) {
    return undefined as T;
  }

  return response.json() as Promise<T>;
}

export const apiClient = {
  get<T>(path: string, accessToken: string, signal?: AbortSignal): Promise<T> {
    return request<T>(path, { accessToken, signal });
  },

  post<T>(path: string, body: unknown, accessToken: string): Promise<T> {
    return request<T>(path, { method: "POST", body, accessToken });
  },
};
