/**
 * Live view request detail screen.
 * Fetches the full request, shows all metadata, and renders ApprovalCard
 * with the dual-control guard.
 *
 * Optimistic UI: on approve/reject, the card enters a loading state
 * and the list is invalidated on settlement.
 *
 * Dual-control: ApprovalCard disables the Approve button when
 * currentUserId === request.requester_id (client-side guard).
 * The server enforces the same invariant independently.
 */

import React, { useState } from "react";
import {
  View,
  Text,
  ScrollView,
  ActivityIndicator,
  Alert,
} from "react-native";
import { useLocalSearchParams, router } from "expo-router";
import { SafeAreaView } from "react-native-safe-area-context";
import { useQuery } from "@tanstack/react-query";
import { liveViewDetailQueryOptions } from "@/lib/api/live-view";
import { useApproveLiveView, useRejectLiveView } from "@/hooks/use-live-view";
import { ApprovalCard } from "@/components/approval-card";
import { EmptyState } from "@/components/empty-state";
import { useSessionStore } from "@/lib/auth/session";
import { tr } from "@/lib/i18n/tr";
import { formatDateTime } from "@/lib/utils";

export default function LiveViewDetailScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const currentUserId = useSessionStore((s) => s.user?.sub ?? "");

  const { data, isLoading, isError, refetch } = useQuery(
    liveViewDetailQueryOptions(id),
  );

  const approve = useApproveLiveView(id);
  const reject = useRejectLiveView(id);

  const [isDone, setIsDone] = useState(false);

  const handleApprove = async (requestId: string) => {
    try {
      await approve.mutateAsync({});
      setIsDone(true);
      Alert.alert("", tr.liveView.approveSuccess, [
        { text: tr.common.close, onPress: () => router.back() },
      ]);
    } catch (error) {
      const msg =
        error instanceof Error ? error.message : tr.errors.approvalFailed;
      Alert.alert("Hata", msg);
    }
  };

  const handleReject = async (requestId: string, reason?: string) => {
    try {
      await reject.mutateAsync({ reason });
      setIsDone(true);
      Alert.alert("", tr.liveView.rejectSuccess, [
        { text: tr.common.close, onPress: () => router.back() },
      ]);
    } catch (error) {
      const msg =
        error instanceof Error ? error.message : tr.errors.rejectFailed;
      Alert.alert("Hata", msg);
    }
  };

  if (isLoading) {
    return (
      <SafeAreaView className="flex-1 bg-surface-secondary items-center justify-center">
        <ActivityIndicator size="large" color="#0ea5e9" />
      </SafeAreaView>
    );
  }

  if (isError || !data) {
    return (
      <SafeAreaView className="flex-1 bg-surface-secondary">
        <EmptyState
          message={tr.common.errorGeneric}
          onRetry={() => void refetch()}
          retryLabel={tr.common.retry}
        />
      </SafeAreaView>
    );
  }

  const isPendingApproval = data.state === "REQUESTED";

  return (
    <SafeAreaView className="flex-1 bg-surface-secondary" edges={["bottom"]}>
      <ScrollView contentContainerStyle={{ padding: 16, paddingBottom: 32 }}>
        {/* Status badge */}
        <View className="flex-row mb-4">
          <View
            className={`rounded-xl px-3 py-1 ${
              isPendingApproval ? "bg-amber-100" : "bg-slate-100"
            }`}
          >
            <Text
              className={`text-sm font-semibold ${
                isPendingApproval ? "text-amber-700" : "text-slate-600"
              }`}
            >
              {tr.liveView.states[data.state] ?? data.state}
            </Text>
          </View>
        </View>

        {/* Metadata */}
        <View className="bg-white rounded-2xl p-4 shadow-sm border border-slate-100 mb-4">
          <Text className="text-slate-900 font-semibold text-base mb-3">
            {tr.common.details}
          </Text>
          {[
            {
              label: tr.liveView.targetEndpoint,
              value: data.endpoint_hostname,
            },
            { label: tr.liveView.requester, value: data.requester_username },
            { label: tr.liveView.reasonCode, value: data.reason_code },
            {
              label: tr.liveView.duration,
              value: `${data.duration_minutes} ${tr.liveView.durationUnit}`,
            },
            {
              label: tr.liveView.requestedAt,
              value: formatDateTime(data.requested_at),
            },
          ].map(({ label, value }) => (
            <View key={label} className="flex-row py-1.5 border-b border-slate-50 last:border-0">
              <Text className="text-slate-500 text-sm w-32">{label}:</Text>
              <Text className="text-slate-800 text-sm flex-1" numberOfLines={2}>
                {value}
              </Text>
            </View>
          ))}
        </View>

        {/* KVKK notice */}
        <View className="bg-blue-50 border border-blue-100 rounded-2xl p-4 mb-4">
          <Text className="text-blue-800 text-xs leading-5">
            {tr.liveView.kvkkNotice}
          </Text>
          <Text className="text-blue-700 text-xs mt-1 font-medium">
            {tr.liveView.dualControlNote}
          </Text>
        </View>

        {/* Approval card — only shown for REQUESTED state */}
        {isPendingApproval && !isDone && (
          <ApprovalCard
            request={data}
            currentUserId={currentUserId}
            onApprove={handleApprove}
            onReject={handleReject}
            isApproving={approve.isPending}
            isRejecting={reject.isPending}
          />
        )}
      </ScrollView>
    </SafeAreaView>
  );
}
