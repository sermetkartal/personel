/**
 * ApprovalCard — live view request card with dual-control guard.
 *
 * Dual-control invariant (ADR 0019 + KVKK dual-control pattern):
 * The Approve button is DISABLED and shows a tooltip when the
 * authenticated user is the requester (caller === requester_id).
 *
 * The server ALSO enforces this invariant on POST /v1/live-view/requests/{id}/approve.
 * The UI guard provides immediate feedback before the network call.
 *
 * Turkish string: "Kendi talebinizi onaylayamazsınız"
 */

import React, { useState } from "react";
import {
  View,
  Text,
  TouchableOpacity,
  Alert,
  ActivityIndicator,
} from "react-native";
import * as Haptics from "expo-haptics";
import type { LiveViewRequest } from "@/lib/api/types";
import { tr } from "@/lib/i18n/tr";
import { formatDateTime } from "@/lib/utils";

interface ApprovalCardProps {
  request: LiveViewRequest;
  currentUserId: string;
  onApprove: (requestId: string) => Promise<void>;
  onReject: (requestId: string, reason?: string) => Promise<void>;
  isApproving?: boolean;
  isRejecting?: boolean;
}

export function ApprovalCard({
  request,
  currentUserId,
  onApprove,
  onReject,
  isApproving = false,
  isRejecting = false,
}: ApprovalCardProps) {
  const [showSelfApprovalTooltip, setShowSelfApprovalTooltip] = useState(false);

  // Dual-control guard: disable approve if caller === requester
  const isSelfApproval = request.requester_id === currentUserId;
  const isLoading = isApproving || isRejecting;

  const handleApprovePress = async () => {
    if (isSelfApproval) {
      // Show tooltip explaining the dual-control constraint
      setShowSelfApprovalTooltip(true);
      setTimeout(() => setShowSelfApprovalTooltip(false), 3000);
      await Haptics.notificationAsync(Haptics.NotificationFeedbackType.Warning);
      return;
    }

    Alert.alert(
      tr.liveView.approve,
      tr.liveView.approveConfirm,
      [
        { text: tr.common.cancel, style: "cancel" },
        {
          text: tr.liveView.approve,
          style: "default",
          onPress: async () => {
            await Haptics.notificationAsync(
              Haptics.NotificationFeedbackType.Success,
            );
            await onApprove(request.id);
          },
        },
      ],
    );
  };

  const handleRejectPress = () => {
    Alert.prompt(
      tr.liveView.reject,
      tr.liveView.rejectConfirm,
      [
        { text: tr.common.cancel, style: "cancel" },
        {
          text: tr.liveView.reject,
          style: "destructive",
          onPress: async (reason?: string) => {
            await Haptics.notificationAsync(
              Haptics.NotificationFeedbackType.Warning,
            );
            await onReject(request.id, reason);
          },
        },
      ],
      "plain-text",
      "",
      "default",
    );
  };

  return (
    <View className="bg-white rounded-2xl p-4 mb-3 shadow-sm border border-slate-100">
      {/* Header */}
      <View className="flex-row items-start justify-between mb-3">
        <View className="flex-1 mr-2">
          <Text
            className="text-slate-900 font-semibold text-base"
            numberOfLines={1}
          >
            {request.endpoint_hostname}
          </Text>
          <Text className="text-slate-500 text-xs mt-0.5">
            {formatDateTime(request.requested_at)}
          </Text>
        </View>
        <View className="bg-amber-100 rounded-lg px-2 py-1">
          <Text className="text-amber-700 text-xs font-medium">
            {tr.liveView.states[request.state] ?? request.state}
          </Text>
        </View>
      </View>

      {/* Details */}
      <View className="mb-3 space-y-1">
        <View className="flex-row">
          <Text className="text-slate-500 text-sm w-28">
            {tr.liveView.requester}:
          </Text>
          <Text className="text-slate-700 text-sm flex-1" numberOfLines={1}>
            {request.requester_username}
          </Text>
        </View>
        <View className="flex-row">
          <Text className="text-slate-500 text-sm w-28">
            {tr.liveView.reasonCode}:
          </Text>
          <Text className="text-slate-700 text-sm flex-1" numberOfLines={1}>
            {request.reason_code}
          </Text>
        </View>
        <View className="flex-row">
          <Text className="text-slate-500 text-sm w-28">
            {tr.liveView.duration}:
          </Text>
          <Text className="text-slate-700 text-sm">
            {request.duration_minutes} {tr.liveView.durationUnit}
          </Text>
        </View>
      </View>

      {/* Self-approval tooltip */}
      {showSelfApprovalTooltip && (
        <View className="bg-amber-50 border border-amber-200 rounded-lg p-2 mb-2">
          <Text className="text-amber-800 text-xs">
            {tr.liveView.selfApprovalTooltip}
          </Text>
        </View>
      )}

      {/* Action buttons */}
      <View className="flex-row space-x-2">
        {/* Approve button — disabled for self-approval */}
        <TouchableOpacity
          onPress={handleApprovePress}
          disabled={isLoading}
          className={`flex-1 rounded-xl py-3 items-center ${
            isSelfApproval
              ? "bg-slate-100"
              : isLoading
                ? "bg-success-100"
                : "bg-success-500"
          }`}
          accessibilityLabel={
            isSelfApproval
              ? tr.liveView.selfApprovalBlocked
              : tr.liveView.approve
          }
          accessibilityRole="button"
          accessibilityState={{ disabled: isSelfApproval || isLoading }}
        >
          {isApproving ? (
            <ActivityIndicator size="small" color="#16a34a" />
          ) : (
            <Text
              className={`font-semibold text-sm ${
                isSelfApproval ? "text-slate-400" : "text-white"
              }`}
            >
              {isSelfApproval
                ? tr.liveView.selfApprovalBlocked
                : tr.liveView.approve}
            </Text>
          )}
        </TouchableOpacity>

        {/* Reject button */}
        <TouchableOpacity
          onPress={handleRejectPress}
          disabled={isLoading}
          className={`flex-1 rounded-xl py-3 items-center border ${
            isLoading
              ? "border-slate-200 bg-slate-50"
              : "border-danger-200 bg-danger-50"
          }`}
          accessibilityLabel={tr.liveView.reject}
          accessibilityRole="button"
          accessibilityState={{ disabled: isLoading }}
        >
          {isRejecting ? (
            <ActivityIndicator size="small" color="#dc2626" />
          ) : (
            <Text
              className={`font-semibold text-sm ${
                isLoading ? "text-slate-400" : "text-danger-600"
              }`}
            >
              {tr.liveView.reject}
            </Text>
          )}
        </TouchableOpacity>
      </View>
    </View>
  );
}
