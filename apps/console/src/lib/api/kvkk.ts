/**
 * KVKK compliance bundle API query functions.
 *
 * Covers VERBİS registration, aydınlatma (privacy notice) publication,
 * DPA (Data Processing Agreement) signed document upload, DPIA amendment
 * upload, and explicit consent records (ADR 0013 DLP opt-in, etc.).
 *
 * See apps/api/internal/kvkk/* for the backend implementation and
 * docs/compliance/kvkk-framework.md for the legal framing.
 */

import { apiClient, ApiError, NetworkError } from "./client";

const API_BASE =
  process.env.NEXT_PUBLIC_API_BASE_URL ?? "http://localhost:8080";

// ── Types ─────────────────────────────────────────────────────────────────────

export interface VerbisInfo {
  registration_number: string;
  registered_at: string; // RFC3339
}

export interface AydinlatmaInfo {
  markdown: string;
  published_at: string; // RFC3339
  version: number;
}

export interface DpaSignatory {
  name: string;
  role: string;
  organization: string;
  signed_at: string; // RFC3339
}

export interface DpaInfo {
  signed_at: string; // RFC3339
  document_key: string;
  document_sha256: string;
  signatories: DpaSignatory[];
}

export interface DpiaInfo {
  amendment_key: string;
  amendment_sha256: string;
  completed_at: string; // RFC3339
}

/**
 * Enumerated consent types accepted by the backend.
 * Keep in sync with apps/api/internal/kvkk/consent.go AllowedConsentTypes.
 */
export const AllowedConsentType = [
  "dlp",
  "live_view_recording",
  "screen_capture_high_freq",
  "cross_department_transfer",
] as const;

export type ConsentType = (typeof AllowedConsentType)[number];

export interface ConsentRecord {
  id: string;
  user_id: string;
  consent_type: ConsentType;
  signed_at: string; // RFC3339
  revoked_at: string | null;
  document_key: string;
  document_sha256: string;
  created_at: string; // RFC3339
}

export interface ConsentList {
  items: ConsentRecord[];
}

// ── Request shapes ────────────────────────────────────────────────────────────

export interface UpdateVerbisRequest {
  registration_number: string;
  registered_at: string; // RFC3339
}

export interface PublishAydinlatmaRequest {
  markdown: string;
}

export interface UploadDpaRequest {
  file: File | Blob;
  signed_at: string; // RFC3339
  signatories: DpaSignatory[];
}

export interface UploadDpiaRequest {
  file: File | Blob;
  completed_at: string; // RFC3339
}

export interface RecordConsentRequest {
  user_id: string;
  consent_type: ConsentType;
  signed_at: string; // RFC3339
  document_base64: string;
}

// ── TanStack Query key factory ────────────────────────────────────────────────

export const kvkkKeys = {
  all: ["kvkk"] as const,
  verbis: ["kvkk", "verbis"] as const,
  aydinlatma: ["kvkk", "aydinlatma"] as const,
  dpa: ["kvkk", "dpa"] as const,
  dpia: ["kvkk", "dpia"] as const,
  consents: (type?: ConsentType) =>
    (type ? ["kvkk", "consents", type] : ["kvkk", "consents"]) as readonly [
      "kvkk",
      "consents",
      ConsentType?,
    ],
} as const;

// ── VERBİS ────────────────────────────────────────────────────────────────────

export async function getVerbis(
  opts: { token?: string; signal?: AbortSignal } = {},
): Promise<VerbisInfo> {
  return apiClient.get<VerbisInfo>("/v1/kvkk/verbis", opts);
}

export async function updateVerbis(
  req: UpdateVerbisRequest,
  opts: { token?: string; signal?: AbortSignal } = {},
): Promise<void> {
  return apiClient.patch<void>("/v1/kvkk/verbis", req, opts);
}

// ── Aydınlatma ────────────────────────────────────────────────────────────────

export async function getAydinlatma(
  opts: { token?: string; signal?: AbortSignal } = {},
): Promise<AydinlatmaInfo> {
  return apiClient.get<AydinlatmaInfo>("/v1/kvkk/aydinlatma", opts);
}

