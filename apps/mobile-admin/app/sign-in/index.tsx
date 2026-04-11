/**
 * Sign-in screen.
 * Single button triggers the OIDC PKCE flow via expo-auth-session.
 * On success, the Zustand session store is populated and the user
 * is redirected to /(tabs)/home.
 */

import React, { useState } from "react";
import {
  View,
  Text,
  TouchableOpacity,
  ActivityIndicator,
  Alert,
  ScrollView,
} from "react-native";
import { router } from "expo-router";
import { SafeAreaView } from "react-native-safe-area-context";
import { signInWithKeycloak } from "@/lib/auth/oidc";
import { tr } from "@/lib/i18n/tr";

export default function SignInScreen() {
  const [isLoading, setIsLoading] = useState(false);

  const handleSignIn = async () => {
    setIsLoading(true);
    try {
      await signInWithKeycloak();
      router.replace("/(tabs)/home");
    } catch (error) {
      const message =
        error instanceof Error ? error.message : tr.common.errorGeneric;
      Alert.alert(tr.auth.errorTitle, message, [
        { text: tr.auth.tryAgain, onPress: () => setIsLoading(false) },
      ]);
    } finally {
      setIsLoading(false);
    }
  };

  return (
    <SafeAreaView className="flex-1 bg-surface-dark">
      <ScrollView
        contentContainerStyle={{ flexGrow: 1 }}
        keyboardShouldPersistTaps="handled"
      >
        <View className="flex-1 px-6 justify-between py-12">
          {/* Header / branding */}
          <View className="flex-1 items-center justify-center">
            <View className="w-20 h-20 rounded-3xl bg-brand-600 items-center justify-center mb-8">
              <Text className="text-white text-4xl font-bold">P</Text>
            </View>
            <Text className="text-white text-3xl font-bold text-center">
              {tr.auth.heading}
            </Text>
            <Text className="text-slate-400 text-base text-center mt-2">
              {tr.auth.subheading}
            </Text>
          </View>

          {/* CTA section */}
          <View>
            <TouchableOpacity
              onPress={handleSignIn}
              disabled={isLoading}
              className={`rounded-2xl py-4 items-center ${
                isLoading ? "bg-brand-700" : "bg-brand-500"
              }`}
              accessibilityRole="button"
              accessibilityLabel={tr.auth.loginButton}
              accessibilityState={{ disabled: isLoading }}
            >
              {isLoading ? (
                <View className="flex-row items-center space-x-3">
                  <ActivityIndicator color="#ffffff" size="small" />
                  <Text className="text-white font-semibold text-base ml-3">
                    {tr.auth.processing}
                  </Text>
                </View>
              ) : (
                <Text className="text-white font-semibold text-base">
                  {tr.auth.loginButton}
                </Text>
              )}
            </TouchableOpacity>

            <Text className="text-slate-500 text-xs text-center mt-4 px-4 leading-5">
              {tr.auth.kvkkNotice}
            </Text>
          </View>
        </View>
      </ScrollView>
    </SafeAreaView>
  );
}
