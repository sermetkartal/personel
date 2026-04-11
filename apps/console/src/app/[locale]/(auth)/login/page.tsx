import type { Metadata } from "next";
import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { LoginButton } from "./login-button";
import { Shield } from "lucide-react";

interface LoginPageProps {
  params: Promise<{ locale: string }>;
  searchParams: Promise<{ callbackUrl?: string }>;
}

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations("auth");
  return { title: t("login.title") };
}

export default async function LoginPage({
  params,
  searchParams,
}: LoginPageProps): Promise<JSX.Element> {
  const { locale } = await params;
  const { callbackUrl } = await searchParams;
  const session = await getSession();

  // Already authenticated — redirect
  if (session?.user) {
    redirect(callbackUrl ?? `/${locale}/dashboard`);
  }

  const t = await getTranslations("auth");

  return (
    <div className="flex min-h-svh flex-col items-center justify-center bg-background p-6">
      <div className="w-full max-w-sm space-y-8">
        {/* Logo / brand */}
        <div className="flex flex-col items-center gap-3 text-center">
          <div className="flex h-14 w-14 items-center justify-center rounded-full bg-primary/10">
            <Shield className="h-7 w-7 text-primary" aria-hidden="true" />
          </div>
          <div>
            <h1 className="text-2xl font-bold tracking-tight">{t("login.heading")}</h1>
            <p className="text-sm text-muted-foreground mt-1">{t("login.subheading")}</p>
          </div>
        </div>

        {/* Login card */}
        <div className="rounded-xl border bg-card shadow-sm p-6 space-y-4">
          <p className="text-sm text-muted-foreground text-center">
            {t("login.ssoNote")}
          </p>
          <LoginButton callbackUrl={callbackUrl} />
        </div>

        {/* KVKK notice */}
        <p className="text-xs text-center text-muted-foreground leading-relaxed px-2">
          {t("login.kvkkNotice")}
        </p>
      </div>
    </div>
  );
}
