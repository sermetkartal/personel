import type { Metadata } from "next";
import { getTranslations } from "next-intl/server";
import { Link } from "@/lib/i18n/navigation";
import { Button } from "@/components/ui/button";
import { ShieldOff, ChevronLeft } from "lucide-react";

interface UnauthorizedPageProps {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations("unauthorized");
  return { title: t("title") };
}

export default async function UnauthorizedPage({
  params,
}: UnauthorizedPageProps): Promise<JSX.Element> {
  await params; // Consume params to satisfy Next.js
  const t = await getTranslations("unauthorized");

  return (
    <div className="flex min-h-svh flex-col items-center justify-center gap-6 p-6 text-center">
      <div className="flex h-16 w-16 items-center justify-center rounded-full bg-destructive/10">
        <ShieldOff className="h-8 w-8 text-destructive" aria-hidden="true" />
      </div>
      <div className="space-y-2">
        <h1 className="text-2xl font-bold">{t("heading")}</h1>
        <p className="text-muted-foreground max-w-sm">{t("body")}</p>
      </div>
      <Button asChild variant="outline">
        <Link href="/dashboard">
          <ChevronLeft className="mr-2 h-4 w-4" aria-hidden="true" />
          {t("backToDashboard")}
        </Link>
      </Button>
    </div>
  );
}
