"use client";

/**
 * DSR detail client with tabs + KVKK m.11 fulfillment workflow.
 *
 * Key rules:
 * - Access (m.11/b) → "Export Oluştur" produces a 7-day presigned download
 *   with SHA-256 + size so auditors can vouch for pack integrity.
 * - Erasure (m.11/f) → mandatory "Dry Run" first; real destruction disabled
 *   for non-DPO roles with a tooltip. 409 CONFLICT surfaces blocking legal
 *   holds as a yellow banner (never a transient toast).
 * - Defensive: we never render raw scope values that look like keystroke
 *   content; scope_json is deep-stringified and keystroke.* keys redacted.
 */

import { useState } from "react";
import { useTranslations, useLocale } from "next-intl";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { toast } from "sonner";
import {
  ChevronLeft,
  Download,
  FlaskConical,
  FileArchive,
  AlertTriangle,
  FileText,
  Clock,
  Trash2,
} from "lucide-react";
import {
  getDSR,
  dsrKeys,
  dsrDaysElapsed,
  dsrDaysRemaining,
  fulfillDSRAccess,
  fulfillDSRErasure,
  type DSRAccessFulfillment,
  type DSRErasureReport,
} from "@/lib/api/dsr";
import type { DSRRequest, DSRState, Role } from "@/lib/api/types";
import { formatDateTR, slaStatusFromDays, SLA_STATUS_COLORS, snakeToTitle } from "@/lib/utils";
import { ApiError } from "@/lib/api/client";
import { toUserFacingError } from "@/lib/errors";
import { DSRFulfillmentActions } from "@/components/dsr/fulfillment-actions";
import { RequestTimeline } from "@/components/dsr/request-timeline";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";

const DSR_TYPE_LABELS: Record<string, string> = {
  access: "Bilgi Talebi",
  rectify: "Düzeltme",
  erase: "Silme",
  object: "İtiraz",
  restrict: "Kısıtlama",
  portability: "Taşınabilirlik",
};

const STATE_VARIANTS: Record<
  DSRState,
  "info" | "warning" | "destructive" | "success" | "outline"
> = {
  open: "info",
  at_risk: "warning",
  overdue: "destructive",
  resolved: "success",
  rejected: "outline",
};

interface DSRDetailClientProps {
  dsr: DSRRequest;
  role: Role;
  canExecuteErasure: boolean;
}

