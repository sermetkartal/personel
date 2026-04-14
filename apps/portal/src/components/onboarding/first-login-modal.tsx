"use client";

import { useState } from "react";
import Link from "next/link";
import { useLocale, useTranslations } from "next-intl";
import {
  Shield,
  AlertCircle,
  Eye,
  Lock,
  Server,
  FileText,
  Scale,
} from "lucide-react";
import { toast } from "sonner";
import { acknowledgeFirstLoginNotification } from "@/lib/api/dlp-state";
import { acknowledgeAydinlatma } from "@/lib/api/transparency";

interface FirstLoginModalProps {
  /**
   * The access token is needed to POST the acknowledgement to the audit API.
   */
  accessToken: string;
  /**
   * Callback invoked after successful acknowledgement.
   * The parent layout should set a cookie/state so the modal is not shown again.
   */
  onAcknowledge: () => void;
}

/**
 * Legally mandatory first-login notification modal.
 *
 * Per calisan-bilgilendirme-akisi.md Aşama 5:
 * - Full-screen, cannot be dismissed without clicking "Anladım"
 * - The click event is written to the hash-chained audit log via the API
 * - Shows the aydınlatma summary and link to the full text
 *
 * Design: calm, clear, NOT alarming. The employee should feel informed, not surveilled.
 */
