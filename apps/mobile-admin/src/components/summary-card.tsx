import React from "react";
import { View, Text, TouchableOpacity } from "react-native";

interface SummaryCardProps {
  title: string;
  description: string;
  count: number;
  accent?: "brand" | "warning" | "danger" | "success";
  onPress?: () => void;
}

const accentClasses: Record<NonNullable<SummaryCardProps["accent"]>, string> = {
  brand: "bg-brand-500",
  warning: "bg-warning-500",
  danger: "bg-danger-500",
  success: "bg-success-500",
};

const accentTextClasses: Record<NonNullable<SummaryCardProps["accent"]>, string> = {
  brand: "text-brand-700",
  warning: "text-warning-600",
  danger: "text-danger-700",
  success: "text-success-600",
};

export function SummaryCard({
  title,
  description,
  count,
  accent = "brand",
  onPress,
}: SummaryCardProps) {
  const Wrapper = onPress ? TouchableOpacity : View;

  return (
    <Wrapper
      onPress={onPress}
      className="bg-white rounded-2xl p-4 mb-3 shadow-sm border border-slate-100 flex-row items-center"
      activeOpacity={onPress ? 0.7 : 1}
    >
      <View
        className={`w-12 h-12 rounded-xl ${accentClasses[accent]} items-center justify-center mr-4`}
      >
        <Text className="text-white text-xl font-bold">
          {count > 99 ? "99+" : String(count)}
        </Text>
      </View>
      <View className="flex-1">
        <Text className="text-slate-900 font-semibold text-base">{title}</Text>
        <Text className="text-slate-500 text-sm mt-0.5">{description}</Text>
      </View>
      {onPress && (
        <Text className={`text-sm font-medium ${accentTextClasses[accent]}`}>
          ›
        </Text>
      )}
    </Wrapper>
  );
}
