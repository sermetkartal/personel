import React from "react";
import { View, Text } from "react-native";
import type { SilenceGap } from "@/lib/api/types";
import { tr } from "@/lib/i18n/tr";
import { formatDateTime, formatDurationSeconds } from "@/lib/utils";

interface SilenceBadgeProps {
  gap: SilenceGap;
}

export function SilenceBadge({ gap }: SilenceBadgeProps) {
  const duration =
    gap.duration_seconds !== undefined
      ? formatDurationSeconds(gap.duration_seconds)
      : "—";

  return (
    <View className="bg-white rounded-2xl p-4 mb-3 shadow-sm border border-slate-100">
      <View className="flex-row items-start justify-between mb-2">
        <Text
          className="text-slate-900 font-semibold text-base flex-1 mr-2"
          numberOfLines={1}
        >
          {gap.endpoint_hostname}
        </Text>
        <View
          className={`rounded-lg px-2 py-1 ${
            gap.acknowledged ? "bg-success-100" : "bg-warning-100"
          }`}
        >
          <Text
            className={`text-xs font-medium ${
              gap.acknowledged ? "text-success-600" : "text-warning-700"
            }`}
          >
            {gap.acknowledged
              ? tr.silence.acknowledged
              : tr.silence.notAcknowledged}
          </Text>
        </View>
      </View>

      <View className="space-y-1">
        <View className="flex-row">
          <Text className="text-slate-500 text-sm w-28">
            {tr.silence.gapStarted}:
          </Text>
          <Text className="text-slate-700 text-sm flex-1">
            {formatDateTime(gap.started_at)}
          </Text>
        </View>
        {gap.ended_at && (
          <View className="flex-row">
            <Text className="text-slate-500 text-sm w-28">
              {tr.silence.gapEnded}:
            </Text>
            <Text className="text-slate-700 text-sm flex-1">
              {formatDateTime(gap.ended_at)}
            </Text>
          </View>
        )}
        <View className="flex-row">
          <Text className="text-slate-500 text-sm w-28">
            {tr.silence.duration}:
          </Text>
          <Text className="text-slate-700 text-sm">{duration}</Text>
        </View>
        {gap.reason && (
          <View className="flex-row">
            <Text className="text-slate-500 text-sm w-28">
              {tr.silence.reason}:
            </Text>
            <Text className="text-slate-700 text-sm flex-1">{gap.reason}</Text>
          </View>
        )}
      </View>
    </View>
  );
}
