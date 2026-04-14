import { redirect } from "next/navigation";
import { getTranslations, getLocale } from "next-intl/server";
import Link from "next/link";
import {
  Monitor,
  Lock,
  Users,
  ClipboardCheck,
  ArrowRight,
  ShieldCheck,
  HandMetal,
} from "lucide-react";
import { getSession } from "@/lib/auth/session";
import { ActiveSessionBanner } from "@/components/live-view/active-session-banner";

export async function generateMetadata(): Promise<{ title: string }> {
  const t = await getTranslations("canliIzleme");
  return { title: t("title") };
}

/**
 * Live view policy explainer page.
 * Explains what live view is, how the HR approval works, the guarantees,
 * and the employee's rights during a session.
 *
 * Tone: calm and factual. The system has this capability, here is how
 * it is governed, here is what protects you.
 */
export default async function CanliIzlemePage(): Promise<JSX.Element> {
  const session = await getSession();
  const locale = await getLocale();

  if (!session) redirect(`/${locale}/giris`);

  const t = await getTranslations("canliIzleme");

  return (
    <div className="space-y-8 animate-fade-in">
      {/* Active session banner — poll every 10s */}
      <ActiveSessionBanner accessToken={session.accessToken} />

      <header className="page-header">
        <div className="flex items-center gap-3 mb-2">
          <div
            className="w-10 h-10 rounded-xl bg-portal-100 flex items-center justify-center"
            aria-hidden="true"
          >
            <Monitor className="w-5 h-5 text-portal-600" />
          </div>
          <h1>{t("title")}</h1>
        </div>
        <p className="text-warm-600">{t("subtitle")}</p>
      </header>

      {/* What is it */}
      <section className="card" aria-labelledby="what-is-live-view">
        <h2 id="what-is-live-view" className="text-lg font-semibold text-warm-900 mb-3">
          {t("whatIsIt")}
        </h2>
        <p className="text-warm-600 leading-relaxed">{t("whatIsItText")}</p>
      </section>

      {/* How approval works */}
      <section aria-labelledby="approval-process">
        <h2 id="approval-process" className="text-base font-semibold text-warm-800 mb-4">
          {t("howApproval")}
        </h2>
        <p className="text-sm text-warm-600 mb-5">{t("howApprovalText")}</p>

        <ol className="space-y-4" role="list">
          {[
            { icon: Lock, step: "1", text: t("step1") },
            { icon: Users, step: "2", text: t("step2") },
            { icon: ShieldCheck, step: "3", text: t("step3") },
          ].map((item) => {
            const Icon = item.icon;
            return (
              <li key={item.step} className="flex items-start gap-4">
                <div
                  className="w-8 h-8 rounded-full bg-portal-600 text-white text-sm font-bold flex items-center justify-center flex-shrink-0"
                  aria-hidden="true"
                >
                  {item.step}
                </div>
                <div className="flex-1 pt-1">
                  <p className="text-sm text-warm-700 leading-relaxed">
                    <Icon
                      className="inline w-4 h-4 text-portal-400 mr-1.5 mb-0.5"
                      aria-hidden="true"
                    />
                    {item.text}
                  </p>
                </div>
              </li>
            );
          })}
        </ol>
      </section>

      {/* Guarantees */}
      <section aria-labelledby="guarantees">
        <h2 id="guarantees" className="text-base font-semibold text-warm-800 mb-4">
          {t("guarantees")}
        </h2>
        <ul className="space-y-3" role="list">
          {[
            t("guarantee1"),
            t("guarantee2"),
            t("guarantee3"),
            t("guarantee4"),
          ].map((guarantee, idx) => (
            <li
              key={idx}
              className="flex items-start gap-3 text-sm text-warm-700 leading-relaxed"
            >
              <ClipboardCheck
                className="w-4 h-4 text-trust-500 flex-shrink-0 mt-0.5"
                aria-hidden="true"
              />
              {guarantee}
            </li>
          ))}
        </ul>
      </section>

      {/* KVKK m.5 conditions section — when can your screen be observed */}
      <section className="card" aria-labelledby="kvkk-conditions">
        <h2
          id="kvkk-conditions"
          className="text-base font-semibold text-warm-800 mb-3"
        >
          {t("kvkkConditionsTitle")}
        </h2>
        <p className="text-sm text-warm-600 mb-3 leading-relaxed">
          {t("kvkkConditionsIntro")}
        </p>
        <ul className="space-y-2 text-sm text-warm-700" role="list">
          {[
            t("kvkkCondition1"),
            t("kvkkCondition2"),
            t("kvkkCondition3"),
            t("kvkkCondition4"),
          ].map((item, i) => (
            <li key={i} className="flex items-start gap-2">
              <span
                className="mt-0.5 w-4 h-4 rounded-full bg-portal-100 text-portal-600 flex items-center justify-center text-xs flex-shrink-0"
                aria-hidden="true"
              >
                {i + 1}
              </span>
              {item}
            </li>
          ))}
        </ul>
      </section>

      {/* Rights during session */}
      <section className="card bg-portal-50 border-portal-100" aria-labelledby="rights-during">
        <h2 id="rights-during" className="text-sm font-semibold text-portal-700 mb-2">
          {t("yourRightsDuring")}
        </h2>
        <p className="text-sm text-portal-600 leading-relaxed">
          {t("yourRightsDuringText")}
        </p>
      </section>

      {/* Right to object / refuse */}
      <section className="card border-l-4 border-l-trust-400" aria-labelledby="right-to-refuse">
        <div className="flex items-start gap-3 mb-2">
          <HandMetal
            className="w-5 h-5 text-trust-600 mt-0.5 flex-shrink-0"
            aria-hidden="true"
          />
          <h2
            id="right-to-refuse"
            className="text-base font-semibold text-warm-900"
          >
            {t("rightToRefuseTitle")}
          </h2>
        </div>
        <p className="text-sm text-warm-700 leading-relaxed mb-3">
          {t("rightToRefuseBody")}
        </p>
        <Link
          href={`/${locale}/haklar/yeni-basvuru?type=object`}
          className="inline-flex items-center gap-1.5 text-sm text-trust-700 font-medium hover:underline underline-offset-2"
        >
          {t("rightToRefuseAction")}
          <ArrowRight className="w-4 h-4" aria-hidden="true" />
        </Link>
      </section>

      {/* Visibility policy */}
      <section className="card" aria-labelledby="visibility-policy">
        <h2 id="visibility-policy" className="text-sm font-semibold text-warm-800 mb-2">
          {t("visibility")}
        </h2>
        <p className="text-sm text-warm-600 leading-relaxed">{t("visibilityText")}</p>
      </section>

      {/* CTA to session history */}
      <div className="flex">
        <Link
          href={`/${locale}/canli-izleme/oturum-gecmisi`}
          className="inline-flex items-center gap-2 bg-portal-600 hover:bg-portal-700 text-white font-medium py-2.5 px-5 rounded-xl text-sm transition-colors"
        >
          {t("viewHistory")}
          <ArrowRight className="w-4 h-4" aria-hidden="true" />
        </Link>
      </div>
    </div>
  );
}
