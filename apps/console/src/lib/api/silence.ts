/**
 * Agent silence / heartbeat gap API query functions (Flow 7).
 */

import { apiClient } from "./client";
import type {
  DateRangeParams,
  PaginationParams,
  SilenceAcknowledge,
  SilenceList,
  SilenceTimeline,
} from "./types";

export interface ListSilenceParams extends PaginationParams {
  endpoint_id?: string;
  from?: string;
  to?: string;
}

export const silenceKeys = {
  all: ["silence"] as const,
  list: (params: ListSilenceParams) => ["silence", "list", params] as const,
  timeline: (endpointId: string, range: DateRangeParams) =>
    ["silence", "timeline", endpointId, range] as const,
};

export async function listSilenceGaps(
  params: ListSilenceParams = {},
): Promise<SilenceList> {
  const qs = apiClient.buildQuery(params);
  return apiClient.get<SilenceList>(`/v1/silence${qs}`);
}

export async function getSilenceTimeline(
  endpointId: string,
  range: DateRangeParams,
): Promise<SilenceTimeline> {
  const qs = apiClient.buildQuery(range);
  return apiClient.get<SilenceTimeline>(`/v1/silence/${endpointId}/timeline${qs}`);
}

export async function acknowledgeSilence(
  endpointId: string,
  req: SilenceAcknowledge,
): Promise<void> {
  return apiClient.post<void>(`/v1/silence/${endpointId}/acknowledge`, req);
}
