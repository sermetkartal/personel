/**
 * Push notification handlers.
 *
 * Foreground: show an in-app notification badge/toast instead of a system banner.
 * Background: deep link into the relevant screen when the user taps.
 *
 * Payload shape (enforced by mobile-bff — no PII ever sent):
 * {
 *   type: "live_view_request" | "dsr_new" | "silence_alert" | "audit_spike",
 *   count: number,
 *   deep_link: "personel://live-view/abc123" | "personel://dsr" | ...
 * }
 *
 * The mobile app MUST make an authenticated API call to fetch details after
 * the user taps. It NEVER renders employee names from the notification payload.
 */

import * as Notifications from "expo-notifications";
import * as Linking from "expo-linking";
import type { PushNotificationPayload } from "@/lib/api/types";
import { tr } from "@/lib/i18n/tr";

// ── Foreground handler ─────────────────────────────────────────────────────────

/**
 * Configure how notifications are handled when the app is in the foreground.
 * Shows a banner with the sanitized count; never with PII.
 */
export function configureForegroundNotificationHandler(): void {
  Notifications.setNotificationHandler({
    handleNotification: async (notification) => {
      const data = notification.request.content.data as Partial<PushNotificationPayload>;
      const type = data.type;

      let title = "Personel Admin";
      let body = "";

      switch (type) {
        case "live_view_request":
          title = tr.notifications.liveViewRequest;
          body = tr.notifications.liveViewRequestBody.replace(
            "{count}",
            String(data.count ?? 1),
          );
          break;
        case "dsr_new":
          title = tr.notifications.dsrNew;
          body = tr.notifications.dsrNewBody.replace(
            "{count}",
            String(data.count ?? 1),
          );
          break;
        case "silence_alert":
          title = tr.notifications.silenceAlert;
          body = tr.notifications.silenceAlertBody.replace(
            "{count}",
            String(data.count ?? 1),
          );
          break;
        case "audit_spike":
          title = tr.notifications.auditSpike;
          body = tr.notifications.auditSpikeBody;
          break;
        default:
          body = "Yeni bildirim";
      }

      return {
        shouldShowAlert: true,
        shouldPlaySound: true,
        shouldSetBadge: true,
        // Override title/body to enforce the no-PII contract in case
        // the BFF accidentally sends more data than expected
        alert: { title, body },
      };
    },
  });
}

// ── Background / tap handler ──────────────────────────────────────────────────

/**
 * Called when the user taps a notification (foreground or background).
 * Deep-links to the appropriate screen.
 * Details are fetched via an authenticated API call after navigation.
 */
export function registerNotificationResponseHandler(): Notifications.Subscription {
  return Notifications.addNotificationResponseReceivedListener((response) => {
    const data = response.notification.request.content
      .data as Partial<PushNotificationPayload>;

    const deepLink = data.deep_link;
    if (typeof deepLink === "string" && deepLink.startsWith("personel://")) {
      void Linking.openURL(deepLink);
    }
  });
}

// ── Background notification received ─────────────────────────────────────────

/**
 * Registers a listener for notifications received while app is running
 * in background but not yet tapped.
 */
export function registerNotificationReceivedHandler(): Notifications.Subscription {
  return Notifications.addNotificationReceivedListener((_notification) => {
    // Badge count update is handled by the OS via shouldSetBadge: true above.
    // No PII-bearing processing here.
  });
}
