/**
 * Logout route — clears session and redirects to Keycloak logout.
 */

import { type NextRequest, NextResponse } from "next/server";
import { getSession } from "@/lib/auth/session";
import { routing } from "@/lib/i18n/routing";

export async function GET(request: NextRequest): Promise<NextResponse> {
  const session = await getSession();

  const keycloakUrl = process.env.NEXT_PUBLIC_KEYCLOAK_URL ?? "http://localhost:8180";
  const realm = process.env.NEXT_PUBLIC_KEYCLOAK_REALM ?? "personel";
  const appUrl = request.nextUrl.origin;
  const redirectUri = `${appUrl}/${routing.defaultLocale}/login`;

  const logoutUrl = new URL(
    `${keycloakUrl}/realms/${realm}/protocol/openid-connect/logout`,
  );
  logoutUrl.searchParams.set("post_logout_redirect_uri", redirectUri);
  logoutUrl.searchParams.set(
    "client_id",
    process.env.NEXT_PUBLIC_KEYCLOAK_CLIENT_ID ?? "personel-console",
  );

  if (session?.user.access_token) {
    // id_token_hint enables RP-initiated logout
    logoutUrl.searchParams.set("id_token_hint", session.user.access_token);
  }

  const response = NextResponse.redirect(logoutUrl.toString());

  // Clear the session cookie
  response.cookies.set("personel_session", "", {
    httpOnly: true,
    secure: process.env.NODE_ENV === "production",
    sameSite: "lax",
    maxAge: 0,
    path: "/",
  });

  return response;
}
