"use client";

import { useTranslations, useLocale } from "next-intl";
import Link from "next/link";
import {
  Monitor,
  AlertTriangle,
  FileText,
  Eye,
  Shield,
  ShieldOff,
  ArrowRight,
  Clock,
} from "lucide-react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import type { AuditRecord, DLPStateResponse, Role } from "@/lib/api/types";
import { formatRelativeTR } from "@/lib/utils";
import { can } from "@/lib/auth/rbac";

interface DashboardClientProps {
  activeEndpointsTotal: number;
  openDSRs: number;
  atRiskDSRs: number;
  overdueDSRs: number;
  pendingLiveViews: number;
  recentAuditItems: AuditRecord[];
  dlpState: DLPStateResponse | null;
  userRole: Role;
}

interface StatCardProps {
  title: string;
  description: string;
  value: number;
  icon: React.ElementType;
  variant?: "default" | "warning" | "critical";
  href: string;
}

function StatCard({ title, description, value, icon: Icon, variant = "default", href }: StatCardProps): JSX.Element {
  const locale = useLocale();

  return (
    <Link href={`/${locale}${href}`} className="block group">
      <Card className="transition-shadow group-hover:shadow-md">
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <CardTitle className="text-sm font-medium">{title}</CardTitle>
          <Icon
            className={`h-4 w-4 ${
              variant === "critical"
                ? "text-red-500"
                : variant === "warning"
                ? "text-amber-500"
                : "text-muted-foreground"
            }`}
            aria-hidden="true"
          />
        </CardHeader>
        <CardContent>
          <div
            className={`text-3xl font-bold ${
              variant === "critical"
                ? "text-red-600"
                : variant === "warning"
                ? "text-amber-600"
                : "text-foreground"
            }`}
          >
            {value.toLocaleString("tr-TR")}
          </div>
          <p className="text-xs text-muted-foreground mt-1">{description}</p>
        </CardContent>
      </Card>
    </Link>
  );
}

