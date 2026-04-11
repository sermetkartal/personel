/**
 * Live view approvals list screen.
 * Shows pending HR-approval requests in a FlatList.
 * Tapping a row navigates to /live-view/[id] for detail + action.
 */

import React from "react";
import {
  View,
  Text,
  FlatList,
  RefreshControl,
  TouchableOpacity,
  ActivityIndicator,
} from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { pendingLiveViewQueryOptions } from "@/lib/api/live-view";
import { EmptyState } from "@/components/empty-state";
import { tr } from "@/lib/i18n/tr";
import { formatDateTime } from "@/lib/utils";
import type { LiveViewRequest } from "@/lib/api/types";

function LiveViewRow({ item }: { item: LiveViewRequest }) {
  return (
    <TouchableOpacity
      onPress={() => router.push(`/live-view/${item.id}`)}
      className="bg-white mx-4 rounded-2xl p-4 mb-3 shadow-sm border border-slate-100"
      activeOpacity={0.7}
      accessibilityRole="button"
      accessibilityLabel={`${item.endpoint_hostname} - ${item.reason_code}`}
    >
      <View className="flex-row items-start justify-between">
        <View className="flex-1 mr-2">
          <Text
            className="text-slate-900 font-semibold text-base"
            numberOfLines={1}
          >
            {item.endpoint_hostname}
          </Text>
          <Text className="text-slate-500 text-xs mt-0.5">
            {item.requester_username} · {item.duration_minutes}{" "}
            {tr.liveView.durationUnit}
          </Text>
        </View>
        <View className="bg-amber-100 rounded-lg px-2 py-1">
          <Text className="text-amber-700 text-xs font-medium">
            {tr.liveView.states[item.state] ?? item.state}
          </Text>
        </View>
      </View>
      <Text className="text-slate-400 text-xs mt-2">
        {formatDateTime(item.requested_at)}
      </Text>
    </TouchableOpacity>
  );
}

export default function LiveViewScreen() {
  const { data, isLoading, isError, refetch, isFetching } = useQuery(
    pendingLiveViewQueryOptions(),
  );

  if (isLoading) {
    return (
      <SafeAreaView className="flex-1 bg-surface-secondary items-center justify-center">
        <ActivityIndicator size="large" color="#0ea5e9" />
      </SafeAreaView>
    );
  }

  const items = data?.data ?? [];

  return (
    <SafeAreaView className="flex-1 bg-surface-secondary" edges={["top"]}>
      {/* Header */}
      <View className="px-4 pt-4 pb-2">
        <Text className="text-slate-900 text-2xl font-bold">
          {tr.liveView.title}
        </Text>
        <Text className="text-slate-500 text-sm mt-0.5">
          {tr.liveView.subtitle}
        </Text>
      </View>

      {isError ? (
        <EmptyState
          message={tr.common.errorGeneric}
          onRetry={() => void refetch()}
          retryLabel={tr.common.retry}
        />
      ) : (
        <FlatList
          data={items}
          keyExtractor={(item) => item.id}
          renderItem={({ item }) => <LiveViewRow item={item} />}
          contentContainerStyle={{
            paddingTop: 8,
            paddingBottom: 24,
            flexGrow: items.length === 0 ? 1 : undefined,
          }}
          ListEmptyComponent={
            <EmptyState message={tr.liveView.noRequests} />
          }
          refreshControl={
            <RefreshControl
              refreshing={isFetching && !isLoading}
              onRefresh={() => void refetch()}
              tintColor="#0ea5e9"
            />
          }
        />
      )}
    </SafeAreaView>
  );
}
