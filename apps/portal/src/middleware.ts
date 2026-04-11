import createMiddleware from "next-intl/middleware";
import { type NextRequest, NextResponse } from "next/server";
import { routing } from "./lib/i18n/routing";
import { getSessionFromRequest } from "./lib/auth/session";

const intlMiddleware = createMiddleware(routing);

// Paths that do not require authentication
const PUBLIC_PATHS = [
  "/tr/(auth)/giris",
  "/en/(auth)/giris",
  "/tr/(auth)/callback",
  "/en/(auth)/callback",
  "/api/auth/login",
  "/api/auth/logout",
  "/api/auth/session",
];

function isPublicPath(pathname: string): boolean {
  return PUBLIC_PATHS.some((p) => pathname.startsWith(p.replace(/\(auth\)\//, "")));
}

export async function middleware(request: NextRequest): Promise<NextResponse> {
  const { pathname } = request.nextUrl;

  // Skip middleware for static files and Next.js internals
  if (
    pathname.startsWith("/_next") ||
    pathname.startsWith("/favicon") ||
    pathname.startsWith("/logo") ||
    pathname.includes(".")
  ) {
    return NextResponse.next();
  }

  // Run intl middleware first to handle locale routing
  const intlResponse = intlMiddleware(request);

  // Allow public paths through without auth check
  if (isPublicPath(pathname)) {
    return intlResponse ?? NextResponse.next();
  }

  // Check authentication
  const session = await getSessionFromRequest(request);

  if (!session) {
    // Redirect to login, preserving the intended destination
    const locale = pathname.split("/")[1] ?? "tr";
    const loginUrl = new URL(`/${locale}/giris`, request.url);
    loginUrl.searchParams.set("callbackUrl", pathname);
    return NextResponse.redirect(loginUrl);
  }

  return intlResponse ?? NextResponse.next();
}

export const config = {
  matcher: [
    // Match all paths except static files
    "/((?!_next/static|_next/image|favicon.ico|logo.svg).*)",
  ],
};
