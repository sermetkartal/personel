"use client";

import { useTranslations } from "next-intl";
import { AlertTriangle } from "lucide-react";
import {
  Alert,
  AlertDescription,
  AlertTitle,
} from "@/components/ui/alert";

export interface ReportErrorProps {
  status?: number;
  code?: string;
  message?: string;
}

function resolveMessageKey(status?: number): string {
  if (status === 403) return "forbidden";
  if (status === 404) return "notFound";
  if (status === 400 || status === 422) return "validation";
  if (status !== undefined && status >= 500) return "server";
  return "unknown";
}

export function ReportError({
  status,
  code: _code,
  message,
}: ReportErrorProps): JSX.Element {
  const t = useTranslations("reports.error");
  const messageKey = resolveMessageKey(status);

  return (
    <Alert variant="destructive" className="my-4">
      <AlertTriangle className="h-4 w-4" />
      <AlertTitle>{t("title")}</AlertTitle>
      <AlertDescription className="mt-2 space-y-1">
        <p>{message ?? t(messageKey)}</p>
        <p className="text-xs opacity-75">{t("retry")}</p>
        <p className="text-xs opacity-50">
          {t("timestamp")}: {new Date().toISOString()}
          {status !== undefined && (
            <span className="ml-2">(HTTP {status})</span>
          )}
        </p>
      </AlertDescription>
    </Alert>
  );
}