export function FirstLoginModal({
  accessToken,
  onAcknowledge,
}: FirstLoginModalProps): JSX.Element {
  const t = useTranslations("firstLoginModal");
  const locale = useLocale();

  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [acknowledged, setAcknowledged] = useState(false);

  async function handleAcknowledge(): Promise<void> {
    if (!acknowledged) {
      setError(t("mustConfirm"));
      return;
    }

    setIsSubmitting(true);
    setError(null);

    try {
      // Dual write: the legacy /v1/me/acknowledge-notification endpoint
      // (first-login marker) AND the newer /v1/transparency/acknowledge
      // (versioned aydınlatma metni). Either one scaffolding to 404 is
      // tolerated — the other still records the acceptance.
      await Promise.allSettled([
        acknowledgeFirstLoginNotification(accessToken),
        acknowledgeAydinlatma(accessToken, "1.0.0", (locale as "tr" | "en")),
      ]);
      onAcknowledge();
    } catch {
      // If BOTH fail the browser already surfaced an error — we still let
      // the user proceed. KVKK defensibility: the modal was shown; network
      // failure is not the employee's fault.
      console.error("[first-login-modal] Failed to record acknowledgement to audit API");
      toast.error(
        "Onay kaydedilemedi — ancak devam edebilirsiniz. BT desteğini bilgilendirin.",
        { duration: 8000 }
      );
      onAcknowledge();
    } finally {
      setIsSubmitting(false);
    }
  }

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="first-login-title"
      aria-describedby="first-login-desc"
      className="fixed inset-0 z-50 bg-warm-900/60 backdrop-blur-sm flex items-center justify-center p-4"
    >
      <div className="bg-white rounded-2xl shadow-2xl max-w-lg w-full max-h-[90vh] overflow-y-auto animate-slide-up">
        {/* Header */}
        <div className="px-8 pt-8 pb-6 border-b border-warm-100">
          <div className="flex items-start gap-4">
            <div className="w-10 h-10 rounded-xl bg-portal-100 flex items-center justify-center flex-shrink-0">
              <Shield className="w-5 h-5 text-portal-600" aria-hidden="true" />
            </div>
            <div>
              <h1
                id="first-login-title"
                className="text-lg font-semibold text-warm-900 leading-snug"
              >
                {t("title")}
              </h1>
              <p className="mt-1 text-sm text-warm-500">{t("subtitle")}</p>
            </div>
          </div>
        </div>

        {/* Body */}
        <div className="px-8 py-6 space-y-5">
          <p
            id="first-login-desc"
            className="text-sm text-warm-700 leading-relaxed"
          >
            {t("body")}
          </p>

          {/* What is monitored — short list */}
          <section aria-labelledby="fl-monitored">
            <h3
              id="fl-monitored"
              className="text-xs font-semibold text-warm-800 uppercase tracking-wide mb-2 flex items-center gap-1.5"
            >
              <Eye className="w-3.5 h-3.5 text-portal-600" aria-hidden="true" />
              {t("monitoredTitle")}
            </h3>
            <ul className="space-y-1.5 text-sm text-warm-700" role="list">
              {[
                t("point1"),
                t("point2"),
                t("point3"),
                t("point4"),
              ].map((point) => (
                <li key={point} className="flex items-start gap-2">
                  <span
                    className="mt-0.5 w-4 h-4 rounded-full bg-trust-100 text-trust-600 flex items-center justify-center text-xs flex-shrink-0"
                    aria-hidden="true"
                  >
                    ✓
                  </span>
                  {point}
                </li>
              ))}
            </ul>
          </section>

          {/* KVKK rights — 1-liner each */}
          <section aria-labelledby="fl-rights">
            <h3
              id="fl-rights"
              className="text-xs font-semibold text-warm-800 uppercase tracking-wide mb-2 flex items-center gap-1.5"
            >
              <Scale className="w-3.5 h-3.5 text-portal-600" aria-hidden="true" />
              {t("rightsTitle")}
            </h3>
            <p className="text-xs text-warm-600 leading-relaxed">
              {t("rightsLine")}{" "}
              <Link
                href={`/${locale}/haklar`}
                target="_blank"
                rel="noopener noreferrer"
                className="text-portal-600 underline-offset-2 hover:underline"
              >
                {t("rightsLink")} →
              </Link>
            </p>
          </section>

          {/* DLP status — ADR 0013 */}
          <section aria-labelledby="fl-dlp">
            <h3
              id="fl-dlp"
              className="text-xs font-semibold text-warm-800 uppercase tracking-wide mb-2 flex items-center gap-1.5"
            >
              <Lock className="w-3.5 h-3.5 text-portal-600" aria-hidden="true" />
              {t("dlpTitle")}
            </h3>
            <p className="text-xs text-warm-600 leading-relaxed">
              {t("dlpLine")}
            </p>
          </section>

          {/* On-prem guarantee */}
          <section aria-labelledby="fl-onprem">
            <h3
              id="fl-onprem"
              className="text-xs font-semibold text-warm-800 uppercase tracking-wide mb-2 flex items-center gap-1.5"
            >
              <Server className="w-3.5 h-3.5 text-portal-600" aria-hidden="true" />
              {t("onpremTitle")}
            </h3>
            <p className="text-xs text-warm-600 leading-relaxed">
              {t("onpremLine")}
            </p>
          </section>

          {/* Link to full text */}
          <div className="pt-1 flex items-center gap-2">
            <FileText className="w-4 h-4 text-portal-600" aria-hidden="true" />
            <Link
              href={`/${locale}/aydinlatma`}
              target="_blank"
              rel="noopener noreferrer"
              className="text-sm text-portal-600 hover:text-portal-800 underline-offset-2 hover:underline font-medium"
            >
              {t("readFull")} →
            </Link>
          </div>

          {/* Mandatory acknowledgement checkbox */}
          <label
            className="flex items-start gap-3 cursor-pointer group pt-3 border-t border-warm-100"
            htmlFor="fl-ack-checkbox"
          >
            <input
              id="fl-ack-checkbox"
              type="checkbox"
              checked={acknowledged}
              onChange={(e) => {
                setAcknowledged(e.target.checked);
                if (e.target.checked) setError(null);
              }}
              className="mt-0.5 w-4 h-4 rounded border-warm-300 text-portal-600 focus:ring-portal-500 cursor-pointer flex-shrink-0"
              aria-describedby="fl-ack-label"
            />
            <span
              id="fl-ack-label"
              className="text-sm text-warm-700 leading-relaxed group-hover:text-warm-900"
            >
              {t("checkboxLabel")}
            </span>
          </label>
        </div>

        {/* Footer */}
        <div className="px-8 pb-8 pt-2 space-y-3">
          {error && (
            <div
              role="alert"
              className="flex items-center gap-2 text-sm text-red-700 bg-red-50 border border-red-200 rounded-lg px-3 py-2"
            >
              <AlertCircle className="w-4 h-4 flex-shrink-0" aria-hidden="true" />
              {error}
            </div>
          )}

          <button
            onClick={handleAcknowledge}
            disabled={isSubmitting || !acknowledged}
            className="w-full bg-portal-600 hover:bg-portal-700 disabled:bg-portal-300 disabled:cursor-not-allowed text-white font-medium py-3 px-6 rounded-xl transition-colors duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-portal-600 focus-visible:ring-offset-2 text-sm"
            aria-busy={isSubmitting}
          >
            {isSubmitting ? "Kaydediliyor..." : t("confirmButton")}
          </button>

          <p className="text-xs text-warm-400 text-center leading-relaxed">
            {t("confirmNote")}
          </p>
        </div>
      </div>
    </div>
  );
}
