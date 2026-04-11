import { type NextRequest, NextResponse } from "next/server";

const KEYCLOAK_ISSUER = process.env["KEYCLOAK_ISSUER"] ?? "";
const CLIENT_ID = process.env["KEYCLOAK_CLIENT_ID"] ?? "personel-portal";
const BASE_URL = process.env["NEXT_PUBLIC_BASE_URL"] ?? "http://localhost:3001";

/**
 * GET /api/auth/login
 * Initiates the Keycloak OAuth2 Authorization Code flow.
 * Redirects the browser to Keycloak's authorize endpoint.
 */
export async function GET(request: NextRequest): Promise<NextResponse> {
  const callbackUrl = request.nextUrl.searchParams.get("callbackUrl") ?? "/tr";
  const redirectUri = `${BASE_URL}/api/auth/session?callbackUrl=${encodeURIComponent(callbackUrl)}`;

  const authorizeUrl = new URL(`${KEYCLOAK_ISSUER}/protocol/openid-connect/auth`);
  authorizeUrl.searchParams.set("client_id", CLIENT_ID);
  authorizeUrl.searchParams.set("response_type", "code");
  authorizeUrl.searchParams.set("redirect_uri", redirectUri);
  authorizeUrl.searchParams.set("scope", "openid profile email");
  authorizeUrl.searchParams.set(
    "state",
    Buffer.from(callbackUrl).toString("base64url")
  );

  return NextResponse.redirect(authorizeUrl.toString());
}
