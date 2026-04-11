"use client";

import { useState } from "react";
import Link from "next/link";
import { useLocale, useTranslations } from "next-intl";
import { Shield, AlertCircle } from "lucide-react";
import { toast } from "sonner";
import { acknowledgeFirstLoginNotification } from "@/lib/api/dlp-state";

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
  const tNav = useTranslations("nav");
  const locale = useLocale();

  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleAcknowledge(): Promise<void> {
    setIsSubmitting(true);
    setError(null);

    try {
      await acknowledgeFirstLoginNotification(accessToken);
      onAcknowledge();
    } catch {
      // If the API call fails, we still allow the user to proceed
      // but log the failure. KVKK defensibility note: the modal was shown;
      // network failure is not the employee's fault.
      console.error("[first-login-modal] Failed to record acknowledgement to audit API");
      toast.error(
        "Onay kaydedilemedi — ancak devam edebilirsiniz. BT desteğini bilgilendirin.",
        { duration: 8000 }
      );
      // Allow proceeding even if audit write fails — UX over failure
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
        <div className="px-8 py-6 space-y-4">
          <p
            id="first-login-desc"
            className="text-sm text-warm-700 leading-relaxed"
          >
            {t("body")}
          </p>

          {/* Key points */}
          <ul className="space-y-2 text-sm text-warm-700" role="list">
            {[
              "Uygulama kullanımı, dosya olayları ve ağ etkinliği kaydedilmektedir.",
              "Klavye içeriği şifreli olarak saklanır; hiçbir yönetici okuyamaz.",
              "Canlı ekran izleme için İK onayı zorunludur; oturumlar bu portalde görünür.",
              "Tüm veriler şirketinizin kendi sunucularında kalır; dışarı aktarılmaz.",
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

          {/* Link to full text */}
          <div className="pt-2">
            <Link
              href={`/${locale}/aydinlatma`}
              target="_blank"
              rel="noopener noreferrer"
              className="text-sm text-portal-600 hover:text-portal-800 underline-offset-2 hover:underline"
            >
              {t("readFull")} →
            </Link>
          </div>
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
            disabled={isSubmitting}
            className="w-full bg-portal-600 hover:bg-portal-700 disabled:bg-portal-300 text-white font-medium py-3 px-6 rounded-xl transition-colors duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-portal-600 focus-visible:ring-offset-2 text-sm"
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
