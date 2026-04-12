"use client";

import { useTranslations } from "next-intl";
import { Inbox } from "lucide-react";

export function ReportEmpty({
  titleKey,
  descKey,
}: {
  titleKey: string;
  descKey: string;
}): JSX.Element {
  const t = useTranslations("reports");
  return (
    <div className="flex flex-col items-center justify-center rounded-xl border border-dashed bg-card py-16 text-center">
      <Inbox className="mb-3 h-10 w-10 text-muted-foreground/50" />
      <div className="text-base font-medium">{t(titleKey)}</div>
      <div className="mt-1 max-w-md text-sm text-muted-foreground">
        {t(descKey)}
      </div>
    </div>
  );
}