export function DSRDetailClient({
  dsr: initialDSR,
  role,
  canExecuteErasure,
}: DSRDetailClientProps): JSX.Element {
  const t = useTranslations("dsr");
  const tc = useTranslations("common");
  const locale = useLocale();
  const qc = useQueryClient();

  const [activeTab, setActiveTab] = useState("info");
  const [accessExport, setAccessExport] = useState<DSRAccessFulfillment | null>(
    null,
  );
  const [erasureReport, setErasureReport] = useState<DSRErasureReport | null>(
    null,
  );
  const [erasureDialog, setErasureDialog] = useState<
    "dry-run" | "confirm" | null
  >(null);
  const [erasureConfirmText, setErasureConfirmText] = useState("");
  const [legalHoldBlock, setLegalHoldBlock] = useState<string | null>(null);
  const [blockingHolds, setBlockingHolds] = useState<
    { id: string; reason_code: string }[] | null
  >(null);

  const { data: dsr = initialDSR } = useQuery({
    queryKey: dsrKeys.detail(initialDSR.id),
    queryFn: () => getDSR(initialDSR.id),
    initialData: initialDSR,
  });

  const isDPO = role === "dpo";
  const daysElapsed = dsrDaysElapsed(dsr);
  const daysRemaining = dsrDaysRemaining(dsr);
  const slaStatus = slaStatusFromDays(daysElapsed);

  const redactedScope = redactKeystroke(dsr.scope_json ?? {});

  // ── Access fulfillment ───────────────────────────────────────────────
  const accessMutation = useMutation({
    mutationFn: () => fulfillDSRAccess(dsr.id),
    onSuccess: (result) => {
      setAccessExport(result);
      void qc.invalidateQueries({ queryKey: dsrKeys.detail(dsr.id) });
      toast.success(t("actions.accessExportGenerated"));
    },
    onError: (err) => {
      const ufe = toUserFacingError(err);
      toast.error(ufe.title, { description: ufe.description });
    },
  });

  // ── Erasure fulfillment ──────────────────────────────────────────────
  const erasureMutation = useMutation({
    mutationFn: (dryRun: boolean) => fulfillDSRErasure(dsr.id, dryRun),
    onSuccess: (report, dryRun) => {
      setErasureReport(report);
      setLegalHoldBlock(null);
      setBlockingHolds(null);
      if (dryRun) {
        setErasureDialog("dry-run");
      } else {
        setErasureDialog(null);
        setErasureConfirmText("");
        void qc.invalidateQueries({ queryKey: dsrKeys.detail(dsr.id) });
        toast.success(tc("saving"));
      }
    },
    onError: (err) => {
      if (err instanceof ApiError && err.status === 409) {
        const holds =
          (err.problem as { blocking_holds?: { id: string; reason_code: string }[] })
            .blocking_holds ?? null;
        setBlockingHolds(holds);
        setLegalHoldBlock(err.problem.detail ?? t("actions.legalHoldBlock"));
        return;
      }
      const ufe = toUserFacingError(err);
      toast.error(ufe.title, { description: ufe.description });
    },
  });

  return (
    <div className="space-y-6 max-w-4xl animate-fade-in">
      <Button variant="ghost" size="sm" className="-ml-2" asChild>
        <Link href={`/${locale}/dsr`}>
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
          <Badge variant={STATE_VARIANTS[dsr.state] ?? "outline"}>
            {t(`states.${dsr.state}`)}
          </Badge>
        </div>
      </div>

      {/* SLA countdown banner */}
      <div className="rounded-lg border bg-card p-4 space-y-3">
        <h2 className="text-sm font-semibold">{t("detail.slaTitle")}</h2>
        <RequestTimeline request={dsr} />
        <p className={`text-xs font-medium ${SLA_STATUS_COLORS[slaStatus]}`}>
          {daysRemaining > 0
            ? t("detail.daysRemaining", { days: daysRemaining })
            : t("detail.daysOverdue", { days: Math.abs(daysRemaining) })}
        </p>
        {daysRemaining < 0 && (
          <p className="text-sm font-medium text-red-600" role="alert">
            {t("actions.slaOverdue", { days: Math.abs(daysRemaining) })}
          </p>
        )}
      </div>

      <Tabs value={activeTab} onValueChange={setActiveTab} className="w-full">
        <TabsList>
          <TabsTrigger value="info">
            <FileText className="mr-2 h-4 w-4" aria-hidden="true" />
            Talep Bilgisi
          </TabsTrigger>
          <TabsTrigger value="fulfill">İşleme</TabsTrigger>
          <TabsTrigger value="history">
            <Clock className="mr-2 h-4 w-4" aria-hidden="true" />
            Geçmiş
          </TabsTrigger>
        </TabsList>

        <TabsContent value="info" className="space-y-4">
          <div className="rounded-lg border bg-card p-4 space-y-4">
            <h2 className="text-sm font-semibold">{t("detail.infoTitle")}</h2>
            <dl className="grid grid-cols-2 gap-4 text-sm">
              <div>
                <dt className="text-xs text-muted-foreground">
                  {t("detail.submittedAt")}
                </dt>
                <dd>
                  <time dateTime={dsr.created_at}>
                    {formatDateTR(dsr.created_at)}
                  </time>
                </dd>
              </div>
              <div>
                <dt className="text-xs text-muted-foreground">
                  {t("detail.deadline")}
                </dt>
                <dd>
                  <time dateTime={dsr.sla_deadline}>
                    {formatDateTR(dsr.sla_deadline)}
                  </time>
                </dd>
              </div>
              <div>
                <dt className="text-xs text-muted-foreground">
                  {t("detail.employee")}
                </dt>
                <dd className="font-mono text-xs">{dsr.employee_user_id}</dd>
              </div>
              {dsr.assigned_to && (
                <div>
                  <dt className="text-xs text-muted-foreground">
                    {t("detail.assignedTo")}
                  </dt>
                  <dd className="font-mono text-xs">{dsr.assigned_to}</dd>
                </div>
              )}
              {dsr.response_artifact_ref && (
                <div className="col-span-2">
                  <dt className="text-xs text-muted-foreground">
                    {t("detail.artifactRef")}
                  </dt>
                  <dd className="font-mono text-xs break-all">
                    {dsr.response_artifact_ref}
                  </dd>
                </div>
              )}
            </dl>

            {Object.keys(redactedScope).length > 0 && (
              <div>
                <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-2">
                  {t("detail.scope")}
                </h3>
                <dl className="grid grid-cols-2 gap-2 text-xs">
                  {Object.entries(redactedScope).map(([k, v]) => (
                    <div key={k}>
                      <dt className="text-muted-foreground">
                        {snakeToTitle(k)}
                      </dt>
                      <dd className="font-medium break-words">{String(v)}</dd>
                    </div>
                  ))}
                </dl>
              </div>
            )}
          </div>
        </TabsContent>

        <TabsContent value="fulfill" className="space-y-4">
          {/* Legal hold banner */}
          {legalHoldBlock && (
            <div
              className="rounded-md border border-amber-300 bg-amber-50 p-4 text-sm text-amber-900 dark:bg-amber-900/20 dark:text-amber-200"
              role="alert"
            >
              <div className="flex items-start gap-3">
                <AlertTriangle
                  className="mt-0.5 h-5 w-5 shrink-0"
                  aria-hidden="true"
                />
                <div className="space-y-1">
                  <p className="font-medium">
                    {t("actions.legalHoldBlock")}
                  </p>
                  <p className="text-xs">{legalHoldBlock}</p>
                  {blockingHolds && blockingHolds.length > 0 && (
                    <p className="text-xs">
                      Blocking holds:{" "}
                      {blockingHolds
                        .map((h) => `${h.reason_code} (${h.id.slice(0, 8)})`)
                        .join(", ")}
                    </p>
                  )}
                </div>
              </div>
            </div>
          )}

          {/* Access fulfillment */}
          {dsr.request_type === "access" && (
            <div className="rounded-lg border bg-card p-4 space-y-4">
              <h3 className="text-sm font-semibold">
                {t("actions.fulfillAccess")}
              </h3>
              <p className="text-xs text-muted-foreground">
                KVKK m.11/b — çalışanın kişisel verilerine ilişkin tüm
                kayıtların şifreli bir paket olarak dışa aktarılması.
              </p>

              {accessExport ? (
                <AccessArtifactCard result={accessExport} />
              ) : (
                <Button
                  onClick={() => accessMutation.mutate()}
                  disabled={
                    !isDPO ||
                    accessMutation.isPending ||
                    dsr.state === "resolved"
                  }
                >
                  <FileArchive
                    className="mr-2 h-4 w-4"
                    aria-hidden="true"
                  />
                  {accessMutation.isPending
                    ? tc("loading")
                    : t("actions.fulfillAccess")}
                </Button>
              )}
            </div>
          )}

          {/* Erasure fulfillment */}
          {dsr.request_type === "erase" && (
            <div className="rounded-lg border bg-card p-4 space-y-4">
              <h3 className="text-sm font-semibold">
                {t("actions.fulfillErasure")}
              </h3>
              <p className="text-xs text-muted-foreground">
                KVKK m.11/f — kişisel verilerin postgres/clickhouse/minio/vault
                bileşenlerinden kalıcı olarak silinmesi. Silmeden önce her
                zaman bir simülasyon (dry run) çalıştırılması zorunludur.
              </p>

              <div className="flex flex-wrap gap-2">
                <Button
                  variant="outline"
                  onClick={() => erasureMutation.mutate(true)}
                  disabled={erasureMutation.isPending}
                >
                  <FlaskConical
                    className="mr-2 h-4 w-4"
                    aria-hidden="true"
                  />
                  {t("actions.dryRun")}
                </Button>

                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span className="inline-block">
                        <Button
                          variant="destructive"
                          onClick={() => setErasureDialog("confirm")}
                          disabled={
                            !canExecuteErasure ||
                            !erasureReport?.dry_run ||
                            erasureMutation.isPending
                          }
                        >
                          <Trash2
                            className="mr-2 h-4 w-4"
                            aria-hidden="true"
                          />
                          {t("actions.executeErasure")}
                        </Button>
                      </span>
                    </TooltipTrigger>
                    {!canExecuteErasure && (
                      <TooltipContent>
                        {t("actions.erasureNotAllowed")}
                      </TooltipContent>
                    )}
                    {canExecuteErasure && !erasureReport?.dry_run && (
                      <TooltipContent>
                        Önce Dry Run çalıştırın.
                      </TooltipContent>
                    )}
                  </Tooltip>
                </TooltipProvider>
              </div>

              {erasureReport && !erasureReport.dry_run && (
                <ErasureReportCard report={erasureReport} finalRun />
              )}
            </div>
          )}

          {/* Generic legacy actions (assign/respond/extend/reject) */}
          {isDPO && (
            <div className="rounded-lg border bg-card p-4 space-y-3">
              <h2 className="text-sm font-semibold">
                {t("detail.actionsTitle")}
              </h2>
              <DSRFulfillmentActions dsrId={dsr.id} state={dsr.state} />
            </div>
          )}
        </TabsContent>

        <TabsContent value="history" className="space-y-4">
          <div className="rounded-lg border bg-card p-4 text-sm text-muted-foreground">
            Audit zinciri üzerinden bu talep için tutulan geçmiş kayıtlar
            denetim günlüğünde görüntülenebilir.
          </div>
        </TabsContent>
      </Tabs>

      {/* Dry run report dialog */}
      <Dialog
        open={erasureDialog === "dry-run"}
        onOpenChange={(o) => !o && setErasureDialog(null)}
      >
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>{t("actions.erasureDryRunTitle")}</DialogTitle>
            <DialogDescription>
              {t("actions.erasureDryRunDesc")}
            </DialogDescription>
          </DialogHeader>
          {erasureReport && <ErasureReportCard report={erasureReport} />}
          <DialogFooter>
            <Button onClick={() => setErasureDialog(null)}>Kapat</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Real erasure confirmation dialog */}
      <Dialog
        open={erasureDialog === "confirm"}
        onOpenChange={(o) => {
          if (!o) {
            setErasureDialog(null);
            setErasureConfirmText("");
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="text-red-700">
              {t("actions.erasureConfirmTitle")}
            </DialogTitle>
            <DialogDescription>
              {t("actions.erasureConfirmDesc")}
            </DialogDescription>
          </DialogHeader>
          {erasureReport && <ErasureReportCard report={erasureReport} />}
          <div className="space-y-1 pt-3">
            <Label htmlFor="erasure-confirm">
              {t("actions.erasureTypeToConfirm")}
            </Label>
            <Input
              id="erasure-confirm"
              value={erasureConfirmText}
              onChange={(e) =>
                setErasureConfirmText(e.target.value.toUpperCase())
              }
              placeholder="SIL"
            />
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => {
                setErasureDialog(null);
                setErasureConfirmText("");
              }}
            >
              {tc("cancel")}
            </Button>
            <Button
              variant="destructive"
              disabled={
                erasureConfirmText !== "SIL" || erasureMutation.isPending
              }
              onClick={() => erasureMutation.mutate(false)}
            >
              {erasureMutation.isPending
                ? tc("saving")
                : t("actions.executeErasure")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

// ── Helpers ───────────────────────────────────────────────────────────────

function AccessArtifactCard({
  result,
}: {
  result: DSRAccessFulfillment;
}): JSX.Element {
  const t = useTranslations("dsr.actions");
  const sizeStr = formatBytes(result.artifact_size_bytes);
  return (
    <div className="rounded-md border bg-muted/30 p-4 space-y-3">
      <p className="text-sm font-medium">{t("accessExportGenerated")}</p>
      <div className="text-xs text-muted-foreground space-y-1">
        <p>
          {t("artifactRecordCount", { count: result.record_count })}
        </p>
        <p className="font-mono break-all">
          {t("artifactSize", {
            size: sizeStr,
            hash: `${result.artifact_sha256.slice(0, 12)}…`,
          })}
        </p>
        <p>
          {t("artifactExpires", { date: formatDateTR(result.expires_at) })}
        </p>
      </div>
      <Button size="sm" asChild>
        <a
          href={result.artifact_url}
          target="_blank"
          rel="noopener noreferrer"
          download
        >
          <Download className="mr-2 h-4 w-4" aria-hidden="true" />
          {t("downloadArtifact")}
        </a>
      </Button>
    </div>
  );
}

function ErasureReportCard({
  report,
  finalRun,
}: {
  report: DSRErasureReport;
  finalRun?: boolean;
}): JSX.Element {
  const t = useTranslations("dsr.actions");
  return (
    <div
      className={`rounded-md border p-4 text-xs space-y-2 ${
        finalRun ? "border-green-300 bg-green-50 dark:bg-green-900/10" : "bg-muted/30"
      }`}
      data-testid="erasure-report"
    >
      <dl className="grid grid-cols-2 gap-2">
        <div>
          <dt className="text-muted-foreground">{t("pgRows")}</dt>
          <dd className="font-mono font-medium">
            {report.postgres_rows_deleted.toLocaleString("tr-TR")}
          </dd>
        </div>
        <div>
          <dt className="text-muted-foreground">{t("chRows")}</dt>
          <dd className="font-mono font-medium">
            {report.clickhouse_rows_deleted.toLocaleString("tr-TR")}
          </dd>
        </div>
        <div>
          <dt className="text-muted-foreground">{t("minioKeys")}</dt>
          <dd className="font-mono font-medium">
            {report.minio_keys_erased.toLocaleString("tr-TR")}
          </dd>
        </div>
        <div>
          <dt className="text-muted-foreground">{t("vaultKeys")}</dt>
          <dd className="font-mono font-medium">
            {report.vault_keys_destroyed.toLocaleString("tr-TR")}
          </dd>
        </div>
      </dl>
      {report.completed_at && (
        <p className="text-muted-foreground">
          {t("completedAt")}:{" "}
          <time dateTime={report.completed_at}>
            {formatDateTR(report.completed_at)}
          </time>
        </p>
      )}
      {report.audit_log_id && (
        <p className="text-muted-foreground font-mono">
          {t("auditLogId")}: {report.audit_log_id}
        </p>
      )}
    </div>
  );
}

/**
 * Defensive keystroke.* redaction. Agents must never emit raw keystroke
 * content, but if a buggy collector ever does, the console STILL refuses
 * to render it — we belt-and-brace this on the client.
 */
function redactKeystroke(
  obj: Record<string, unknown>,
): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(obj)) {
    if (k.toLowerCase().startsWith("keystroke")) {
      out[k] = "[REDACTED]";
      continue;
    }
    out[k] = v;
  }
  return out;
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1)} MB`;
  return `${(n / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}
