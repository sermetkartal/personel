"use client";

import { useState } from "react";
import { useLocale, useTranslations } from "next-intl";
import { CheckCircle2, Download, AlertCircle, FileText } from "lucide-react";
import { toast } from "sonner";
import { acknowledgeAydinlatma, aydinlatmaPdfUrl } from "@/lib/api/transparency";

interface AcknowledgePanelProps {
  accessToken: string;
  version: string;
  lastUpdated: string;
  /** True if the employee has already acknowledged this version */
  alreadyAcknowledged: boolean;
}

/**
 * Client-side panel that sits under the aydınlatma metni and offers:
 *   1. A PDF download button (scaffold — toasts "yakında" if backend 404s)
 *   2. A "Kabul Ediyorum" checkbox + submit button that POSTs to
 *      /v1/transparency/acknowledge and writes to the hash-chained audit log.
 *
 * Per KVKK framework, the acknowledgement is NOT a consent (m.5/2-a) — the
 * legal bases remain m.5/2-c and m.5/2-f. This panel only records that the
 * employee has been shown + read the disclosure, as required by m.10.
 */
export function AcknowledgePanel({
  accessToken,
  version,
  lastUpdated,
  alreadyAcknowledged,
}: AcknowledgePanelProps): JSX.Element {
  const t = useTranslations("aydinlatma");
  const locale = useLocale() as "tr" | "en";

  const [checked, setChecked] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [ack, setAck] = useState(alreadyAcknowledged);
  const [error, setError] = useState<string | null>(null);

  async function handleDownloadPdf(): Promise<void> {
    const url = aydinlatmaPdfUrl(locale, version);
    // The scaffold endpoint does not yet exist. We HEAD it and fall back to
    // a friendly toast. We intentionally do NOT attempt a bearer-token
    // download here — the server must implement a signed-URL issuance flow
    // first (ADR-style TODO).
    try {
      const resp = await fetch(url, {
        method: "HEAD",
        headers: { Authorization: `Bearer ${accessToken}` },
      });
      if (resp.ok) {
        window.open(url, "_blank", "noopener,noreferrer");
      } else {
        toast.info(t("pdfSoonTitle"), {
          description: t("pdfSoonDesc"),
          duration: 5000,
        });
      }
    } catch {
      toast.info(t("pdfSoonTitle"), {
        description: t("pdfSoonDesc"),
        duration: 5000,
      });
    }
  }

  async function handleAcknowledge(): Promise<void> {
    if (!checked) return;
    setSubmitting(true);
    setError(null);
    try {
      const result = await acknowledgeAydinlatma(accessToken, version, locale);
      setAck(true);
      if (result) {
        toast.success(t("ackSuccessTitle"), {
          description: t("ackSuccessDesc"),
          duration: 4000,
        });
      } else {
        // 404 scaffold — local state is updated so UX doesn't block
        toast.success(t("ackSuccessTitle"), {
          description: t("ackLocalOnly"),
          duration: 5000,
        });
      }
    } catch (err) {
      const message =
        err instanceof Error ? err.message : t("ackFailed");
      setError(message);
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <section
      aria-labelledby="ack-heading"
      className="card mt-6 border-portal-200 bg-portal-50/40"
    >
      <div className="flex items-start gap-3 mb-4">
        <FileText
          className="w-5 h-5 text-portal-600 mt-0.5 flex-shrink-0"
          aria-hidden="true"
        />
        <div className="flex-1">
          <h2
            id="ack-heading"
            className="text-base font-semibold text-warm-900"
          >
            {t("ackHeading")}
          </h2>
          <p className="text-xs text-warm-500 mt-1">
            {t("versionLabel")}: <span className="font-mono">{version}</span>
            {" · "}
            {t("lastUpdatedLabel")}: {lastUpdated}
          </p>
        </div>
      </div>

      <p className="text-sm text-warm-700 leading-relaxed mb-4">
        {t("ackExplain")}
      </p>

      {/* PDF download */}
      <div className="mb-5">
        <button
          type="button"
          onClick={handleDownloadPdf}
          className="inline-flex items-center gap-2 bg-white hover:bg-portal-50 border border-portal-200 text-portal-700 font-medium py-2 px-4 rounded-xl text-sm transition-colors"
          aria-label={t("downloadPdf")}
        >
          <Download className="w-4 h-4" aria-hidden="true" />
          {t("downloadPdf")}
        </button>
      </div>

      {/* Acknowledge */}
      {ack ? (
        <div className="flex items-start gap-3 text-sm text-trust-700 bg-trust-50 border border-trust-200 rounded-xl px-4 py-3">
          <CheckCircle2
            className="w-5 h-5 text-trust-500 flex-shrink-0 mt-0.5"
            aria-hidden="true"
          />
          <div>
            <p className="font-medium">{t("ackAlreadyTitle")}</p>
            <p className="text-xs text-trust-600 mt-1">
              {t("ackAlreadyDesc", { version })}
            </p>
          </div>
        </div>
      ) : (
        <div className="space-y-3">
          <label className="flex items-start gap-3 cursor-pointer group">
            <input
              type="checkbox"
              checked={checked}
              onChange={(e) => setChecked(e.target.checked)}
              className="mt-0.5 w-4 h-4 rounded border-warm-300 text-portal-600 focus:ring-portal-500 cursor-pointer"
              aria-describedby="ack-checkbox-desc"
            />
            <span
              id="ack-checkbox-desc"
              className="text-sm text-warm-700 leading-relaxed group-hover:text-warm-900"
            >
              {t("ackCheckboxLabel")}
            </span>
          </label>

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
            type="button"
            onClick={handleAcknowledge}
            disabled={!checked || submitting}
            className="inline-flex items-center gap-2 bg-portal-600 hover:bg-portal-700 disabled:bg-portal-200 disabled:cursor-not-allowed text-white font-medium py-2.5 px-5 rounded-xl text-sm transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-portal-600 focus-visible:ring-offset-2"
            aria-busy={submitting}
          >
            {submitting ? t("ackSubmitting") : t("ackSubmit")}
          </button>

          <p className="text-xs text-warm-400 leading-relaxed">
            {t("ackAuditNote")}
          </p>
        </div>
      )}
    </section>
  );
}
