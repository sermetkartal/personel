/**
 * OAuth2 PKCE callback route.
 * Accepts the authorization code + state, exchanges for tokens,
 * creates a session cookie, and returns the redirect URL.
 *
 * Called via POST from the client-side callback page.
 */

import { type NextRequest, NextResponse } from "next/server";
import { serializeSession, sessionCookieOptions } from "@/lib/auth/session";
import { routing } from "@/lib/i18n/routing";
import type { Role } from "@/lib/api/types";

interface CallbackBody {
  code: string;
  state: string;
}

interface KeycloakTokenResponse {
  access_token: string;
  refresh_token: string;
  expires_in: number;
  token_type: string;
  id_token?: string;
}

interface KeycloakIntrospectResponse {
  sub: string;
  email: string;
  preferred_username: string;
  active: boolean;
}

function problem(status: number, detail: string): NextResponse {
  return NextResponse.json({ detail }, { status });
}

export async function POST(request: NextRequest): Promise<NextResponse> {
  let body: CallbackBody;
  try {
    body = (await request.json()) as CallbackBody;
  } catch {
    return problem(400, "Invalid request body.");
  }

  const { code, state } = body;
  if (!code || !state) {
    return problem(400, "Missing code or state.");
  }

  // Validate CSRF state
  const storedState = request.cookies.get("auth_state")?.value;
  if (!storedState || storedState !== state) {
    return problem(400, "State mismatch — possible CSRF.");
  }

  const codeVerifier = request.cookies.get("pkce_verifier")?.value;
  if (!codeVerifier) {
    return problem(400, "Missing PKCE verifier — session may have expired.");
  }

  const callbackUrl =
    request.cookies.get("callback_url")?.value ??
    `/${routing.defaultLocale}/dashboard`;

  // Exchange code for tokens at Keycloak
  const keycloakUrl = process.env.NEXT_PUBLIC_KEYCLOAK_URL ?? "http://localhost:8180";
  const realm = process.env.NEXT_PUBLIC_KEYCLOAK_REALM ?? "personel";
  const clientId = process.env.NEXT_PUBLIC_KEYCLOAK_CLIENT_ID ?? "personel-console";
  const appUrl = process.env.NEXTAUTH_URL ?? "http://localhost:3000";

  let tokenData: KeycloakTokenResponse;
  try {
    const tokenRes = await fetch(
      `${keycloakUrl}/realms/${realm}/protocol/openid-connect/token`,
      {
        method: "POST",
        headers: { "Content-Type": "application/x-www-form-urlencoded" },
        body: new URLSearchParams({
          grant_type: "authorization_code",
          client_id: clientId,
          code,
          redirect_uri: `${appUrl}/api/auth/callback`,
          code_verifier: codeVerifier,
        }),
      },
    );

    if (!tokenRes.ok) {
      const err = (await tokenRes.json().catch(() => ({}))) as { error_description?: string };
      return problem(401, err.error_description ?? "Token exchange failed.");
    }

    tokenData = (await tokenRes.json()) as KeycloakTokenResponse;
  } catch {
    return problem(502, "Failed to reach Keycloak.");
  }

  // Introspect token to get user info + role
  let userInfo: KeycloakIntrospectResponse;
  try {
    const introspectRes = await fetch(
      `${keycloakUrl}/realms/${realm}/protocol/openid-connect/userinfo`,
      {
        headers: { Authorization: `Bearer ${tokenData.access_token}` },
      },
    );

    if (!introspectRes.ok) {
      return problem(401, "Could not fetch user info.");
    }

    userInfo = (await introspectRes.json()) as KeycloakIntrospectResponse;
  } catch {
    return problem(502, "Failed to fetch user info.");
  }

  // Extract role from token claims
  // In real implementation, decode the JWT and read realm_access.roles
  // For Phase 1: read from a custom claim or fallback to "admin"
  const role: Role = (process.env.DEV_DEFAULT_ROLE as Role | undefined) ?? "admin";

  const response = NextResponse.json({ redirect_to: callbackUrl });

  // Set session cookie
  response.cookies.set(
    "personel_session",
    serializeSession({
      id: userInfo.sub,
      email: userInfo.email,
      username: userInfo.preferred_username,
      role,
      tenant_id: process.env.DEFAULT_TENANT_ID ?? "00000000-0000-0000-0000-000000000001",
      access_token: tokenData.access_token,
      refresh_token: tokenData.refresh_token,
      expires_at: Math.floor(Date.now() / 1000) + tokenData.expires_in,
    }),
    sessionCookieOptions(),
  );

  // Clear PKCE cookies
  response.cookies.delete("pkce_verifier");
  response.cookies.delete("auth_state");
  response.cookies.delete("callback_url");

  return response;
}
