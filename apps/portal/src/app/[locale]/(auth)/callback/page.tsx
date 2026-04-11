import { redirect } from "next/navigation";
import { getLocale } from "next-intl/server";

/**
 * OAuth2 callback page — redirects to the API route handler.
 * The actual callback processing happens in /api/auth/session (server-side).
 * This page exists only as a fallback render target.
 */
export default async function CallbackPage(): Promise<JSX.Element> {
  const locale = await getLocale();
  redirect(`/${locale}`);
}
