import { type NextRequest, NextResponse } from "next/server";
import { SESSION_COOKIE_NAME } from "@/lib/auth/session";

const KEYCLOAK_ISSUER = process.env["KEYCLOAK_ISSUER"] ?? "";
const BASE_URL = process.env["NEXT_PUBLIC_BASE_URL"] ?? "http://localhost:3001";

/**
 * POST /api/auth/logout
 * Clears the session cookie and redirects to Keycloak's end-session endpoint.
 */
export async function POST(_request: NextRequest): Promise<NextResponse> {
  const response = NextResponse.redirect(
    `${KEYCLOAK_ISSUER}/protocol/openid-connect/logout?redirect_uri=${encodeURIComponent(BASE_URL + "/tr/giris")}`
  );

  // Clear the session cookie
  response.cookies.set(SESSION_COOKIE_NAME, "", {
    httpOnly: true,
    secure: process.env["NODE_ENV"] === "production",
    sameSite: "lax",
    maxAge: 0,
    path: "/",
  });

  return response;
}
