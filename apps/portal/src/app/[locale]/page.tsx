import { redirect } from "next/navigation";
import Link from "next/link";
import { getTranslations, getLocale } from "next-intl/server";
import {
  Database,
  Scale,
  InboxIcon,
  ArrowRight,
  Shield,
  Info,
} from "lucide-react";
import { getSession } from "@/lib/auth/session";
import { listMyDSRs } from "@/lib/api/dsr";
import { LegalTerm } from "@/components/common/legal-term";
import { cn } from "@/lib/utils";

export default async function HomePage(): Promise<JSX.Element> {
  const session = await getSession();
  const locale = await getLocale();

  if (!session) {
    redirect(`/${locale}/giris`);
  }

  // TypeScript: redirect() throws, so session is non-null below
  const validSession = session!;

  const t = await getTranslations("home");

  // Load DSR summary for quick links — non-critical, gracefully handle failure
  let openDsrCount = 0;
  try {
    const dsrList = await listMyDSRs(validSession.accessToken);
    openDsrCount = dsrList.items.filter(
      (d) => d.state === "open" || d.state === "at_risk" || d.state === "overdue"
    ).length;
  } catch {
    // Non-critical — homepage still renders
  }

  const firstName = validSession.name.split(" ")[0] ?? validSession.name;

  const quickLinks = [
    {
      href: `/${locale}/verilerim`,
      icon: Database,
      title: t("viewMyData"),
      description: t("viewMyDataDesc"),
      color: "portal",
    },
    {
      href: `/${locale}/haklar`,
      icon: Scale,
      title: t("myRights"),
      description: t("myRightsDesc"),
      color: "trust",
    },
    {
      href: `/${locale}/basvurularim`,
      icon: InboxIcon,
      title: t("myApplications"),
      description: t("myApplicationsDesc"),
      badge: openDsrCount > 0 ? t("openDsrCount", { count: openDsrCount }) : undefined,
      color: "portal",
    },
  ] as const;

  return (
    <div className="space-y-10 animate-fade-in">
      {/* Welcome section */}
      <section aria-labelledby="welcome-heading">
        <div className="flex items-start gap-4">
          <div
            className="w-10 h-10 rounded-xl bg-portal-100 flex items-center justify-center flex-shrink-0"
            aria-hidden="true"
          >
            <Shield className="w-5 h-5 text-portal-600" />
          </div>
          <div>
            <h1 id="welcome-heading" className="text-2xl font-semibold text-warm-900">
              {t("welcome")}, {firstName}
            </h1>
            <p className="mt-2 text-warm-600 leading-relaxed max-w-2xl">
              {t("subtitle")}
            </p>
          </div>
        </div>
      </section>

      {/* Quick links */}
      <section aria-labelledby="quick-links-heading">
        <h2
          id="quick-links-heading"
          className="text-base font-semibold text-warm-700 mb-4"
        >
          {t("quickLinks")}
        </h2>
        <div className="grid gap-4 sm:grid-cols-3">
          {quickLinks.map((link) => {
            const Icon = link.icon;
            return (
              <Link
                key={link.href}
                href={link.href}
                className="card-hover group flex flex-col gap-3 no-underline"
                aria-label={link.title}
              >
                <div className="flex items-start justify-between">
                  <div
                    className={cn(
                      "w-9 h-9 rounded-xl flex items-center justify-center flex-shrink-0",
                      link.color === "trust"
                        ? "bg-trust-50 group-hover:bg-trust-100"
                        : "bg-portal-50 group-hover:bg-portal-100",
                      "transition-colors duration-150"
                    )}
                    aria-hidden="true"
                  >
                    <Icon
                      className={cn(
                        "w-4 h-4",
                        link.color === "trust"
                          ? "text-trust-600"
                          : "text-portal-600"
                      )}
                    />
                  </div>
                  {"badge" in link && link.badge && (
                    <span className="badge-warning text-xs">{link.badge}</span>
                  )}
                </div>
                <div>
                  <h3 className="font-medium text-warm-900 group-hover:text-portal-700 transition-colors duration-150">
                    {link.title}
                  </h3>
                  <p className="mt-1 text-sm text-warm-500 leading-relaxed">
                    {link.description}
                  </p>
                </div>
                <div className="flex items-center gap-1 text-xs text-portal-500 mt-auto">
                  <span>Görüntüle</span>
                  <ArrowRight
                    className="w-3 h-3 group-hover:translate-x-0.5 transition-transform duration-150"
                    aria-hidden="true"
                  />
                </div>
              </Link>
            );
          })}
        </div>
      </section>

      {/* About this portal */}
      <section
        aria-labelledby="portal-purpose-heading"
        className="card bg-warm-50 border-warm-200"
      >
        <div className="flex items-start gap-3">
          <Info
            className="w-5 h-5 text-portal-400 flex-shrink-0 mt-0.5"
            aria-hidden="true"
          />
          <div className="space-y-3">
            <h2 id="portal-purpose-heading" className="text-sm font-semibold text-warm-800">
              {t("portalPurpose")}
            </h2>
            <p className="text-sm text-warm-600 leading-relaxed">
              <LegalTerm termKey="m10">KVKK m.10</LegalTerm> ve{" "}
              <LegalTerm termKey="m11">m.11</LegalTerm>{" "}
              {t("portalPurposeText").split("KVKK m.10")[1] ??
                t("portalPurposeText")}
            </p>
            <p className="text-sm text-warm-600 leading-relaxed">
              {t("yourDataIsYoursText")}
            </p>
          </div>
        </div>
      </section>
    </div>
  );
}
