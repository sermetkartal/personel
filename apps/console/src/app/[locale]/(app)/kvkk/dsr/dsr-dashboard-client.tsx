"use client";

import { useTranslations, useLocale } from "next-intl";
import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { listDSRs, dsrKeys, dsrDaysElapsed, dsrDaysRemaining } from "@/lib/api/dsr";
import type { DSRList, DSRRequest, DSRState, DSRType } from "@/lib/api/types";
import { formatDateTR } from "@/lib/utils";
import { RequestTimeline } from "@/components/dsr/request-timeline";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { AlertTriangle, FileText, CheckCircle, XCircle, Clock } from "lucide-react";
import { useState } from "react";
import { useRouter } from "next/navigation";

interface DSRDashboardClientProps {
  openCount: number;
  atRiskCount: number;
  overdueCount: number;
  initialList: DSRList;
  currentState?: string;
  currentPage: number;
}

const STATE_BADGE: Record<DSRState, { variant: "success" | "warning" | "critical" | "destructive" | "info" | "outline"; icon: React.ElementType }> = {
  open: { variant: "info", icon: Clock },
  at_risk: { variant: "warning", icon: AlertTriangle },
  overdue: { variant: "destructive", icon: XCircle },
  resolved: { variant: "success", icon: CheckCircle },
  rejected: { variant: "outline", icon: XCircle },
};

function DSRRow({ request }: { request: DSRRequest }): JSX.Element {
  const t = useTranslations("dsr");
  const locale = useLocale();
  const stateConfig = STATE_BADGE[request.state];
  const daysElapsed = dsrDaysElapsed(request);
  const daysRemaining = dsrDaysRemaining(request);
  const slaColor =
    daysRemaining < 0
      ? "text-red-600 font-semibold"
      : daysRemaining < 3
        ? "text-red-600"
        : daysRemaining < 7
          ? "text-amber-600"
          : "text-muted-foreground";

  return (
    <tr className="border-b hover:bg-muted/30 transition-colors">
      <td className="px-4 py-3">
        <Link
          href={`/${locale}/kvkk/dsr/${request.id}`}
          className="font-mono text-xs text-muted-foreground hover:text-foreground hover:underline"
        >
          {request.id.slice(0, 8)}...
        </Link>
      </td>
      <td className="px-4 py-3">
        <Badge variant="outline" className="text-xs">
          {t(`types.${request.request_type}`)}
        </Badge>
      </td>
      <td className="px-4 py-3">
        <Badge variant={stateConfig.variant}>
          <stateConfig.icon className="mr-1 h-3 w-3" aria-hidden="true" />
          {t(`states.${request.state}`)}
        </Badge>
      </td>
      <td className="px-4 py-3 text-sm text-muted-foreground">
        <time dateTime={request.created_at}>{formatDateTR(request.created_at, "d MMM yyyy")}</time>
      </td>
      <td className="px-4 py-3">
        <div className="w-32">
          <div className="h-1.5 rounded-full bg-muted overflow-hidden">
            <div
              className={`h-full rounded-full transition-all ${
                daysElapsed >= 30
                  ? "bg-red-500"
                  : daysElapsed >= 28
                  ? "bg-orange-500"
                  : daysElapsed >= 20
                  ? "bg-amber-500"
                  : "bg-green-500"
              }`}
              style={{ width: `${Math.min((daysElapsed / 30) * 100, 100)}%` }}
              role="progressbar"
              aria-valuemin={0}
              aria-valuemax={30}
              aria-valuenow={daysElapsed}
              aria-label={`${daysElapsed} gün geçti`}
            />
          </div>
          <span className={`text-xs ${slaColor}`}>
            {daysRemaining < 0
              ? t("detail.daysOverdue", { days: Math.abs(daysRemaining) })
              : t("detail.daysRemaining", { days: daysRemaining })}
          </span>
        </div>
      </td>
      <td className="px-4 py-3 text-right">
        <Button variant="ghost" size="sm" asChild>
          <Link href={`/${locale}/kvkk/dsr/${request.id}`} aria-label={`Talep ${request.id.slice(0, 8)} detaylarını görüntüle`}>
            Görüntüle
          </Link>
        </Button>
      </td>
    </tr>
  );
}

