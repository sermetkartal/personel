/**
 * Typed error classes for the Personel console.
 * Maps API RFC 7807 responses to actionable UI error states.
 */

import { ApiError } from "./api/client";

export type ErrorSeverity = "info" | "warning" | "error" | "critical";

export interface UserFacingError {
  title: string;
  description: string;
  severity: ErrorSeverity;
  code?: string;
  retryable: boolean;
}

/**
 * Convert any caught error into a user-facing error object.
 * Uses the Turkish detail from RFC 7807 if available.
 */
export function toUserFacingError(err: unknown): UserFacingError {
  if (err instanceof ApiError) {
    const { problem, status } = err;

    if (status === 401) {
      return {
        title: "Oturum Sona Erdi",
        description: "Oturumunuz sona erdi. Lütfen tekrar giriş yapın.",
        severity: "warning",
        code: problem.code,
        retryable: false,
      };
    }

    if (status === 403) {
      return {
        title: "Erişim Reddedildi",
        description:
          problem.detail ?? "Bu işlemi gerçekleştirme yetkiniz yok.",
        severity: "error",
        code: problem.code,
        retryable: false,
      };
    }

    if (status === 404) {
      return {
        title: "Kaynak Bulunamadı",
        description: problem.detail ?? "Aranan kaynak mevcut değil.",
        severity: "info",
        code: problem.code,
        retryable: false,
      };
    }

    if (status === 409) {
      return {
        title: "Çakışma",
        description:
          problem.detail ?? "Bu kaynak zaten mevcut veya çakışma var.",
        severity: "warning",
        code: problem.code,
        retryable: false,
      };
    }

    if (status === 400) {
      const fieldErrors = problem.errors
        ?.map((e) => `${e.field}: ${e.message ?? e.code}`)
        .join(", ");
      return {
        title: "Doğrulama Hatası",
        description: fieldErrors ?? problem.detail ?? "İstek doğrulanamadı.",
        severity: "warning",
        code: problem.code,
        retryable: false,
      };
    }

    if (status >= 500) {
      return {
        title: "Sunucu Hatası",
        description:
          problem.detail ??
          "Sunucu hatası oluştu. Sistem yöneticinize bildirin.",
        severity: "critical",
        code: problem.code,
        retryable: true,
      };
    }

    return {
      title: problem.title,
      description: problem.detail ?? "Beklenmeyen bir hata oluştu.",
      severity: "error",
      code: problem.code,
      retryable: status < 500,
    };
  }

  if (err instanceof Error) {
    if (err.name === "NetworkError") {
      return {
        title: "Bağlantı Hatası",
        description:
          "Sunucuya bağlanılamıyor. Ağ bağlantınızı kontrol edin.",
        severity: "error",
        retryable: true,
      };
    }

    return {
      title: "Beklenmeyen Hata",
      description: err.message,
      severity: "error",
      retryable: false,
    };
  }

  return {
    title: "Beklenmeyen Hata",
    description: "Bilinmeyen bir hata oluştu.",
    severity: "error",
    retryable: false,
  };
}

/**
 * Type guard: is this an audit failure error?
 * Audit failures must block the operation.
 */
export function isAuditFailure(err: unknown): boolean {
  if (err instanceof ApiError) {
    return err.problem.code === "err.audit_failed";
  }
  return false;
}
