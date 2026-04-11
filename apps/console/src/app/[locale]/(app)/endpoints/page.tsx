import { getTranslations } from "next-intl/server";
import { Suspense } from "react";
import { listEndpoints } from "@/lib/api/endpoints";
import { EndpointsClient } from "./endpoints-client";
import { Skeleton } from "@/components/ui/skeleton";
import type { EndpointStatus } from "@/lib/api/types";

interface EndpointsPageProps {
  searchParams: Promise<{ status?: string; page?: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("endpoints");
  return { title: t("title") };
}

export default async function EndpointsPage({
  searchParams,
}: EndpointsPageProps): Promise<JSX.Element> {
  const t = await getTranslations("endpoints");
  const params = await searchParams;

  const status = params.status as EndpointStatus | undefined;
  const page = params.page ? parseInt(params.page, 10) : 1;

  const endpoints = await listEndpoints({
    status,
    page,
    page_size: 50,
  }).catch(() => ({ items: [], pagination: { page: 1, page_size: 50, total: 0 } }));

  return (
    <div className="space-y-6 animate-fade-in">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">{t("title")}</h1>
          <p className="text-muted-foreground">{t("subtitle")}</p>
        </div>
      </div>

      <Suspense fallback={<Skeleton className="h-96 w-full" />}>
        <EndpointsClient
          initialData={endpoints}
          currentStatus={status}
          currentPage={page}
        />
      </Suspense>
    </div>
  );
}