export function DSRDashboardClient({
  openCount,
  atRiskCount,
  overdueCount,
  initialList,
  currentState,
  currentPage,
}: DSRDashboardClientProps): JSX.Element {
  const t = useTranslations("dsr");
  const locale = useLocale();
  const router = useRouter();
  const [stateFilter, setStateFilter] = useState<string>(currentState ?? "all");
  const [typeFilter, setTypeFilter] = useState<string>("all");

  const { data } = useQuery({
    queryKey: dsrKeys.list({
      state: stateFilter === "all" ? undefined : (stateFilter as DSRState),
      type: typeFilter === "all" ? undefined : (typeFilter as DSRType),
      page: currentPage,
      page_size: 20,
    }),
    queryFn: () =>
      listDSRs({
        state: stateFilter === "all" ? undefined : (stateFilter as DSRState),
        type: typeFilter === "all" ? undefined : (typeFilter as DSRType),
        page: currentPage,
        page_size: 20,
      }),
    initialData:
      stateFilter === (currentState ?? "all") && typeFilter === "all"
        ? initialList
        : undefined,
  });

  const handleStateChange = (value: string) => {
    setStateFilter(value);
    const params = new URLSearchParams();
    if (value !== "all") params.set("state", value);
    router.push(`/${locale}/dsr?${params.toString()}`);
  };

  const items = data?.items ?? [];

  return (
    <div className="space-y-6">
      {/* Summary cards */}
      <div className="grid gap-4 sm:grid-cols-3">
        <Card
          className={overdueCount > 0 ? "border-red-200 bg-red-50 dark:bg-red-900/10" : ""}
        >
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">{t("dashboard.overdue")}</CardTitle>
            <XCircle
              className={`h-4 w-4 ${overdueCount > 0 ? "text-red-500" : "text-muted-foreground"}`}
              aria-hidden="true"
            />
          </CardHeader>
          <CardContent>
            <div
              className={`text-3xl font-bold ${overdueCount > 0 ? "text-red-600" : "text-foreground"}`}
            >
              {overdueCount}
            </div>
            <p className="text-xs text-muted-foreground mt-1">30 gün sınırı geçmiş</p>
          </CardContent>
        </Card>

        <Card className={atRiskCount > 0 ? "border-amber-200 bg-amber-50 dark:bg-amber-900/10" : ""}>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">{t("dashboard.atRisk")}</CardTitle>
            <AlertTriangle
              className={`h-4 w-4 ${atRiskCount > 0 ? "text-amber-500" : "text-muted-foreground"}`}
              aria-hidden="true"
            />
          </CardHeader>
          <CardContent>
            <div className={`text-3xl font-bold ${atRiskCount > 0 ? "text-amber-600" : "text-foreground"}`}>
              {atRiskCount}
            </div>
            <p className="text-xs text-muted-foreground mt-1">20+ gün geçmiş</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium">{t("dashboard.open")}</CardTitle>
            <FileText className="h-4 w-4 text-muted-foreground" aria-hidden="true" />
          </CardHeader>
          <CardContent>
            <div className="text-3xl font-bold">{openCount}</div>
            <p className="text-xs text-muted-foreground mt-1">Yanıt bekliyor</p>
          </CardContent>
        </Card>
      </div>

      {/* Filters */}
      <div className="flex flex-wrap items-center gap-3">
        <Select value={stateFilter} onValueChange={handleStateChange}>
          <SelectTrigger className="w-48" aria-label="Durum filtresi">
            <SelectValue placeholder="Tüm Durumlar" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">Tüm Durumlar</SelectItem>
            <SelectItem value="open">{t("states.open")}</SelectItem>
            <SelectItem value="at_risk">{t("states.at_risk")}</SelectItem>
            <SelectItem value="overdue">{t("states.overdue")}</SelectItem>
            <SelectItem value="resolved">{t("states.resolved")}</SelectItem>
            <SelectItem value="rejected">{t("states.rejected")}</SelectItem>
          </SelectContent>
        </Select>

        <Select value={typeFilter} onValueChange={setTypeFilter}>
          <SelectTrigger className="w-48" aria-label="Talep türü filtresi">
            <SelectValue placeholder="Tüm Türler" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">Tüm Türler</SelectItem>
            <SelectItem value="access">{t("types.access")}</SelectItem>
            <SelectItem value="rectify">{t("types.rectify")}</SelectItem>
            <SelectItem value="erase">{t("types.erase")}</SelectItem>
            <SelectItem value="object">{t("types.object")}</SelectItem>
            <SelectItem value="restrict">{t("types.restrict")}</SelectItem>
            <SelectItem value="portability">{t("types.portability")}</SelectItem>
          </SelectContent>
        </Select>

        <Button variant="outline" size="sm" asChild>
          <Link href={`/${locale}/kvkk/dsr/new`}>
            <FileText className="h-4 w-4" aria-hidden="true" />
            Yeni DSR Oluştur
          </Link>
        </Button>
      </div>

      {/* Table */}
      <div className="rounded-md border">
        <table className="w-full text-sm" role="table" aria-label="Veri talepleri listesi">
          <thead>
            <tr className="border-b bg-muted/50">
              <th className="px-4 py-3 text-left font-medium text-muted-foreground" scope="col">
                Talep No.
              </th>
              <th className="px-4 py-3 text-left font-medium text-muted-foreground" scope="col">
                {t("detail.requestType")}
              </th>
              <th className="px-4 py-3 text-left font-medium text-muted-foreground" scope="col">
                Durum
              </th>
              <th className="px-4 py-3 text-left font-medium text-muted-foreground hidden md:table-cell" scope="col">
                {t("detail.submittedAt")}
              </th>
              <th className="px-4 py-3 text-left font-medium text-muted-foreground hidden md:table-cell" scope="col">
                SLA
              </th>
              <th className="px-4 py-3 text-right font-medium text-muted-foreground" scope="col">
                <span className="sr-only">İşlemler</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {items.length === 0 ? (
              <tr>
                <td className="px-4 py-8 text-center text-muted-foreground" colSpan={6}>
                  Kayıt bulunamadı.
                </td>
              </tr>
            ) : (
              items.map((request) => (
                <DSRRow key={request.id} request={request} />
              ))
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
