import React from "react";
import { View, Text, TouchableOpacity } from "react-native";

interface EmptyStateProps {
  message: string;
  onRetry?: () => void;
  retryLabel?: string;
}

export function EmptyState({ message, onRetry, retryLabel = "Yenile" }: EmptyStateProps) {
  return (
    <View className="flex-1 items-center justify-center py-16 px-8">
      <View className="w-16 h-16 rounded-full bg-slate-100 items-center justify-center mb-4">
        <Text className="text-3xl">📭</Text>
      </View>
      <Text className="text-slate-500 text-base text-center">{message}</Text>
      {onRetry && (
        <TouchableOpacity
          onPress={onRetry}
          className="mt-4 bg-brand-500 rounded-xl px-6 py-2"
          accessibilityRole="button"
        >
          <Text className="text-white font-semibold text-sm">{retryLabel}</Text>
        </TouchableOpacity>
      )}
    </View>
  );
}