export function DashboardClient({
  activeEndpointsTotal,
  openDSRs,
  atRiskDSRs,
  overdueDSRs,
  pendingLiveViews,
  recentAuditItems,
  dlpState,
  userRole,
}: DashboardClientProps): JSX.Element {
  const t = useTranslations("dashboard");
  const tDlp = useTranslations("dlp");
  const tAudit = useTranslations("audit");
  const locale = useLocale();

  const dlpIsActive = dlpState?.state === "active";

  return (
    <div className="space-y-6">
      {/* DLP State Banner — always shown, per ADR 0013 */}
      <Alert
        variant={dlpIsActive ? "success" : "warning"}
        role="status"
        aria-live="polite"
      >
        {dlpIsActive ? (
          <Shield className="h-4 w-4" aria-hidden="true" />
        ) : (
          <ShieldOff className="h-4 w-4" aria-hidden="true" />
        )}
        <AlertTitle>
          {dlpIsActive
            ? t("dlpBanner.active.title")
            : t("dlpBanner.disabled.title")}
        </AlertTitle>
        <AlertDescription className="flex items-center justify-between gap-4">
          <span>
            {dlpIsActive
              ? t("dlpBanner.active.description")
              : t("dlpBanner.disabled.description")}
          </span>
          <Button variant="outline" size="sm" asChild>
            <Link href={`/${locale}/settings/dlp`}>
              {dlpIsActive
                ? t("dlpBanner.active.action")
                : t("dlpBanner.disabled.action")}
              <ArrowRight className="ml-1.5 h-3 w-3" aria-hidden="true" />
            </Link>
          </Button>
        </AlertDescription>
      </Alert>

      {/* Overdue DSR alert */}
      {overdueDSRs > 0 && (
        <Alert variant="destructive" role="alert" aria-live="assertive">
          <AlertTriangle className="h-4 w-4" aria-hidden="true" />
          <AlertTitle>KVKK m.11 SLA İhlali</AlertTitle>
          <AlertDescription className="flex items-center justify-between gap-4">
            <span>
              {overdueDSRs} veri talebi 30 günlük yasal süreyi aşmıştır. Acil
              müdahale gereklidir.
            </span>
            <Button variant="outline" size="sm" asChild>
              <Link href={`/${locale}/dsr?state=overdue`}>
                Görüntüle <ArrowRight className="ml-1.5 h-3 w-3" aria-hidden="true" />
              </Link>
            </Button>
          </AlertDescription>
        </Alert>
      )}

      {/* Stat cards */}
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        {can(userRole, "manage:endpoints") && (
          <StatCard
            title={t("cards.activeEndpoints")}
            description={t("cards.activeEndpointsDesc")}
            value={activeEndpointsTotal}
            icon={Monitor}
            href="/endpoints"
          />
        )}

        {can(userRole, "manage:dsr") && (
          <StatCard
            title={t("cards.pendingDsrs")}
            description={t("cards.pendingDsrsDesc")}
            value={openDSRs + atRiskDSRs}
            icon={FileText}
            variant={atRiskDSRs > 0 ? "warning" : "default"}
            href="/dsr"
          />
        )}

        {can(userRole, "manage:dsr") && overdueDSRs > 0 && (
          <StatCard
            title={t("cards.overduedsrs")}
            description={t("cards.overduedsrsDesc")}
            value={overdueDSRs}
            icon={AlertTriangle}
            variant="critical"
            href="/dsr?state=overdue"
          />
        )}

        {can(userRole, "view:live-view-sessions") && (
          <StatCard
            title={t("cards.pendingLiveViewApprovals")}
            description={t("cards.pendingLiveViewApprovalsDesc")}
            value={pendingLiveViews}
            icon={Eye}
            variant={pendingLiveViews > 0 ? "warning" : "default"}
            href="/live-view/approvals"
          />
        )}
      </div>

      {/* Quick actions */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("quickActions.title")}</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-wrap gap-2">
          {can(userRole, "request:live-view") && (
            <Button variant="outline" size="sm" asChild>
              <Link href={`/${locale}/live-view/request`}>
                <Eye className="h-4 w-4" aria-hidden="true" />
                {t("quickActions.newLiveViewRequest")}
              </Link>
            </Button>
          )}
          {can(userRole, "manage:dsr") && (
            <Button variant="outline" size="sm" asChild>
              <Link href={`/${locale}/dsr`}>
                <FileText className="h-4 w-4" aria-hidden="true" />
                {t("quickActions.viewPendingDsrs")}
              </Link>
            </Button>
          )}
          {can(userRole, "view:audit-trail") && (
            <Button variant="outline" size="sm" asChild>
              <Link href={`/${locale}/audit`}>
                <Shield className="h-4 w-4" aria-hidden="true" />
                {t("quickActions.auditLog")}
              </Link>
            </Button>
          )}
        </CardContent>
      </Card>

      {/* Recent audit events */}
      {can(userRole, "view:audit-trail") && (
        <Card>
          <CardHeader className="flex flex-row items-center justify-between">
            <div>
              <CardTitle className="text-base">{t("recentAudit.title")}</CardTitle>
              <CardDescription>
                Son {recentAuditItems.length} denetim kaydı
              </CardDescription>
            </div>
            <Button variant="ghost" size="sm" asChild>
              <Link href={`/${locale}/audit`}>
                {t("recentAudit.viewAll")}
                <ArrowRight className="ml-1.5 h-3 w-3" aria-hidden="true" />
              </Link>
            </Button>
          </CardHeader>
          <CardContent>
            {recentAuditItems.length === 0 ? (
              <p className="text-sm text-muted-foreground py-4 text-center">
                Henüz denetim kaydı yok.
              </p>
            ) : (
              <div className="space-y-2">
                {recentAuditItems.map((record) => (
                  <div
                    key={record.id}
                    className="flex items-center justify-between gap-4 rounded-md border border-border/50 px-3 py-2 text-sm"
                  >
                    <div className="flex items-center gap-2 min-w-0">
                      <Badge variant="outline" className="shrink-0 font-mono text-xs">
                        {record.type}
                      </Badge>
                      {record.actor_id && (
                        <span className="text-muted-foreground truncate">
                          {record.actor_id.slice(0, 8)}...
                        </span>
                      )}
                    </div>
                    <div className="flex items-center gap-1 text-xs text-muted-foreground shrink-0">
                      <Clock className="h-3 w-3" aria-hidden="true" />
                      <time dateTime={record.created_at}>
                        {formatRelativeTR(record.created_at)}
                      </time>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </CardContent>
        </Card>
      )}
    </div>
  );
}
