/**
 * DSR detail screen.
 * Shows full DSR metadata and a "Respond" action (mobile triage scope).
 * Respond action marks the DSR resolved with an artifact reference.
 *
 * Out of scope on mobile: assign, extend, reject, erase — use web console.
 */

import React, { useState } from "react";
import {
  View,
  Text,
  ScrollView,
  ActivityIndicator,
  TextInput,
  TouchableOpacity,
  Alert,
  KeyboardAvoidingView,
  Platform,
} from "react-native";
import { useLocalSearchParams, router } from "expo-router";
import { SafeAreaView } from "react-native-safe-area-context";
import { useQuery } from "@tanstack/react-query";
import { dsrDetailQueryOptions } from "@/lib/api/dsr";
import { useRespondDSR } from "@/hooks/use-dsr";
import { EmptyState } from "@/components/empty-state";
import { tr } from "@/lib/i18n/tr";
import { formatDate, daysUntil } from "@/lib/utils";

export default function DsrDetailScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();

  const { data, isLoading, isError, refetch } = useQuery(
    dsrDetailQueryOptions(id),
  );
  const respond = useRespondDSR(id);

  const [artifactRef, setArtifactRef] = useState("");
  const [notes, setNotes] = useState("");
  const [isDone, setIsDone] = useState(false);

  const handleRespond = async () => {
    if (!artifactRef.trim()) {
      Alert.alert("", tr.dsr.artifactNote);
      return;
    }
    try {
      await respond.mutateAsync({
        artifact_ref: artifactRef.trim(),
        notes: notes.trim() || undefined,
      });
      setIsDone(true);
      Alert.alert("", tr.dsr.respondSuccess, [
        { text: tr.common.close, onPress: () => router.back() },
      ]);
    } catch (error) {
      const msg =
        error instanceof Error ? error.message : tr.errors.respondFailed;
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

  const deadline = data.extended_deadline ?? data.sla_deadline;
  const days = daysUntil(deadline);
  const isOverdue = days < 0;
  const isActionable = ["open", "at_risk", "overdue"].includes(data.state);

  const daysLabel = isOverdue
    ? tr.dsr.daysOverdue.replace("{days}", String(Math.abs(days)))
    : tr.dsr.daysRemaining.replace("{days}", String(days));

  const typeLabel = tr.dsr.types[data.request_type] ?? data.request_type;
  const stateLabel = tr.dsr.states[data.state] ?? data.state;

  return (
    <SafeAreaView className="flex-1 bg-surface-secondary" edges={["bottom"]}>
      <KeyboardAvoidingView
        behavior={Platform.OS === "ios" ? "padding" : "height"}
        style={{ flex: 1 }}
      >
        <ScrollView
          contentContainerStyle={{ padding: 16, paddingBottom: 40 }}
          keyboardShouldPersistTaps="handled"
        >
          {/* State + SLA */}
          <View className="flex-row items-center space-x-2 mb-4">
            <View className="bg-slate-100 rounded-xl px-3 py-1">
              <Text className="text-slate-700 text-sm font-semibold">
                {stateLabel}
              </Text>
            </View>
            <View
              className={`rounded-xl px-3 py-1 ${
                isOverdue
                  ? "bg-danger-100"
                  : days <= 5
                    ? "bg-warning-100"
                    : "bg-slate-100"
              }`}
            >
              <Text
                className={`text-sm font-semibold ${
                  isOverdue
                    ? "text-danger-700"
                    : days <= 5
                      ? "text-warning-700"
                      : "text-slate-600"
                }`}
              >
                {daysLabel}
              </Text>
            </View>
          </View>

          {/* Metadata */}
          <View className="bg-white rounded-2xl p-4 shadow-sm border border-slate-100 mb-4">
            <Text className="text-slate-900 font-semibold text-base mb-3">
              {tr.common.details}
            </Text>
            {[
              { label: tr.dsr.requestType, value: typeLabel },
              { label: tr.dsr.submittedAt, value: formatDate(data.submitted_at) },
              { label: tr.dsr.slaDeadline, value: formatDate(deadline) },
            ].map(({ label, value }) => (
              <View
                key={label}
                className="flex-row py-1.5 border-b border-slate-50 last:border-0"
              >
                <Text className="text-slate-500 text-sm w-32">{label}:</Text>
                <Text className="text-slate-800 text-sm flex-1">
                  {value}
                </Text>
              </View>
            ))}
          </View>

          {/* Respond form */}
          {isActionable && !isDone && (
            <View className="bg-white rounded-2xl p-4 shadow-sm border border-slate-100">
              <Text className="text-slate-900 font-semibold text-base mb-1">
                {tr.dsr.respondTitle}
              </Text>
              <Text className="text-slate-500 text-xs mb-4 leading-5">
                {tr.dsr.artifactNote}
              </Text>

              <Text className="text-slate-700 text-sm font-medium mb-1">
                {tr.dsr.artifactLabel}
              </Text>
              <TextInput
                value={artifactRef}
                onChangeText={setArtifactRef}
                placeholder={tr.dsr.artifactPlaceholder}
                className="border border-slate-200 rounded-xl px-4 py-3 text-slate-900 bg-slate-50 text-sm mb-3"
                autoCapitalize="none"
                autoCorrect={false}
              />

              <Text className="text-slate-700 text-sm font-medium mb-1">
                {tr.dsr.notesLabel}
              </Text>
              <TextInput
                value={notes}
                onChangeText={setNotes}
                placeholder={tr.dsr.notesLabel}
                className="border border-slate-200 rounded-xl px-4 py-3 text-slate-900 bg-slate-50 text-sm mb-4"
                multiline
                numberOfLines={3}
                textAlignVertical="top"
                style={{ minHeight: 72 }}
              />

              <TouchableOpacity
                onPress={() => void handleRespond()}
                disabled={respond.isPending}
                className={`rounded-xl py-3 items-center ${
                  respond.isPending ? "bg-success-200" : "bg-success-500"
                }`}
                accessibilityRole="button"
              >
                <Text className="text-white font-semibold">
                  {respond.isPending ? tr.dsr.responding : tr.dsr.respond}
                </Text>
              </TouchableOpacity>
            </View>
          )}
        </ScrollView>
      </KeyboardAvoidingView>
    </SafeAreaView>
  );
}
