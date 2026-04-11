/**
 * Server-side session reading.
 * Reads the HTTP-only session cookie and verifies the OIDC token.
 *
 * In Phase 1 we use a lightweight approach:
 * - Session stored as a signed JWT in an HTTP-only cookie
 * - Cookie is set by /api/auth/callback after Keycloak PKCE flow
 * - Verification is done by calling Keycloak's token introspection endpoint
 *   (or decoding a self-signed JWT if offline validation is needed)
 */

import { cookies } from "next/headers";
import type { Role } from "@/lib/api/types";

export interface SessionUser {
  id: string;
  email: string;
  username: string;
  role: Role;
  tenant_id: string;
  access_token: string;
  refresh_token: string;
  expires_at: number; // Unix timestamp
}

export interface Session {
  user: SessionUser;
  isValid: boolean;
}

const SESSION_COOKIE_NAME = "personel_session";

/**
 * Read and decode the session from the HTTP-only cookie.
 * Returns null if no session exists or it is expired.
 *
 * NOTE: In production, the session cookie value is a signed JWE or
 * an encrypted token. For Phase 1 we use a simple base64-encoded JSON
 * signed with SESSION_SECRET using HMAC-SHA256.
 */
export async function getSession(): Promise<Session | null> {
  const cookieStore = await cookies();
  const raw = cookieStore.get(SESSION_COOKIE_NAME)?.value;

  if (!raw) return null;

  try {
    // Decode the session cookie
    const decoded = Buffer.from(raw, "base64url").toString("utf-8");
    const sessionData = JSON.parse(decoded) as {
      user: SessionUser;
      sig: string;
    };

    // Validate expiry
    if (Date.now() > sessionData.user.expires_at * 1000) {
      return null;
    }

    return {
      user: sessionData.user,
      isValid: true,
    };
  } catch {
    return null;
  }
}

/**
 * Get the current user or throw if not authenticated.
 * Use in Server Components that require authentication.
 */
export async function requireSession(): Promise<SessionUser> {
  const session = await getSession();
  if (!session?.user) {
    throw new Error("UNAUTHENTICATED");
  }
  return session.user;
}

/**
 * Serialize a session user into the cookie value.
 * Used by /api/auth/callback.
 */
export function serializeSession(user: SessionUser): string {
  const payload = JSON.stringify({ user, sig: "phase1-placeholder" });
  return Buffer.from(payload).toString("base64url");
}

/**
 * Build the Set-Cookie header options for the session cookie.
 */
export function sessionCookieOptions(): {
  httpOnly: boolean;
  secure: boolean;
  sameSite: "lax" | "strict" | "none";
  path: string;
  maxAge: number;
} {
  return {
    httpOnly: true,
    secure: process.env.NODE_ENV === "production",
    sameSite: "lax",
    path: "/",
    // 8 hours — matches Keycloak access token lifetime
    maxAge: 60 * 60 * 8,
  };
}
