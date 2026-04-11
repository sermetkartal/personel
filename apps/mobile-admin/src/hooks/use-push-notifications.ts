/**
 * Push notification setup hook.
 * Called once from the root layout after the user is authenticated.
 * Registers the device token with mobile-bff and sets up notification handlers.
 */

import { useEffect, useRef } from "react";
import type { Subscription } from "expo-notifications";
import { registerForPushNotificationsAsync } from "@/lib/notifications/register";
import {
  configureForegroundNotificationHandler,
  registerNotificationResponseHandler,
  registerNotificationReceivedHandler,
} from "@/lib/notifications/handlers";

export function usePushNotifications(isAuthenticated: boolean): void {
  const responseSubscriptionRef = useRef<Subscription | null>(null);
  const receivedSubscriptionRef = useRef<Subscription | null>(null);

  useEffect(() => {
    if (!isAuthenticated) return;

    // Configure foreground display behavior
    configureForegroundNotificationHandler();

    // Register device with mobile-bff
    void registerForPushNotificationsAsync().catch((err: unknown) => {
      console.warn("[push] Registration error:", err);
    });

    // Wire up tap and received listeners
    responseSubscriptionRef.current = registerNotificationResponseHandler();
    receivedSubscriptionRef.current = registerNotificationReceivedHandler();

    return () => {
      responseSubscriptionRef.current?.remove();
      receivedSubscriptionRef.current?.remove();
    };
  }, [isAuthenticated]);
}
