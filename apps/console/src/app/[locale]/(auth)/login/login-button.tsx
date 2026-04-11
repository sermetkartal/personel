"use client";

import { useTranslations } from "next-intl";
import { LogIn } from "lucide-react";
import { Button } from "@/components/ui/button";

interface LoginButtonProps {
  callbackUrl?: string;
}

export function LoginButton({ callbackUrl }: LoginButtonProps): JSX.Element {
  const t = useTranslations("auth.login");

  const loginUrl = callbackUrl
    ? `/api/auth/login?callbackUrl=${encodeURIComponent(callbackUrl)}`
    : "/api/auth/login";

  return (
    <Button className="w-full" size="lg" asChild>
      <a href={loginUrl}>
        <LogIn className="mr-2 h-4 w-4" aria-hidden="true" />
        {t("cta")}
      </a>
    </Button>
  );
}
