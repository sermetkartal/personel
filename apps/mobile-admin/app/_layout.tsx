/**
 * Root layout — wraps the entire app in:
 * 1. QueryClientProvider (TanStack Query)
 * 2. GestureHandlerRootView (react-native-gesture-handler)
 * 3. Auth check via Zustand session store
 * 4. Push notification setup
 * 5. Expo splash screen management
 *
 * On mount:
 * - Initialises MMKV encrypted storage (bootstraps encryption key from SecureStore)
 * - If authenticated, sets up push notifications
 * - Redirects unauthenticated users to /sign-in
 */

import "../global.css";
import React, { useEffect, useState } from "react";
import { Stack } from "expo-router";
import { StatusBar } from "expo-status-bar";
import * as SplashScreen from "expo-splash-screen";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { GestureHandlerRootView } from "react-native-gesture-handler";
import { useSessionStore, initSessionStorage } from "@/lib/auth/session";
import { usePushNotifications } from "@/hooks/use-push-notifications";

SplashScreen.preventAutoHideAsync();

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: (failureCount, error) => {
        // Do not retry on 401 or 403
        if (
          error instanceof Error &&
          error.name === "ApiError" &&
          "status" in error &&
          (error as { status: number }).status < 500
        ) {
          return false;
        }
        return failureCount < 2;
      },
      staleTime: 30_000,
    },
    mutations: {
      retry: false,
    },
  },
});

function RootLayoutInner() {
  const isAuthenticated = useSessionStore((s) => s.isAuthenticated);
  usePushNotifications(isAuthenticated);

  return (
    <>
      <Stack screenOptions={{ headerShown: false }}>
        <Stack.Screen name="index" />
        <Stack.Screen name="sign-in/index" />
        <Stack.Screen name="(tabs)" />
        <Stack.Screen
          name="live-view/[id]"
          options={{
            headerShown: true,
            headerTitle: "Canlı İzleme Detayı",
            headerBackTitle: "Geri",
            presentation: "card",
          }}
        />
        <Stack.Screen
          name="dsr/[id]"
          options={{
            headerShown: true,
            headerTitle: "Talep Detayı",
            headerBackTitle: "Geri",
            presentation: "card",
          }}
        />
        <Stack.Screen name="+not-found" />
      </Stack>
      <StatusBar style="auto" />
    </>
  );
}

export default function RootLayout() {
  const [storageReady, setStorageReady] = useState(false);

  useEffect(() => {
    async function prepare() {
      try {
        await initSessionStorage();
      } catch (e) {
        console.warn("[RootLayout] Storage init error:", e);
      } finally {
        setStorageReady(true);
        await SplashScreen.hideAsync();
      }
    }
    void prepare();
  }, []);

  if (!storageReady) return null;

  return (
    <GestureHandlerRootView style={{ flex: 1 }}>
      <QueryClientProvider client={queryClient}>
        <RootLayoutInner />
      </QueryClientProvider>
    </GestureHandlerRootView>
  );
}
