"use client";

import { useQuery } from "@tanstack/react-query";
import type { SessionUser } from "@/lib/auth/session";

/**
 * Fetch the current user from the Next.js session API route.
 * The session route reads the HTTP-only cookie and returns the user payload.
 */
async function fetchCurrentUser(): Promise<SessionUser | null> {
  const res = await fetch("/api/auth/session", {
    credentials: "include",
    cache: "no-store",
  });
  if (!res.ok) return null;
  const data = (await res.json()) as { user?: SessionUser };
  return data.user ?? null;
}

export const currentUserKey = ["current-user"] as const;

export function useCurrentUser(): {
  user: SessionUser | null;
  isLoading: boolean;
} {
  const { data, isLoading } = useQuery({
    queryKey: currentUserKey,
    queryFn: fetchCurrentUser,
    staleTime: 5 * 60 * 1000, // 5 minutes
    retry: false,
  });

  return {
    user: data ?? null,
    isLoading,
  };
}
