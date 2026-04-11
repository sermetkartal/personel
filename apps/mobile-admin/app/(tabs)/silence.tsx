/**
 * Silence (Flow 7) screen.
 * Shows heartbeat gap alerts from the last 24 hours.
 * Tapping a gap badge allows acknowledging it with a reason code.
 */

import React, { useState } from "react";
import {
  View,
  Text,
  FlatList,
  RefreshControl,
  ActivityIndicator,
  Alert,
  TextInput,
  Modal,
  TouchableOpacity,
} from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { useQuery } from "@tanstack/react-query";
import { silenceListQueryOptions, useAcknowledgeSilence } from "@/lib/api/silence";
import { SilenceBadge } from "@/components/silence-badge";
import { EmptyState } from "@/components/empty-state";
import { tr } from "@/lib/i18n/tr";
import type { SilenceGap } from "@/lib/api/types";

interface AcknowledgeModalProps {
  gap: SilenceGap | null;
  onClose: () => void;
}

function AcknowledgeModal({ gap, onClose }: AcknowledgeModalProps) {
  const [reason, setReason] = useState("");
  const acknowledge = useAcknowledgeSilence(gap?.endpoint_id ?? "");

  const handleConfirm = async () => {
    if (!gap) return;
    if (!reason.trim()) {
      Alert.alert("", tr.silence.acknowledgeReasonHint);
      return;
    }
    try {
      await acknowledge.mutateAsync({ reason: reason.trim() });
      setReason("");
      onClose();
    } catch {
      Alert.alert("Hata", tr.common.errorGeneric);
    }
  };

  return (
    <Modal
      visible={!!gap}
      transparent
      animationType="slide"
      onRequestClose={onClose}
    >
      <View className="flex-1 justify-end bg-black/40">
        <View className="bg-white rounded-t-3xl p-6">
          <Text className="text-slate-900 font-bold text-xl mb-1">
            {tr.silence.acknowledgeTitle}
          </Text>
          <Text className="text-slate-500 text-sm mb-4">
            {gap?.endpoint_hostname}
          </Text>

          <Text className="text-slate-700 text-sm font-medium mb-2">
            {tr.silence.acknowledgeReason}
          </Text>
          <TextInput
            value={reason}
            onChangeText={setReason}
            placeholder={tr.silence.acknowledgeReasonHint}
            className="border border-slate-200 rounded-xl px-4 py-3 text-slate-900 bg-slate-50 text-base"
            autoCapitalize="none"
            returnKeyType="done"
          />

          <View className="flex-row space-x-3 mt-6">
            <TouchableOpacity
              onPress={onClose}
              className="flex-1 border border-slate-200 rounded-xl py-3 items-center"
            >
              <Text className="text-slate-600 font-semibold">
                {tr.common.cancel}
              </Text>
            </TouchableOpacity>
            <TouchableOpacity
              onPress={() => void handleConfirm()}
              disabled={acknowledge.isPending}
              className={`flex-1 rounded-xl py-3 items-center ${
                acknowledge.isPending ? "bg-brand-300" : "bg-brand-500"
              }`}
            >
              <Text className="text-white font-semibold">
                {acknowledge.isPending
                  ? tr.common.saving
                  : tr.silence.acknowledgeConfirm}
              </Text>
            </TouchableOpacity>
          </View>
        </View>
      </View>
    </Modal>
  );
}

export default function SilenceScreen() {
  const { data, isLoading, isError, refetch, isFetching } = useQuery(
    silenceListQueryOptions(),
  );
  const [selectedGap, setSelectedGap] = useState<SilenceGap | null>(null);

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
          {tr.silence.title}
        </Text>
        <Text className="text-slate-500 text-sm mt-0.5">
          {tr.silence.subtitle}
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
            <TouchableOpacity
              className="px-4"
              onPress={() =>
                !item.acknowledged ? setSelectedGap(item) : undefined
              }
              activeOpacity={item.acknowledged ? 1 : 0.7}
              accessibilityRole={item.acknowledged ? "none" : "button"}
              accessibilityLabel={
                item.acknowledged
                  ? item.endpoint_hostname
                  : `${item.endpoint_hostname} - ${tr.silence.acknowledgeTitle}`
              }
            >
              <SilenceBadge gap={item} />
            </TouchableOpacity>
          )}
          contentContainerStyle={{
            paddingTop: 8,
            paddingBottom: 24,
            flexGrow: items.length === 0 ? 1 : undefined,
          }}
          ListEmptyComponent={
            <EmptyState message={tr.silence.noAlerts} />
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

      <AcknowledgeModal
        gap={selectedGap}
        onClose={() => setSelectedGap(null)}
      />
    </SafeAreaView>
  );
}
