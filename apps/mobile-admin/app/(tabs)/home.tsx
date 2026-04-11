/**
 * Home screen — 4 summary cards pulling from /v1/mobile/summary.
 * TODO (backend-developer): Implement GET /v1/mobile/summary in mobile-bff.
 */

import React from "react";
import {
  View,
  Text,
  ScrollView,
  RefreshControl,
  ActivityIndicator,
} from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { summaryQueryOptions } from "@/lib/api/home";
import { SummaryCard } from "@/components/summary-card";
import { EmptyState } from "@/components/empty-state";
import { tr } from "@/lib/i18n/tr";
import { formatDateTime } from "@/lib/utils";
import { useSessionStore } from "@/lib/auth/session";
import { signOut } from "@/lib/auth/oidc";

export default function HomeScreen() {
  const { data, isLoading, isError, refetch, isFetching } = useQuery(
    summaryQueryOptions(),
  );

  const username = useSessionStore((s) => s.user?.username ?? "");

  const handleLogout = async () => {
    await signOut();
    router.replace("/sign-in");
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

  return (
    <SafeAreaView className="flex-1 bg-surface-secondary" edges={["top"]}>
      <ScrollView
        contentContainerStyle={{ paddingBottom: 24 }}
        refreshControl={
          <RefreshControl
            refreshing={isFetching && !isLoading}
            onRefresh={() => void refetch()}
            tintColor="#0ea5e9"
          />
        }
      >
        {/* Header */}
        <View className="px-4 pt-4 pb-2 flex-row items-center justify-between">
          <View>
            <Text className="text-slate-900 text-2xl font-bold">
              {tr.home.title}
            </Text>
            <Text className="text-slate-500 text-sm mt-0.5">
              {username ? `Hoş geldiniz, ${username}` : tr.home.subtitle}
            </Text>
          </View>
          <Text
            onPress={() => void handleLogout()}
            className="text-slate-500 text-sm"
            accessibilityRole="button"
          >
            {tr.auth.logout}
          </Text>
        </View>

        {/* Summary cards */}
        <View className="px-4 mt-4">
          <SummaryCard
            title={tr.home.pendingLiveViewApprovals}
            description={tr.home.pendingLiveViewApprovalsDesc}
            count={data.pending_live_view_approvals}
            accent={data.pending_live_view_approvals > 0 ? "warning" : "success"}
            onPress={() => router.push("/(tabs)/live-view")}
          />
          <SummaryCard
            title={tr.home.pendingDsrs}
            description={tr.home.pendingDsrsDesc}
            count={data.pending_dsrs}
            accent={data.pending_dsrs > 0 ? "danger" : "success"}
            onPress={() => router.push("/(tabs)/dsr")}
          />
          <SummaryCard
            title={tr.home.silenceAlerts}
            description={tr.home.silenceAlertsDesc}
            count={data.silence_alerts_24h}
            accent={data.silence_alerts_24h > 0 ? "warning" : "success"}
            onPress={() => router.push("/(tabs)/silence")}
          />
        </View>

        {/* Recent audit events */}
        <View className="px-4 mt-6">
          <View className="flex-row items-center justify-between mb-3">
            <Text className="text-slate-900 font-semibold text-base">
              {tr.home.recentAudit}
            </Text>
          </View>
          {data.recent_audit_entries.length === 0 ? (
            <EmptyState message={tr.common.noResults} />
          ) : (
            <View className="bg-white rounded-2xl shadow-sm border border-slate-100 overflow-hidden">
              {data.recent_audit_entries.map((entry, idx) => (
                <View
                  key={entry.id}
                  className={`px-4 py-3 ${
                    idx < data.recent_audit_entries.length - 1
                      ? "border-b border-slate-100"
                      : ""
                  }`}
                >
                  <Text
                    className="text-slate-700 text-sm font-medium"
                    numberOfLines={1}
                  >
                    {entry.action}
                  </Text>
                  <Text className="text-slate-400 text-xs mt-0.5">
                    {formatDateTime(entry.timestamp)}
                  </Text>
                </View>
              ))}
            </View>
          )}
        </View>
      </ScrollView>
    </SafeAreaView>
  );
}
