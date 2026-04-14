/**
 * Admin API reverse proxy.
 *
 * Client components POST/GET/etc. to `/api/proxy/v1/...` which this route
 * forwards to the real Admin API with the session user's access token.
 * Keeps the token server-side (httpOnly cookie never enters the browser
 * bundle) while still letting client components use the typed apiClient.
 *
 * Previously client.ts pointed directly at the API and relied on a
 * browser-side token store that was never populated after login —
 * every client fetch went unauthenticated and the API returned 401.
 */

import { type NextRequest, NextResponse } from "next/server";
import { getSession } from "@/lib/auth/session";

const API_BASE =
  process.env.NEXT_PUBLIC_API_BASE_URL ?? "http://localhost:8080";

type RouteContext = {
  params: Promise<{ path: string[] }>;
};

async function forward(
  request: NextRequest,
  context: RouteContext,
): Promise<NextResponse> {
  const { path } = await context.params;
  const session = await getSession();

  const search = request.nextUrl.search;
  const upstream = `${API_BASE}/${path.join("/")}${search}`;

  // Clone headers but replace Authorization with the session token so
  // the real bearer never touches the browser. Drop Host / X-Forwarded-*
  // which upstream Envoy/Nginx injects its own values for.
  const headers = new Headers(request.headers);
  headers.delete("host");
  headers.delete("x-forwarded-for");
  headers.delete("x-forwarded-proto");
  headers.delete("x-forwarded-host");
  if (session?.user?.access_token) {
    headers.set("authorization", `Bearer ${session.user.access_token}`);
  } else {
    headers.delete("authorization");
  }

  const init: RequestInit = {
    method: request.method,
    headers,
    // For non-body verbs, RequestInit must not set body.
    body:
      request.method === "GET" || request.method === "HEAD"
        ? undefined
        : await request.arrayBuffer(),
    // Bypass Next.js data cache — these are always dynamic.
    cache: "no-store",
    redirect: "manual",
  };

  let upstreamRes: Response;
  try {
    upstreamRes = await fetch(upstream, init);
  } catch (err) {
    return NextResponse.json(
      {
        type: "https://personel.internal/problems/upstream-unreachable",
        title: "Upstream unreachable",
        status: 502,
        detail: err instanceof Error ? err.message : String(err),
      },
      { status: 502 },
    );
  }

  // Stream body through; preserve status + headers (minus hop-by-hop).
  const respHeaders = new Headers(upstreamRes.headers);
  respHeaders.delete("transfer-encoding");
  respHeaders.delete("connection");
  respHeaders.delete("content-encoding");

  return new NextResponse(upstreamRes.body, {
    status: upstreamRes.status,
    headers: respHeaders,
  });
}

export async function GET(request: NextRequest, context: RouteContext) {
  return forward(request, context);
}
export async function POST(request: NextRequest, context: RouteContext) {
  return forward(request, context);
}
export async function PUT(request: NextRequest, context: RouteContext) {
  return forward(request, context);
}
export async function PATCH(request: NextRequest, context: RouteContext) {
  return forward(request, context);
}
export async function DELETE(request: NextRequest, context: RouteContext) {
  return forward(request, context);
}
