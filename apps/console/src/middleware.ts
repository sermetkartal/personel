import createMiddleware from "next-intl/middleware";
import { type NextRequest, NextResponse } from "next/server";
import { routing } from "@/lib/i18n/routing";

const intlMiddleware = createMiddleware(routing);

// Routes that do not require authentication
const PUBLIC_PATHS = [
  "/:locale/login",
  "/:locale/callback",
  "/api/auth/login",
  "/api/auth/logout",
  "/api/auth/callback",
  "/api/auth/session",
  "/api/health",
  "/healthz",
  "/readyz",
  "/_next",
  "/favicon.ico",
  "/logo.svg",
];

function isPublicPath(pathname: string): boolean {
  return PUBLIC_PATHS.some((pattern) => {
    // Simple glob match for /:locale/ prefix patterns
    if (pattern.startsWith("/:locale/")) {
      const suffix = pattern.slice("/:locale/".length);
      return /^\/[a-z]{2}\//.test(pathname) && pathname.includes(`/${suffix}`);
    }
    return pathname.startsWith(pattern);
  });
}

export async function middleware(request: NextRequest): Promise<NextResponse> {
  const { pathname } = request.nextUrl;

  // API routes must bypass next-intl entirely — otherwise
  // intlMiddleware rewrites /api/auth/login to /tr/api/auth/login
  // which 404s. /api/* handlers are locale-agnostic by design.
  if (pathname.startsWith("/api/")) {
    return NextResponse.next();
  }

  // Skip auth check for public paths and static assets
  if (
    isPublicPath(pathname) ||
    pathname.startsWith("/_next/") ||
    pathname.includes(".")
  ) {
    return intlMiddleware(request);
  }

  // Check session cookie
  const sessionCookie = request.cookies.get("personel_session");

  if (!sessionCookie?.value) {
    // Determine locale from URL or use default
    const locale = pathname.split("/")[1] ?? "tr";
    const validLocales = routing.locales as readonly string[];
    const safeLocale = validLocales.includes(locale) ? locale : routing.defaultLocale;

    const loginUrl = new URL(`/${safeLocale}/login`, request.url);
    loginUrl.searchParams.set("callbackUrl", pathname);
    return NextResponse.redirect(loginUrl);
  }

  // Pass to next-intl middleware for locale handling
  return intlMiddleware(request);
}

export const config = {
  // Match all paths except Next.js internals and static files
  matcher: ["/((?!_next/static|_next/image|favicon.ico|logo.svg|.*\\..*).*)"],
};
