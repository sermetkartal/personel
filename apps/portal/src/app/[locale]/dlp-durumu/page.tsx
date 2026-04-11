import { redirect } from "next/navigation";
import { getTranslations, getLocale } from "next-intl/server";
import { KeyRound, Lock, ShieldAlert, Info } from "lucide-react";
import Link from "next/link";
import { getSession } from "@/lib/auth/session";
import { getDLPState } from "@/lib/api/dlp-state";
import { LegalTerm } from "@/components/common/legal-term";
import { formatDate } from "@/lib/utils";
import type { DLPStateResponse } from "@/lib/api/types";
import { cn } from "@/lib/utils";

export async function generateMetadata(): Promise<{ title: string }> {
  const t = await getTranslations("dlpDurumu");
  return { title: t("title") };
}

/**
 * DLP state explainer page — per ADR 0013.
 *
 * Default state (OFF): neutral tone, explains the cryptographic guarantee
 * that even though data is captured, no process holds the decryption key.
 *
 * Enabled state: amber tone, factual activation date, ceremony reference,
 * explains that only an isolated automated process reads the data.
 *
 * This page is referenced from the DLP banner on every page.
 */
export default async function DlpDurumuPage(): Promise<JSX.Element> {
  const session = await getSession();
  const locale = await getLocale() as "tr" | "en";

  if (!session) redirect(`/${locale}/giris`);

  const t = await getTranslations("dlpDurumu");
  const dpoEmail = process.env["NEXT_PUBLIC_DPO_EMAIL"] ?? "kvkk@musteri.com.tr";

  // Fetch DLP state — default to "disabled" if API unavailable
  let dlpState: DLPStateResponse = { status: "disabled" };
  try {
    dlpState = await getDLPState(session.accessToken);
  } catch {
    // API endpoint may not yet exist (flagged as missing in openapi.yaml)
    // Default to disabled — safer assumption aligned with ADR 0013
    dlpState = { status: "disabled" };
  }

  const isEnabled = dlpState.status === "enabled";
  const enabledAt = dlpState.enabled_at
    ? formatDate(dlpState.enabled_at, locale)
    : null;

  return (
    <div className="space-y-8 animate-fade-in max-w-2xl">
      <header className="page-header">
        <div className="flex items-center gap-3 mb-2">
          <div
            className={cn(
              "w-10 h-10 rounded-xl flex items-center justify-center",
              isEnabled ? "bg-dlp-100" : "bg-portal-100"
            )}
            aria-hidden="true"
          >
            {isEnabled ? (
              <ShieldAlert className="w-5 h-5 text-dlp-600" />
            ) : (
              <Lock className="w-5 h-5 text-portal-600" />
            )}
          </div>
          <h1>{t("title")}</h1>
        </div>
        <p className="text-warm-600">{t("subtitle")}</p>
      </header>

      {/* Current state banner */}
      <div
        className={cn(
          "rounded-xl border px-5 py-4",
          isEnabled
            ? "bg-dlp-50 border-dlp-200"
            : "bg-warm-100 border-warm-300"
        )}
        role="status"
        aria-live="polite"
      >
        <div className="flex items-center gap-3">
          <KeyRound
            className={cn(
              "w-5 h-5 flex-shrink-0",
              isEnabled ? "text-dlp-600" : "text-warm-500"
            )}
            aria-hidden="true"
          />
          <div>
            <span
              className={cn(
                "text-xs font-semibold uppercase tracking-wide block mb-0.5",
                isEnabled ? "text-dlp-700" : "text-warm-500"
              )}
            >
              {t("stateLabel")}
            </span>
            <span
              className={cn(
                "font-semibold text-base",
                isEnabled ? "text-dlp-800" : "text-warm-800"
              )}
            >
              {isEnabled ? t("stateOn") : t("stateOff")}
            </span>
            {isEnabled && enabledAt && (
              <span className="block text-sm text-dlp-600 mt-0.5">
                {t("onActivatedOn")}: <strong>{enabledAt}</strong>
              </span>
            )}
            {isEnabled && dlpState.ceremony_reference && (
              <span className="block text-sm text-dlp-600">
                {t("onCeremonyRef")}: <code className="font-mono text-xs">{dlpState.ceremony_reference}</code>
              </span>
            )}
          </div>
        </div>
      </div>

      {/* State-specific explanation */}
      {!isEnabled ? (
        <section className="space-y-4" aria-labelledby="off-explanation">
          <h2 id="off-explanation" className="text-lg font-semibold text-warm-900">
            {t("offTitle")}
          </h2>
          <p className="text-warm-600 leading-relaxed">{t("offExplanation")}</p>

          <div className="rounded-xl bg-portal-50 border border-portal-100 px-4 py-3 flex items-start gap-3">
            <Info className="w-4 h-4 text-portal-400 flex-shrink-0 mt-0.5" aria-hidden="true" />
            <p className="text-sm text-portal-700 leading-relaxed">{t("offTechnical")}</p>
          </div>

          <div className="space-y-2">
            <h3 className="text-sm font-semibold text-warm-800">{t("offActivation")}</h3>
            <p className="text-sm text-warm-600 leading-relaxed">{t("offActivationText")}</p>
          </div>
        </section>
      ) : (
        <section className="space-y-4" aria-labelledby="on-explanation">
          <h2 id="on-explanation" className="text-lg font-semibold text-warm-900">
            {t("onTitle")}
          </h2>
          <p className="text-warm-600 leading-relaxed">{t("onExplanation")}</p>

          <div className="rounded-xl bg-trust-50 border border-trust-200 px-4 py-3">
            <p className="text-sm text-trust-700 leading-relaxed">
              <strong>Önemli:</strong> {t("onHamIcerik")}
            </p>
          </div>
        </section>
      )}

      {/* What is encrypted — always shown */}
      <section className="card" aria-labelledby="encrypted-info">
        <h2 id="encrypted-info" className="text-base font-semibold text-warm-800 mb-3">
          {t("whatIsEncrypted")}
        </h2>
        <p className="text-sm text-warm-600 leading-relaxed mb-3">
          {t("whatIsEncryptedText")}
        </p>
        <p className="text-xs text-warm-400 italic">{t("retentionNote")}</p>
      </section>

      {/* KVKK legal context */}
      <section className="card bg-warm-50" aria-label="Hukuki bağlam">
        <p className="text-sm text-warm-600 leading-relaxed">
          Klavye verilerinin işlenmesi,{" "}
          <LegalTerm termKey="meruMenfaat">meşru menfaat</LegalTerm> (
          <LegalTerm termKey="m5f">m.5/2-f</LegalTerm>) hukuki sebebine
          dayanmaktadır. DLP özelliği etkinleştirilmeden önce müşteri kurumun bir{" "}
          <LegalTerm termKey="dpia">DPIA</LegalTerm> yapması ve{" "}
          <LegalTerm termKey="dpo">DPO</LegalTerm> onayı alması zorunludur.
        </p>
      </section>

      {/* Contact */}
      <footer className="text-sm text-warm-500">
        {t("moreInfo")}:{" "}
        <a
          href={`mailto:${dpoEmail}`}
          className="text-portal-600 hover:underline underline-offset-2"
        >
          {t("contactDpo")}
        </a>{" "}
        veya{" "}
        <Link href={`/${locale}/iletisim`} className="text-portal-600 hover:underline underline-offset-2">
          İletişim sayfası
        </Link>
      </footer>
    </div>
  );
}
