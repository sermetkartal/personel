"use client";

import { useQuery } from "@tanstack/react-query";
import { listAuditRecords, auditKeys } from "@/lib/api/audit";
import type { ListAuditParams } from "@/lib/api/audit";
import type { AuditList } from "@/lib/api/types";

export function useAuditLog(
  params: ListAuditParams = {},
  initialData?: AuditList,
): {
  data: AuditList | undefined;
  isLoading: boolean;
  isError: boolean;
  isFetching: boolean;
  refetch: () => void;
} {
  const query = useQuery({
    queryKey: auditKeys.list(params),
    queryFn: () => listAuditRecords(params),
    staleTime: 10_000,
    initialData,
  });

  return {
    data: query.data,
    isLoading: query.isLoading,
    isError: query.isError,
    isFetching: query.isFetching,
    refetch: query.refetch,
  };
}
