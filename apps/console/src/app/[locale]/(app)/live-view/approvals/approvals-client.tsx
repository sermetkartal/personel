"use client";

import { useTranslations } from "next-intl";
import { useLiveViewRequests } from "@/lib/hooks/use-live-view";
import { ApprovalCard } from "@/components/live-view/approval-card";
import { Skeleton } from "@/components/ui/skeleton";
import { AlertTriangle } from "lucide-react";

interface LiveViewApprovalsClientProps {
  currentUserId: string;
}

export function LiveViewApprovalsClient({
  currentUserId,
}: LiveViewApprovalsClientProps): JSX.Element {
  const t = useTranslations("liveView.approval");

  const { data, isLoading, refetch } = useLiveViewRequests({
    state: "REQUESTED",
    page_size: 50,
  });

  if (isLoading) {
    return (
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {Array.from({ length: 3 }).map((_, i) => (
          <Skeleton key={i} className="h-56 w-full rounded-lg" />
        ))}
      </div>
    );
  }

  const requests = data?.items ?? [];

  if (requests.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-12 text-center">
        <AlertTriangle className="mb-3 h-8 w-8 text-muted-foreground/50" aria-hidden="true" />
        <p className="font-medium">{t("noRequests")}</p>
        <p className="mt-1 text-sm text-muted-foreground">
          Onay bekleyen canlı izleme talebi bulunmamaktadır.
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <p className="text-sm text-muted-foreground">
        {requests.length} talep onay bekliyor.
      </p>
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {requests.map((request) => (
          <ApprovalCard
            key={request.id}
            request={request}
            currentUserId={currentUserId}
            onActionComplete={() => void refetch()}
          />
        ))}
      </div>
    </div>
  );
}
