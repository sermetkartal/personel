"use client";

import { useSearchParams } from "next/navigation";
import { useTranslations } from "next-intl";
import { FileText } from "lucide-react";
import { useAuditLog } from "@/lib/hooks/use-audit-log";
import { AuditFilterBar } from "@/components/audit/filter-bar";
import { AuditChainStatus } from "@/components/audit/chain-status";
import { EventRow } from "@/components/audit/event-row";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import type { AuditList } from "@/lib/api/types";
import { useRouter, usePathname } from "next/navigation";
import { useCallback } from "react";

interface AuditClientProps {
  initialData: AuditList;
}

export function AuditClient({ initialData }: AuditClientProps): JSX.Element {
  const t = useTranslations("audit");
  const searchParams = useSearchParams();
  const router = useRouter();
  const pathname = usePathname();

  const page = Number(searchParams.get("page") ?? "1");
  const action = searchParams.get("action") ?? undefined;
  const actorId = searchParams.get("actor_id") ?? undefined;
  const from = searchParams.get("from") ?? undefined;
  const to = searchParams.get("to") ?? undefined;

  const { data, isLoading, isError, refetch, isFetching } = useAuditLog(
    {
      page,
      page_size: 50,
      action,
      actor_id: actorId,
      from,
      to,
    },
    initialData,
  );

  const records = data?.items ?? [];
  const pagination = data?.pagination;

  const setPage = useCallback(
    (p: number) => {
      const params = new URLSearchParams(searchParams.toString());
      params.set("page", String(p));
      router.replace(`${pathname}?${params.toString()}`);
    },
    [router, pathname, searchParams],
  );

  return (
    <div className="space-y-4">
      {/* Chain integrity indicator */}
      <AuditChainStatus compact />

      {/* Filter bar */}
      <AuditFilterBar
        isRefetching={isFetching && !isLoading}
        onRefetch={() => void refetch()}
      />

      {/* Record count */}
      {pagination && (
        <p className="text-xs text-muted-foreground">
          {pagination.total.toLocaleString("tr-TR")} {t("totalRecords")} · {t("page")} {pagination.page}
        </p>
      )}

      {/* Records list */}
      <div
        className="rounded-lg border bg-card"
        role="list"
        aria-label={t("recordsLabel")}
        aria-busy={isLoading}
      >
        {isLoading ? (
          <div className="divide-y">
            {Array.from({ length: 10 }).map((_, i) => (
              <div key={i} className="flex items-center gap-3 px-4 py-3">
                <Skeleton className="h-4 w-4 rounded" />
                <Skeleton className="h-4 w-12" />
                <Skeleton className="h-5 w-28 rounded-full" />
                <Skeleton className="h-4 flex-1" />
                <Skeleton className="h-4 w-24" />
              </div>
            ))}
          </div>
        ) : isError ? (
          <div className="flex flex-col items-center justify-center py-12 text-center">
            <FileText className="mb-3 h-8 w-8 text-muted-foreground/50" aria-hidden="true" />
            <p className="font-medium">{t("errorLoadingRecords")}</p>
            <Button
              variant="ghost"
              size="sm"
              className="mt-2"
              onClick={() => void refetch()}
            >
              {t("retry")}
            </Button>
          </div>
        ) : records.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-12 text-center">
            <FileText className="mb-3 h-8 w-8 text-muted-foreground/50" aria-hidden="true" />
            <p className="font-medium">{t("noRecords")}</p>
            <p className="mt-1 text-sm text-muted-foreground">{t("noRecordsHint")}</p>
          </div>
        ) : (
          <div role="list">
            {records.map((record) => (
              <div key={record.id} role="listitem">
                <EventRow record={record} />
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Pagination */}
      {pagination && pagination.total > pagination.page_size && (
        <div className="flex items-center justify-between gap-4" role="navigation" aria-label={t("pagination")}>
          <Button
            variant="outline"
            size="sm"
            disabled={page <= 1}
            onClick={() => setPage(page - 1)}
          >
            {t("prevPage")}
          </Button>
          <span className="text-sm text-muted-foreground">
            {t("pageOf", { page, total: Math.ceil(pagination.total / pagination.page_size) })}
          </span>
          <Button
            variant="outline"
            size="sm"
            disabled={page >= Math.ceil(pagination.total / pagination.page_size)}
            onClick={() => setPage(page + 1)}
          >
            {t("nextPage")}
          </Button>
        </div>
      )}
    </div>
  );
}
