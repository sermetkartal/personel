/**
 * DSR hooks — thin wrappers over TanStack Query options
 * defined in src/lib/api/dsr.ts.
 */

import { useQuery } from "@tanstack/react-query";
import {
  dsrQueueQueryOptions,
  dsrDetailQueryOptions,
  useRespondDSR,
} from "@/lib/api/dsr";

export function useDsrQueue() {
  return useQuery(dsrQueueQueryOptions());
}

export function useDsrDetail(id: string) {
  return useQuery(dsrDetailQueryOptions(id));
}

// Re-export mutation for convenience
export { useRespondDSR };
