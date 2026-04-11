/**
 * Entry point — redirects to /home (authenticated) or /sign-in (unauthenticated).
 * Expo Router file-based routing: this file handles the "/" route.
 */

import { Redirect } from "expo-router";
import { useSessionStore } from "@/lib/auth/session";

export default function Index() {
  const isAuthenticated = useSessionStore((s) => s.isAuthenticated);

  if (isAuthenticated) {
    return <Redirect href="/(tabs)/home" />;
  }
  return <Redirect href="/sign-in" />;
}
