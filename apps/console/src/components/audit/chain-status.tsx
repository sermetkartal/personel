"use client";

import { useQuery } from "@tanstack/react-query";
import { useTranslations } from "next-intl";
import { ShieldCheck, ShieldAlert, Loader2, Hash, Clock, Database } from "lucide-react";
import { getAuditChainStatus, auditKeys } from "@/lib/api/audit";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { formatDateTR } from "@/lib/utils";
import { cn } from "@/lib/utils";

interface ChainStatusProps {
  /** If provided, show an inline compact version (for audit list header). */
  compact?: boolean;
}

export function AuditChainStatus({ compact = false }: ChainStatusProps): JSX.Element {
  const t = useTranslations("audit.chain");

  const { data, isLoading, isError } = useQuery({
    queryKey: auditKeys.chainStatus,
    queryFn: getAuditChainStatus,
    // Refresh every 5 minutes — chain verification is server-side and expensive
    refetchInterval: 5 * 60_000,
    staleTime: 60_000,
  });

  if (isLoading) {
    return (
      <div className={cn("flex items-center gap-2 text-sm text-muted-foreground", compact && "text-xs")}>
        <Loader2 className="h-4 w-4 animate-spin" aria-hidden="true" />
        <span>{t("verifying")}</span>
      </div>
    );
  }

  if (isError || !data) {
    return (
      <Alert variant="warning" className={cn(compact && "py-2")}>
        <ShieldAlert className="h-4 w-4" aria-hidden="true" />
        <AlertTitle className={cn(compact && "text-sm")}>{t("errorTitle")}</AlertTitle>
        {!compact && (
          <AlertDescription>{t("errorBody")}</AlertDescription>
        )}
      </Alert>
    );
  }

  if (compact) {
    return (
      <div className="flex items-center gap-2">
        {data.valid ? (
          <>
            <ShieldCheck className="h-4 w-4 text-green-500" aria-hidden="true" />
            <span className="text-xs text-muted-foreground">
              {t("validCompact")} · {data.total_records.toLocaleString("tr-TR")} {t("records")}
            </span>
          </>
        ) : (
          <>
            <ShieldAlert className="h-4 w-4 text-destructive" aria-hidden="true" />
            <span className="text-xs text-destructive font-medium">
              {t("brokenAt")} #{data.broken_at_seq}
            </span>
          </>
        )}
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {/* Overall status alert */}
      {data.valid ? (
        <Alert variant="success">
          <ShieldCheck className="h-4 w-4" aria-hidden="true" />
          <AlertTitle>{t("validTitle")}</AlertTitle>
          <AlertDescription>{t("validBody")}</AlertDescription>
        </Alert>
      ) : (
        <Alert variant="destructive">
          <ShieldAlert className="h-4 w-4" aria-hidden="true" />
          <AlertTitle>{t("brokenTitle")}</AlertTitle>
          <AlertDescription>
            {t("brokenBody")}
            {data.broken_at_seq != null && (
              <span className="block mt-1 font-mono font-medium">
                {t("brokenAt")} #{data.broken_at_seq}
              </span>
            )}
          </AlertDescription>
        </Alert>
      )}

      {/* Chain details */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
        <div className="rounded-lg border bg-card p-4 space-y-1">
          <div className="flex items-center gap-2 text-muted-foreground text-xs">
            <Database className="h-3.5 w-3.5" aria-hidden="true" />
            {t("totalRecords")}
          </div>
          <p className="text-2xl font-bold tabular-nums">
            {data.total_records.toLocaleString("tr-TR")}
          </p>
        </div>

        <div className="rounded-lg border bg-card p-4 space-y-1">
          <div className="flex items-center gap-2 text-muted-foreground text-xs">
            <Clock className="h-3.5 w-3.5" aria-hidden="true" />
            {t("lastVerified")}
          </div>
          <p className="text-sm font-medium">
            <time dateTime={data.last_verified_at}>
              {formatDateTR(data.last_verified_at)}
            </time>
          </p>
        </div>

        <div className="rounded-lg border bg-card p-4 space-y-1">
          <div className="flex items-center gap-2 text-muted-foreground text-xs">
            <Hash className="h-3.5 w-3.5" aria-hidden="true" />
            {t("chainHead")}
          </div>
          <code className="font-hash text-xs break-all text-muted-foreground/80 block">
            {data.chain_head_hash}
          </code>
        </div>
      </div>

      {/* Integrity badge */}
      <div className="flex items-center gap-2">
        <span className="text-sm text-muted-foreground">{t("integrityLabel")}:</span>
        <Badge variant={data.valid ? "success" : "destructive"}>
          {data.valid ? t("integrityOK") : t("integrityFAIL")}
        </Badge>
      </div>
    </div>
  );
}