export async function publishAydinlatma(
  req: PublishAydinlatmaRequest,
  opts: { token?: string; signal?: AbortSignal } = {},
): Promise<AydinlatmaInfo> {
  return apiClient.post<AydinlatmaInfo>(
    "/v1/kvkk/aydinlatma/publish",
    req,
    opts,
  );
}

// ── DPA ───────────────────────────────────────────────────────────────────────

export async function getDpa(
  opts: { token?: string; signal?: AbortSignal } = {},
): Promise<DpaInfo> {
  return apiClient.get<DpaInfo>("/v1/kvkk/dpa", opts);
}

export async function uploadDpa(
  req: UploadDpaRequest,
  opts: { token?: string; signal?: AbortSignal } = {},
): Promise<DpaInfo> {
  const form = new FormData();
  form.append("file", req.file);
  form.append("signed_at", req.signed_at);
  form.append("signatories", JSON.stringify(req.signatories));
  return multipartUpload<DpaInfo>("/v1/kvkk/dpa/upload", form, opts);
}

// ── DPIA ──────────────────────────────────────────────────────────────────────

export async function getDpia(
  opts: { token?: string; signal?: AbortSignal } = {},
): Promise<DpiaInfo> {
  return apiClient.get<DpiaInfo>("/v1/kvkk/dpia", opts);
}

export async function uploadDpia(
  req: UploadDpiaRequest,
  opts: { token?: string; signal?: AbortSignal } = {},
): Promise<DpiaInfo> {
  const form = new FormData();
  form.append("file", req.file);
  form.append("completed_at", req.completed_at);
  return multipartUpload<DpiaInfo>("/v1/kvkk/dpia/upload", form, opts);
}

// ── Consents ──────────────────────────────────────────────────────────────────

export async function listConsents(
  type?: ConsentType,
  opts: { token?: string; signal?: AbortSignal } = {},
): Promise<ConsentList> {
  const qs = type ? apiClient.buildQuery({ type }) : "";
  return apiClient.get<ConsentList>(`/v1/kvkk/consents${qs}`, opts);
}

export async function recordConsent(
  req: RecordConsentRequest,
  opts: { token?: string; signal?: AbortSignal } = {},
): Promise<ConsentRecord> {
  return apiClient.post<ConsentRecord>("/v1/kvkk/consents", req, opts);
}

export async function revokeConsent(
  userId: string,
  consentType: ConsentType,
  opts: { token?: string; signal?: AbortSignal } = {},
): Promise<void> {
  return apiClient.delete<void>(
    `/v1/kvkk/consents/${encodeURIComponent(userId)}/${encodeURIComponent(consentType)}`,
    opts,
  );
}

// ── Multipart helper ──────────────────────────────────────────────────────────

/**
 * Performs a multipart/form-data POST against the admin API.
 *
 * Notes:
 * - We deliberately do NOT set Content-Type; the browser/fetch runtime
 *   injects the correct multipart boundary when `body` is a FormData.
 * - Bearer token must be passed explicitly via opts.token for server-side
 *   callers; browser-side callers rely on the client module's in-memory
 *   token store via a subsequent apiClient call rather than bypassing it.
 *   Since this helper goes around the apiClient to preserve the FormData
 *   body, callers in Server Components must forward their session token.
 */
async function multipartUpload<T>(
  path: string,
  form: FormData,
  opts: { token?: string; signal?: AbortSignal } = {},
): Promise<T> {
  const headers: Record<string, string> = {
    Accept: "application/json",
  };
  if (opts.token) {
    headers["Authorization"] = `Bearer ${opts.token}`;
  }

  let response: Response;
  try {
    response = await fetch(`${API_BASE}${path}`, {
      method: "POST",
      headers,
      body: form,
      signal: opts.signal,
    });
  } catch (err) {
    throw new NetworkError(err);
  }

  if (response.status === 204) {
    return undefined as T;
  }

  const contentType = response.headers.get("Content-Type") ?? "";

  if (!response.ok) {
    if (contentType.includes("application/problem+json")) {
      const problem = await response.json();
      throw new ApiError(response.status, problem);
    }
    throw new ApiError(response.status, {
      type: "about:blank",
      title: `HTTP ${response.status}`,
      status: response.status,
      detail: await response.text().catch(() => undefined),
    });
  }

  if (contentType.includes("application/json")) {
    return (await response.json()) as T;
  }

  return undefined as T;
}
