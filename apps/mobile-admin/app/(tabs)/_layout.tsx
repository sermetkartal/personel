/**
 * Authenticated tab navigator.
 * 4 tabs in Turkish: Ana Sayfa, Canlı İzleme, Veri Talepleri, Sessizlik.
 * Redirects to /sign-in if the session is not authenticated.
 */

import React from "react";
import { Tabs, Redirect } from "expo-router";
import { Text, View } from "react-native";
import { useSessionStore } from "@/lib/auth/session";
import { tr } from "@/lib/i18n/tr";

// Simple text-based tab icons (no external icon library dependency)
function TabIcon({
  emoji,
  focused,
}: {
  emoji: string;
  focused: boolean;
}) {
  return (
    <View className={`items-center justify-center ${focused ? "opacity-100" : "opacity-50"}`}>
      <Text className="text-xl">{emoji}</Text>
    </View>
  );
}

export default function TabsLayout() {
  const isAuthenticated = useSessionStore((s) => s.isAuthenticated);

  if (!isAuthenticated) {
    return <Redirect href="/sign-in" />;
  }

  return (
    <Tabs
      screenOptions={{
        tabBarActiveTintColor: "#0ea5e9",
        tabBarInactiveTintColor: "#94a3b8",
        tabBarStyle: {
          backgroundColor: "#ffffff",
          borderTopColor: "#e2e8f0",
          paddingBottom: 8,
          paddingTop: 4,
          height: 64,
        },
        tabBarLabelStyle: {
          fontSize: 11,
          fontWeight: "500",
          marginTop: 2,
        },
        headerShown: false,
      }}
    >
      <Tabs.Screen
        name="home"
        options={{
          title: tr.nav.home,
          tabBarIcon: ({ focused }) => (
            <TabIcon emoji="🏠" focused={focused} />
          ),
        }}
      />
      <Tabs.Screen
        name="live-view"
        options={{
          title: tr.nav.liveView,
          tabBarIcon: ({ focused }) => (
            <TabIcon emoji="👁" focused={focused} />
          ),
        }}
      />
      <Tabs.Screen
        name="dsr"
        options={{
          title: tr.nav.dsr,
          tabBarIcon: ({ focused }) => (
            <TabIcon emoji="📋" focused={focused} />
          ),
        }}
      />
      <Tabs.Screen
        name="silence"
        options={{
          title: tr.nav.silence,
          tabBarIcon: ({ focused }) => (
            <TabIcon emoji="🔇" focused={focused} />
          ),
        }}
      />
    </Tabs>
  );
}
