/**
 * OIDC PKCE flow using expo-auth-session + expo-web-browser.
 * Authenticates against the same Keycloak realm as the Admin Console.
 * The mobile-bff acts as a thin HTTPS proxy; auth happens directly
 * against Keycloak using the public PKCE client (no client secret).
 *
 * Deep link scheme: "personel://" — configured in app.config.ts.
 */

import * as AuthSession from "expo-auth-session";
import * as WebBrowser from "expo-web-browser";
import * as Crypto from "expo-crypto";
import Constants from "expo-constants";
import { useSessionStore, type SessionUser } from "@/lib/auth/session";

// Required for expo-auth-session redirect handling on iOS/Android
WebBrowser.maybeCompleteAuthSession();

// ── Keycloak discovery ─────────────────────────────────────────────────────────

function getKeycloakDiscoveryUrl(): string {
  const extra = Constants.expoConfig?.extra as Record<string, unknown> | undefined;
  const url = extra?.keycloakUrl as string | undefined;
  const realm = extra?.keycloakRealm as string | undefined;
  if (!url || !realm) {
    throw new Error(
      "KEYCLOAK_URL and KEYCLOAK_REALM must be set in app.config.ts extras",
    );
  }
  return `${url}/realms/${realm}/.well-known/openid-configuration`;
}

function getClientId(): string {
  const extra = Constants.expoConfig?.extra as Record<string, unknown> | undefined;
  const clientId = extra?.keycloakClientId as string | undefined;
  if (!clientId) {
    throw new Error("KEYCLOAK_CLIENT_ID must be set in app.config.ts extras");
  }
  return clientId;
}

// ── PKCE utilities ─────────────────────────────────────────────────────────────

async function generateCodeVerifier(): Promise<string> {
  const bytes = await Crypto.getRandomBytesAsync(32);
  return Buffer.from(bytes)
    .toString("base64")
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=/g, "");
}

async function generateCodeChallenge(verifier: string): Promise<string> {
  const digest = await Crypto.digestStringAsync(
    Crypto.CryptoDigestAlgorithm.SHA256,
    verifier,
    { encoding: Crypto.CryptoEncoding.BASE64 },
  );
  return digest.replace(/\+/g, "-").replace(/\//g, "_").replace(/=/g, "");
}

// ── Token parsing ─────────────────────────────────────────────────────────────

interface JwtPayload {
  sub: string;
  email?: string;
  preferred_username?: string;
  realm_access?: { roles: string[] };
  tenant_id?: string;
  exp: number;
}

function parseJwt(token: string): JwtPayload {
  const parts = token.split(".");
  if (parts.length !== 3) {
    throw new Error("Invalid JWT format");
  }
  const payload = parts[1];
  if (!payload) {
    throw new Error("JWT payload is missing");
  }
  const padded = payload.replace(/-/g, "+").replace(/_/g, "/");
  const decoded = atob(padded);
  return JSON.parse(decoded) as JwtPayload;
}

function sessionUserFromJwt(accessToken: string): SessionUser {
  const payload = parseJwt(accessToken);
  return {
    sub: payload.sub,
    email: payload.email ?? "",
    username: payload.preferred_username ?? payload.sub,
    roles: payload.realm_access?.roles ?? [],
    tenant_id: payload.tenant_id ?? "",
  };
}

// ── Token exchange response ───────────────────────────────────────────────────

interface TokenResponse {
  access_token: string;
  refresh_token: string;
  expires_in: number;
  token_type: string;
}

export interface OidcTokens {
  accessToken: string;
  refreshToken: string;
}

// ── Sign In ───────────────────────────────────────────────────────────────────

export async function signInWithKeycloak(): Promise<void> {
  const discoveryUrl = getKeycloakDiscoveryUrl();
  const clientId = getClientId();
  const redirectUri = AuthSession.makeRedirectUri({ scheme: "personel" });

  const discovery = await AuthSession.fetchDiscoveryAsync(discoveryUrl);

  const codeVerifier = await generateCodeVerifier();
  const codeChallenge = await generateCodeChallenge(codeVerifier);

  const request = new AuthSession.AuthRequest({
    clientId,
    redirectUri,
    scopes: ["openid", "profile", "email", "offline_access"],
    codeChallengeMethod: AuthSession.CodeChallengeMethod.S256,
    codeChallenge,
    extraParams: {
      code_verifier: codeVerifier,
    },
  });

  const result = await request.promptAsync(discovery, {
    useProxy: false,
  });

  if (result.type !== "success") {
    throw new Error(
      result.type === "error"
        ? (result.error?.message ?? "OIDC authorization error")
        : "OIDC flow was cancelled",
    );
  }

  const code = result.params["code"];
  if (!code) {
    throw new Error("No authorization code returned from Keycloak");
  }

  // Exchange code for tokens
  const tokenResponse = await fetch(discovery.tokenEndpoint, {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body: new URLSearchParams({
      grant_type: "authorization_code",
      client_id: clientId,
      redirect_uri: redirectUri,
      code,
      code_verifier: codeVerifier,
    }).toString(),
  });

  if (!tokenResponse.ok) {
    const err = await tokenResponse.text();
    throw new Error(`Token exchange failed: ${err}`);
  }

  const tokens = (await tokenResponse.json()) as TokenResponse;
  const user = sessionUserFromJwt(tokens.access_token);

  useSessionStore.getState().setSession(
    tokens.access_token,
    tokens.refresh_token,
    user,
  );
}

// ── Refresh ───────────────────────────────────────────────────────────────────

export async function refreshAccessToken(
  currentRefreshToken: string,
): Promise<OidcTokens> {
  const discoveryUrl = getKeycloakDiscoveryUrl();
  const clientId = getClientId();

  const discovery = await AuthSession.fetchDiscoveryAsync(discoveryUrl);

  const response = await fetch(discovery.tokenEndpoint, {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body: new URLSearchParams({
      grant_type: "refresh_token",
      client_id: clientId,
      refresh_token: currentRefreshToken,
    }).toString(),
  });

  if (!response.ok) {
    throw new Error("Token refresh failed");
  }

  const tokens = (await response.json()) as TokenResponse;
  return {
    accessToken: tokens.access_token,
    refreshToken: tokens.refresh_token,
  };
}

// ── Sign Out ──────────────────────────────────────────────────────────────────

export async function signOut(): Promise<void> {
  const { refreshToken, clearSession } = useSessionStore.getState();
  clearSession();

  if (!refreshToken) return;

  try {
    const discoveryUrl = getKeycloakDiscoveryUrl();
    const clientId = getClientId();
    const discovery = await AuthSession.fetchDiscoveryAsync(discoveryUrl);

    // Revoke refresh token server-side (best-effort — don't block UX on failure)
    if (discovery.revocationEndpoint) {
      await fetch(discovery.revocationEndpoint, {
        method: "POST",
        headers: { "Content-Type": "application/x-www-form-urlencoded" },
        body: new URLSearchParams({
          client_id: clientId,
          token: refreshToken,
          token_type_hint: "refresh_token",
        }).toString(),
      });
    }
  } catch {
    // Non-fatal: local session is already cleared
  }
}
