/**
 * Login route — redirects to Keycloak PKCE authorization endpoint.
 */

import { type NextRequest, NextResponse } from "next/server";
import { routing } from "@/lib/i18n/routing";

function generateCodeVerifier(): string {
  const array = new Uint8Array(32);
  crypto.getRandomValues(array);
  return btoa(String.fromCharCode(...array))
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=/g, "");
}

async function generateCodeChallenge(verifier: string): Promise<string> {
  const encoder = new TextEncoder();
  const data = encoder.encode(verifier);
  const hash = await crypto.subtle.digest("SHA-256", data);
  return btoa(String.fromCharCode(...new Uint8Array(hash)))
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=/g, "");
}

export async function GET(request: NextRequest): Promise<NextResponse> {
  const { searchParams } = request.nextUrl;
  const callbackUrl = searchParams.get("callbackUrl") ?? `/${routing.defaultLocale}/dashboard`;

  const keycloakUrl = process.env.NEXT_PUBLIC_KEYCLOAK_URL ?? "http://localhost:8180";
  const realm = process.env.NEXT_PUBLIC_KEYCLOAK_REALM ?? "personel";
  const clientId = process.env.NEXT_PUBLIC_KEYCLOAK_CLIENT_ID ?? "personel-console";
  const appUrl = process.env.NEXTAUTH_URL ?? "http://localhost:3000";

  const codeVerifier = generateCodeVerifier();
  const codeChallenge = await generateCodeChallenge(codeVerifier);
  const state = generateCodeVerifier(); // Use same entropy mechanism for state

  const authUrl = new URL(
    `${keycloakUrl}/realms/${realm}/protocol/openid-connect/auth`,
  );
  authUrl.searchParams.set("client_id", clientId);
  authUrl.searchParams.set("response_type", "code");
  authUrl.searchParams.set("scope", "openid profile email");
  // Keycloak redirects here after the user authenticates. The actual
  // handler is the client-side page at /tr/callback which reads the
  // code + state from the URL and POSTs them to /api/auth/callback for
  // the server-side token exchange. Previously this pointed at the
  // API route directly, which only accepts POST — so the browser GET
  // redirect landed on a 405. Fixed 2026-04-12 during the first real
  // browser login attempt.
  authUrl.searchParams.set("redirect_uri", `${appUrl}/tr/callback`);
  authUrl.searchParams.set("code_challenge", codeChallenge);
  authUrl.searchParams.set("code_challenge_method", "S256");
  authUrl.searchParams.set("state", state);

  const response = NextResponse.redirect(authUrl.toString());

  // Store PKCE verifier and state in short-lived cookies
  response.cookies.set("pkce_verifier", codeVerifier, {
    httpOnly: true,
    secure:
      process.env.INSECURE_COOKIES !== "1" &&
      process.env.NODE_ENV === "production",
    sameSite: "lax",
    maxAge: 300, // 5 minutes
    path: "/",
  });
  response.cookies.set("auth_state", state, {
    httpOnly: true,
    secure:
      process.env.INSECURE_COOKIES !== "1" &&
      process.env.NODE_ENV === "production",
    sameSite: "lax",
    maxAge: 300,
    path: "/",
  });
  response.cookies.set("callback_url", callbackUrl, {
    httpOnly: true,
    secure:
      process.env.INSECURE_COOKIES !== "1" &&
      process.env.NODE_ENV === "production",
    sameSite: "lax",
    maxAge: 300,
    path: "/",
  });

  return response;
}
