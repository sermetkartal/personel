/**
 * Push notification token registration.
 *
 * KVKK push payload contract:
 * Notifications MUST NOT contain PII. The mobile-bff sanitizes all push
 * payloads to: { type, count, deep_link }. No employee names, no endpoint
 * IDs, no DSR content. See src/lib/api/types.ts PushNotificationPayload.
 *
 * Token registration flow:
 * 1. Request permission from the OS
 * 2. Obtain Expo push token from Expo's push service
 *    (Expo acts as a relay to APNs/FCM; no raw APNs/FCM tokens are used)
 * 3. POST the token to mobile-bff /v1/mobile/push-tokens
 *    so the BFF can send targeted pushes to this device.
 *
 * TODO (backend-developer): Implement POST /v1/mobile/push-tokens in mobile-bff.
 * Request body: { token: string, platform: "ios"|"android", device_id: string }
 */

import * as Notifications from "expo-notifications";
import * as Device from "expo-device";
import { Platform } from "react-native";
import { apiPost } from "@/lib/api/client";
import type { PushTokenRegistration } from "@/lib/api/types";

export async function registerForPushNotificationsAsync(): Promise<
  string | null
> {
  if (!Device.isDevice) {
    // Simulators/emulators cannot receive push notifications
    console.warn("[push] Skipping push registration: not a physical device");
    return null;
  }

  const { status: existingStatus } =
    await Notifications.getPermissionsAsync();
  let finalStatus = existingStatus;

  if (existingStatus !== "granted") {
    const { status } = await Notifications.requestPermissionsAsync();
    finalStatus = status;
  }

  if (finalStatus !== "granted") {
    console.warn("[push] Push notification permission denied");
    return null;
  }

  // Android requires a notification channel
  if (Platform.OS === "android") {
    await Notifications.setNotificationChannelAsync("default", {
      name: "Personel Admin",
      importance: Notifications.AndroidImportance.MAX,
      vibrationPattern: [0, 250, 250, 250],
      lightColor: "#0ea5e9",
      lockscreenVisibility:
        Notifications.AndroidNotificationVisibility.PRIVATE,
      showBadge: true,
    });
  }

  try {
    const tokenData = await Notifications.getExpoPushTokenAsync({
      projectId:
        (
          require("@/../../app.config") as {
            extra?: { eas?: { projectId?: string } };
          }
        ).extra?.eas?.projectId ?? "REPLACE_WITH_EAS_PROJECT_ID",
    });

    const token = tokenData.data;
    const platform = Platform.OS === "ios" ? "ios" : "android";
    const deviceId = Device.osBuildId ?? Device.modelId ?? "unknown";

    const body: PushTokenRegistration = { token, platform, device_id: deviceId };
    await apiPost<void>("/v1/mobile/push-tokens", body);

    return token;
  } catch (err) {
    console.error("[push] Failed to register push token:", err);
    return null;
  }
}
