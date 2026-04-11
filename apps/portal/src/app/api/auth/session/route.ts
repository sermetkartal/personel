import { type NextRequest, NextResponse } from "next/server";
import {
  createSession,
  getSession,
  SESSION_COOKIE_NAME,
  SESSION_MAX_AGE,
} from "@/lib/auth/session";

const KEYCLOAK_ISSUER = process.env["KEYCLOAK_ISSUER"] ?? "";
const CLIENT_ID = process.env["KEYCLOAK_CLIENT_ID"] ?? "personel-portal";
const CLIENT_SECRET = process.env["KEYCLOAK_CLIENT_SECRET"] ?? "";
const BASE_URL = process.env["NEXT_PUBLIC_BASE_URL"] ?? "http://localhost:3001";

interface KeycloakTokenResponse {
  access_token: string;
  id_token?: string;
  expires_in: number;
  token_type: string;
}

interface KeycloakUserInfo {
  sub: string;
  email: string;
  name?: string;
  preferred_username?: string;
}

/**
 * GET /api/auth/session
 * Returns the current session payload (sanitized — no access token exposed to client).
 */
export async function GET(_request: NextRequest): Promise<NextResponse> {
  const session = await getSession();

  if (!session) {
    return NextResponse.json({ authenticated: false }, { status: 401 });
  }

  // Return safe session data — do NOT expose accessToken to client
  return NextResponse.json({
    authenticated: true,
    userId: session.userId,
    email: session.email,
    name: session.name,
    locale: session.locale,
    firstLoginAcknowledged: session.firstLoginAcknowledged,
  });
}

/**
 * POST /api/auth/session
 * Handles the OAuth2 callback code exchange.
 * Receives the authorization code from Keycloak, exchanges it for tokens,
 * creates a signed session cookie, and redirects the user.
 */
export async function POST(request: NextRequest): Promise<NextResponse> {
  const { searchParams } = request.nextUrl;
  const code = searchParams.get("code");
  const callbackUrl = searchParams.get("callbackUrl") ?? "/tr";

  if (!code) {
    return NextResponse.redirect(`${BASE_URL}/tr/giris?error=no_code`);
  }

  try {
    // Exchange code for tokens
    const redirectUri = `${BASE_URL}/api/auth/session?callbackUrl=${encodeURIComponent(callbackUrl)}`;
    const tokenResponse = await fetch(
      `${KEYCLOAK_ISSUER}/protocol/openid-connect/token`,
      {
        method: "POST",
        headers: { "Content-Type": "application/x-www-form-urlencoded" },
        body: new URLSearchParams({
          grant_type: "authorization_code",
          client_id: CLIENT_ID,
          client_secret: CLIENT_SECRET,
          code,
          redirect_uri: redirectUri,
        }),
      }
    );

    if (!tokenResponse.ok) {
      throw new Error("Token exchange failed");
    }

    const tokens = (await tokenResponse.json()) as KeycloakTokenResponse;

    // Get user info
    const userInfoResponse = await fetch(
      `${KEYCLOAK_ISSUER}/protocol/openid-connect/userinfo`,
      { headers: { Authorization: `Bearer ${tokens.access_token}` } }
    );

    if (!userInfoResponse.ok) {
      throw new Error("User info fetch failed");
    }

    const userInfo = (await userInfoResponse.json()) as KeycloakUserInfo;

    // Create session
    const sessionToken = await createSession({
      userId: userInfo.sub,
      email: userInfo.email ?? userInfo.preferred_username ?? userInfo.sub,
      name: userInfo.name ?? userInfo.preferred_username ?? userInfo.email ?? "Kullanıcı",
      accessToken: tokens.access_token,
      locale: "tr",
      firstLoginAcknowledged: false, // Will be updated via API call
    });

    const response = NextResponse.redirect(`${BASE_URL}${callbackUrl}`);
    response.cookies.set(SESSION_COOKIE_NAME, sessionToken, {
      httpOnly: true,
      secure: process.env["NODE_ENV"] === "production",
      sameSite: "lax",
      maxAge: SESSION_MAX_AGE,
      path: "/",
    });

    return response;
  } catch (err) {
    console.error("[auth/session] OAuth2 callback error:", err);
    return NextResponse.redirect(`${BASE_URL}/tr/giris?error=auth_failed`);
  }
}
