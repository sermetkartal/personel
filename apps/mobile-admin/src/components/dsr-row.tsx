import React from "react";
import { View, Text, TouchableOpacity } from "react-native";
import type { DSRRequest } from "@/lib/api/types";
import { tr } from "@/lib/i18n/tr";
import { daysUntil, formatDate } from "@/lib/utils";

interface DsrRowProps {
  dsr: DSRRequest;
  onPress: () => void;
}

function stateColor(state: DSRRequest["state"]): string {
  switch (state) {
    case "open":
      return "bg-brand-100 text-brand-700";
    case "at_risk":
      return "bg-warning-100 text-warning-600";
    case "overdue":
      return "bg-danger-100 text-danger-700";
    case "resolved":
      return "bg-success-100 text-success-600";
    case "rejected":
      return "bg-slate-100 text-slate-500";
    default:
      return "bg-slate-100 text-slate-500";
  }
}

export function DsrRow({ dsr, onPress }: DsrRowProps) {
  const days = daysUntil(dsr.extended_deadline ?? dsr.sla_deadline);
  const isOverdue = days < 0;

  const daysLabel = isOverdue
    ? tr.dsr.daysOverdue.replace("{days}", String(Math.abs(days)))
    : tr.dsr.daysRemaining.replace("{days}", String(days));

  const stateLabel =
    tr.dsr.states[dsr.state] ?? dsr.state;

  const typeLabel =
    tr.dsr.types[dsr.request_type] ?? dsr.request_type;

  return (
    <TouchableOpacity
      onPress={onPress}
      className="bg-white rounded-2xl p-4 mb-3 shadow-sm border border-slate-100"
      activeOpacity={0.7}
      accessibilityRole="button"
      accessibilityLabel={`${typeLabel} - ${stateLabel}`}
    >
      <View className="flex-row items-start justify-between mb-2">
        <View className="flex-1 mr-2">
          <Text className="text-slate-900 font-semibold text-base">
            {typeLabel}
          </Text>
          <Text className="text-slate-500 text-xs mt-0.5">
            {tr.dsr.submittedAt}: {formatDate(dsr.submitted_at)}
          </Text>
        </View>
        <View className={`rounded-lg px-2 py-1 ${stateColor(dsr.state).split(" ")[0]}`}>
          <Text className={`text-xs font-medium ${stateColor(dsr.state).split(" ")[1]}`}>
            {stateLabel}
          </Text>
        </View>
      </View>

      <View className="flex-row items-center justify-between">
        <Text
          className={`text-sm font-medium ${
            isOverdue ? "text-danger-600" : days <= 5 ? "text-warning-600" : "text-slate-500"
          }`}
        >
          {daysLabel}
        </Text>
        <Text className="text-slate-400 text-sm">›</Text>
      </View>
    </TouchableOpacity>
  );
}
