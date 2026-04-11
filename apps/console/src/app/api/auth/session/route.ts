/**
 * Session API route handler.
 * GET  — returns current user from session cookie
 * POST — handles token refresh
 */

import { type NextRequest, NextResponse } from "next/server";
import { getSession, serializeSession, sessionCookieOptions } from "@/lib/auth/session";

export async function GET(): Promise<NextResponse> {
  const session = await getSession();

  if (!session) {
    return NextResponse.json({ user: null }, { status: 200 });
  }

  // Return user without sensitive token data
  const { access_token, refresh_token, ...safeUser } = session.user;

  // Suppress unused variable warnings — tokens are intentionally excluded
  void access_token;
  void refresh_token;

  return NextResponse.json({ user: safeUser }, { status: 200 });
}

export async function POST(request: NextRequest): Promise<NextResponse> {
  const body = (await request.json()) as { action?: string };

  if (body.action === "refresh") {
    const session = await getSession();
    if (!session?.user.refresh_token) {
      return NextResponse.json({ error: "no_refresh_token" }, { status: 401 });
    }

    // Call Keycloak refresh token endpoint
    const keycloakUrl = process.env.NEXT_PUBLIC_KEYCLOAK_URL;
    const realm = process.env.NEXT_PUBLIC_KEYCLOAK_REALM ?? "personel";
    const clientId = process.env.NEXT_PUBLIC_KEYCLOAK_CLIENT_ID ?? "personel-console";

    const tokenUrl = `${keycloakUrl}/realms/${realm}/protocol/openid-connect/token`;

    const params = new URLSearchParams({
      grant_type: "refresh_token",
      client_id: clientId,
      refresh_token: session.user.refresh_token,
    });

    const res = await fetch(tokenUrl, {
      method: "POST",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body: params.toString(),
    });

    if (!res.ok) {
      return NextResponse.json({ error: "refresh_failed" }, { status: 401 });
    }

    const tokens = (await res.json()) as {
      access_token: string;
      refresh_token: string;
      expires_in: number;
    };

    const updatedUser = {
      ...session.user,
      access_token: tokens.access_token,
      refresh_token: tokens.refresh_token,
      expires_at: Math.floor(Date.now() / 1000) + tokens.expires_in,
    };

    const cookie = serializeSession(updatedUser);
    const opts = sessionCookieOptions();

    const response = NextResponse.json({
      access_token: tokens.access_token,
      refresh_token: tokens.refresh_token,
    });

    response.cookies.set("personel_session", cookie, opts);
    return response;
  }

  return NextResponse.json({ error: "unknown_action" }, { status: 400 });
}
