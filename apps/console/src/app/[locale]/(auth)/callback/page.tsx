"use client";

/**
 * OAuth2 PKCE callback page.
 * Exchanges the authorization code for tokens, then redirects.
 *
 * This runs client-side because it reads the URL hash/search params
 * and must exchange them against our Next.js API route (not a server action)
 * to maintain the cookie boundary correctly.
 */

import { useEffect, useRef } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { useTranslations } from "next-intl";
import { Loader2, ShieldAlert } from "lucide-react";
import { useState } from "react";

export default function CallbackPage(): JSX.Element {
  const t = useTranslations("auth.callback");
  const router = useRouter();
  const searchParams = useSearchParams();
  const [error, setError] = useState<string | null>(null);
  const called = useRef(false);

  useEffect(() => {
    // Guard against StrictMode double-invoke
    if (called.current) return;
    called.current = true;

    const code = searchParams.get("code");
    const state = searchParams.get("state");
    const errorParam = searchParams.get("error");

    if (errorParam) {
      setError(t("authError", { error: errorParam }));
      return;
    }

    if (!code || !state) {
      setError(t("missingParams"));
      return;
    }

    void (async () => {
      try {
        const res = await fetch("/api/auth/callback", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ code, state }),
        });

        if (!res.ok) {
          const body = (await res.json().catch(() => ({}))) as { detail?: string };
          setError(body.detail ?? t("exchangeFailed"));
          return;
        }

        const data = (await res.json()) as { redirect_to?: string };
        router.replace(data.redirect_to ?? "/tr/dashboard");
      } catch {
        setError(t("networkError"));
      }
    })();
  }, [searchParams, router, t]);

  if (error) {
    return (
      <div className="flex min-h-svh flex-col items-center justify-center gap-4 p-6 text-center">
        <ShieldAlert className="h-10 w-10 text-destructive" aria-hidden="true" />
        <h1 className="text-xl font-semibold">{t("errorTitle")}</h1>
        <p className="text-sm text-muted-foreground max-w-sm">{error}</p>
        <a
          href="/api/auth/login"
          className="text-sm text-primary underline underline-offset-4"
        >
          {t("tryAgain")}
        </a>
      </div>
    );
  }

  return (
    <div className="flex min-h-svh flex-col items-center justify-center gap-4 p-6">
      <Loader2 className="h-8 w-8 animate-spin text-primary" aria-hidden="true" />
      <p className="text-sm text-muted-foreground">{t("loading")}</p>
    </div>
  );
}
