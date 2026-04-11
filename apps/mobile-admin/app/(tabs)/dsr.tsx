/**
 * DSR queue screen.
 * Shows "open", "at_risk", and "overdue" DSRs.
 * Tapping a row navigates to /dsr/[id] for detail + respond action.
 */

import React from "react";
import {
  View,
  Text,
  FlatList,
  RefreshControl,
  ActivityIndicator,
} from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { dsrQueueQueryOptions } from "@/lib/api/dsr";
import { DsrRow } from "@/components/dsr-row";
import { EmptyState } from "@/components/empty-state";
import { tr } from "@/lib/i18n/tr";
import type { DSRRequest } from "@/lib/api/types";

export default function DsrScreen() {
  const { data, isLoading, isError, refetch, isFetching } = useQuery(
    dsrQueueQueryOptions(),
  );

  if (isLoading) {
    return (
      <SafeAreaView className="flex-1 bg-surface-secondary items-center justify-center">
        <ActivityIndicator size="large" color="#0ea5e9" />
      </SafeAreaView>
    );
  }

  // Filter to actionable states on client side as belt-and-suspenders
  const items: DSRRequest[] = (data?.data ?? []).filter((d) =>
    ["open", "at_risk", "overdue"].includes(d.state),
  );

  return (
    <SafeAreaView className="flex-1 bg-surface-secondary" edges={["top"]}>
      {/* Header */}
      <View className="px-4 pt-4 pb-2">
        <Text className="text-slate-900 text-2xl font-bold">
          {tr.dsr.title}
        </Text>
        <Text className="text-slate-500 text-sm mt-0.5">
          {tr.dsr.subtitle}
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
          renderItem={({ item }) => (
            <View className="px-4">
              <DsrRow
                dsr={item}
                onPress={() => router.push(`/dsr/${item.id}`)}
              />
            </View>
          )}
          contentContainerStyle={{
            paddingTop: 8,
            paddingBottom: 24,
            flexGrow: items.length === 0 ? 1 : undefined,
          }}
          ListEmptyComponent={
            <EmptyState message={tr.dsr.noPendingDsrs} />
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
