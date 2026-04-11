"use client";

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  listLiveViewRequests,
  listLiveViewSessions,
  requestLiveView,
  approveLiveView,
  rejectLiveView,
  endLiveViewSession,
  terminateLiveViewSession,
  liveViewKeys,
} from "@/lib/api/liveview";
import type {
  ListLiveViewRequestsParams,
  ListLiveViewSessionsParams,
} from "@/lib/api/liveview";
import type { LiveViewCreate } from "@/lib/api/types";

export function useLiveViewRequests(params: ListLiveViewRequestsParams = {}) {
  return useQuery({
    queryKey: liveViewKeys.requests(params),
    queryFn: () => listLiveViewRequests(params),
    refetchInterval: 10_000, // poll every 10s for HR approval queue
  });
}

export function useLiveViewSessions(params: ListLiveViewSessionsParams = {}) {
  return useQuery({
    queryKey: liveViewKeys.sessions(params),
    queryFn: () => listLiveViewSessions(params),
    refetchInterval: 5_000, // poll for active sessions
  });
}

export function useRequestLiveView() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: LiveViewCreate) => requestLiveView(req),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: liveViewKeys.all });
    },
  });
}

export function useApproveLiveView() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      requestId,
      notes,
    }: {
      requestId: string;
      notes?: string;
    }) => approveLiveView(requestId, { notes }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: liveViewKeys.all });
    },
  });
}

export function useRejectLiveView() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      requestId,
      reason,
    }: {
      requestId: string;
      reason: string;
    }) => rejectLiveView(requestId, { reason }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: liveViewKeys.all });
    },
  });
}

export function useEndLiveViewSession() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (sessionId: string) => endLiveViewSession(sessionId),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: liveViewKeys.all });
    },
  });
}

export function useTerminateLiveViewSession() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      sessionId,
      reason,
    }: {
      sessionId: string;
      reason: string;
    }) => terminateLiveViewSession(sessionId, { reason }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: liveViewKeys.all });
    },
  });
}
