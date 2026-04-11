import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect, notFound } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import { getDSR, dsrDaysElapsed, dsrDaysRemaining } from "@/lib/api/dsr";
import { DSRFulfillmentActions } from "@/components/dsr/fulfillment-actions";
import { RequestTimeline } from "@/components/dsr/request-timeline";
import { Badge } from "@/components/ui/badge";
import { Link } from "@/lib/i18n/navigation";
import { Button } from "@/components/ui/button";
import { ChevronLeft } from "lucide-react";
import { formatDateTR, slaStatusFromDays, SLA_STATUS_COLORS, snakeToTitle } from "@/lib/utils";

interface DSRDetailPageProps {
  params: Promise<{ locale: string; id: string }>;
}

export async function generateMetadata({ params }: DSRDetailPageProps) {
  const { id } = await params;
  const t = await getTranslations("dsr");
  return { title: `${t("detail.title")} ${id.slice(0, 8)}` };
}

const DSR_TYPE_LABELS: Record<string, string> = {
  access: "Bilgi Talebi",
  rectify: "Düzeltme",
  erase: "Silme",
  object: "İtiraz",
  restrict: "Kısıtlama",
  portability: "Taşınabilirlik",
};

const STATE_VARIANTS = {
  open: "info",
  at_risk: "warning",
  overdue: "destructive",
  resolved: "success",
  rejected: "default",
} as const;

export default async function DSRDetailPage({
  params,
}: DSRDetailPageProps): Promise<JSX.Element> {
  const { locale, id } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "view:dsr")) {
    redirect(`/${locale}/unauthorized`);
  }

  const t = await getTranslations("dsr");
  const isDPO = can(session.user.role, "manage:dsr");

  let dsr;
  try {
    dsr = await getDSR(id);
  } catch {
    notFound();
  }

  const daysElapsed = dsrDaysElapsed(dsr);
  const daysRemaining = dsrDaysRemaining(dsr);
  const slaStatus = slaStatusFromDays(daysElapsed);

  return (
    <div className="space-y-6 max-w-3xl animate-fade-in">
      {/* Back */}
      <Button variant="ghost" size="sm" className="-ml-2" asChild>
        <Link href="/dsr">
          <ChevronLeft className="mr-1 h-4 w-4" aria-hidden="true" />
          {t("backToList")}
        </Link>
      </Button>

      {/* Header */}
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">
            {t("detail.title")}
          </h1>
          <code className="text-xs text-muted-foreground">{dsr.id}</code>
        </div>
        <div className="flex items-center gap-2">
          <Badge variant="outline" className="text-sm">
            {DSR_TYPE_LABELS[dsr.request_type] ?? dsr.request_type}
          </Badge>
          <Badge variant={STATE_VARIANTS[dsr.state] ?? "default"}>
            {dsr.state}
          </Badge>
        </div>
      </div>

      {/* SLA timeline */}
      <div className="rounded-lg border bg-card p-4 space-y-3">
        <h2 className="text-sm font-semibold">{t("detail.slaTitle")}</h2>
        <RequestTimeline request={dsr} />
        <p className={`text-xs font-medium ${SLA_STATUS_COLORS[slaStatus]}`}>
          {daysRemaining > 0
            ? t("detail.daysRemaining", { days: daysRemaining })
            : t("detail.daysOverdue", { days: Math.abs(daysRemaining) })}
        </p>
      </div>

      {/* Details */}
      <div className="rounded-lg border bg-card p-4 space-y-4">
        <h2 className="text-sm font-semibold">{t("detail.infoTitle")}</h2>
        <dl className="grid grid-cols-2 gap-4 text-sm">
          <div>
            <dt className="text-xs text-muted-foreground">{t("detail.submittedAt")}</dt>
            <dd>
              <time dateTime={dsr.created_at}>{formatDateTR(dsr.created_at)}</time>
            </dd>
          </div>
          <div>
            <dt className="text-xs text-muted-foreground">{t("detail.deadline")}</dt>
            <dd>
              <time dateTime={dsr.sla_deadline}>{formatDateTR(dsr.sla_deadline)}</time>
            </dd>
          </div>
          <div>
            <dt className="text-xs text-muted-foreground">{t("detail.employee")}</dt>
            <dd className="font-mono text-xs">{dsr.employee_user_id}</dd>
          </div>
          {dsr.assigned_to && (
            <div>
              <dt className="text-xs text-muted-foreground">{t("detail.assignedTo")}</dt>
              <dd className="font-mono text-xs">{dsr.assigned_to}</dd>
            </div>
          )}
          {dsr.response_artifact_ref && (
            <div className="col-span-2">
              <dt className="text-xs text-muted-foreground">{t("detail.artifactRef")}</dt>
              <dd className="font-mono text-xs break-all">{dsr.response_artifact_ref}</dd>
            </div>
          )}
        </dl>

        {/* Scope */}
        {dsr.scope_json && Object.keys(dsr.scope_json).length > 0 && (
          <div>
            <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-2">
              {t("detail.scope")}
            </h3>
            <dl className="grid grid-cols-2 gap-2 text-xs">
              {Object.entries(dsr.scope_json).map(([k, v]) => (
                <div key={k}>
                  <dt className="text-muted-foreground">{snakeToTitle(k)}</dt>
                  <dd className="font-medium">{String(v)}</dd>
                </div>
              ))}
            </dl>
          </div>
        )}
      </div>

      {/* Actions — DPO only */}
      {isDPO && (
        <div className="rounded-lg border bg-card p-4 space-y-3">
          <h2 className="text-sm font-semibold">{t("detail.actionsTitle")}</h2>
          <DSRFulfillmentActions dsrId={dsr.id} state={dsr.state} />
        </div>
      )}
    </div>
  );
}
