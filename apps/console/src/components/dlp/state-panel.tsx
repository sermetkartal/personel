"use client";

/**
 * DLP State Panel — Settings Page Component.
 *
 * Per ADR 0013: DLP is disabled by default. This panel:
 * - Shows the current DLP state
 * - Explains the cryptographic guarantee
 * - Describes the opt-in ceremony (no enable button — deliberate)
 * - Shows instructions for the infra/scripts/dlp-enable.sh operator script
 *
 * CRITICAL: There is intentionally NO "Enable DLP" button in this UI.
 * The console CANNOT bypass the ceremony.
 */

import { useTranslations } from "next-intl";
import {
  Shield,
  ShieldOff,
  Lock,
  FileText,
  Terminal,
  Bell,
  CheckCircle,
  AlertTriangle,
  Copy,
} from "lucide-react";
import { toast } from "sonner";
import { useDLPState } from "@/lib/hooks/use-dlp-state";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Separator } from "@/components/ui/separator";
import { formatDateTR } from "@/lib/utils";

const CEREMONY_STEPS = [
  { stepKey: "1", icon: FileText },
  { stepKey: "2", icon: FileText },
  { stepKey: "3", icon: Terminal },
  { stepKey: "4", icon: Bell },
  { stepKey: "5", icon: CheckCircle },
] as const;

export function DLPStatePanel(): JSX.Element {
  const t = useTranslations("dlp");
  const { data, isLoading } = useDLPState();

  if (isLoading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-20 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  const isActive = data?.state === "active";

  const handleCopyCommand = () => {
    void navigator.clipboard.writeText("infra/scripts/dlp-enable.sh");
    toast.success("Komut kopyalandı.");
  };

  return (
    <div className="space-y-6">
      {/* Current state */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle className="flex items-center gap-2">
              {isActive ? (
                <Shield className="h-5 w-5 text-green-500" aria-hidden="true" />
              ) : (
                <ShieldOff className="h-5 w-5 text-red-500" aria-hidden="true" />
              )}
              {t("state.title")}
            </CardTitle>
            <Badge
              variant={isActive ? "success" : "destructive"}
              className="text-sm px-3 py-1"
            >
              {isActive ? t("statusBadge.active") : t("statusBadge.inactive")}
            </Badge>
          </div>
        </CardHeader>
        <CardContent className="space-y-4">
          {isActive ? (
            <>
              <Alert variant="success">
                <Shield className="h-4 w-4" aria-hidden="true" />
                <AlertTitle>{t("state.active.heading")}</AlertTitle>
                <AlertDescription>{t("state.active.body")}</AlertDescription>
              </Alert>
              {data?.enabled_at && (
                <div className="grid grid-cols-2 gap-4 text-sm">
                  <div>
                    <p className="text-xs text-muted-foreground">{t("state.active.enabledAt")}</p>
                    <p className="font-medium">
                      <time dateTime={data.enabled_at}>
                        {formatDateTR(data.enabled_at)}
                      </time>
                    </p>
                  </div>
                </div>
              )}
            </>
          ) : (
            <>
              <Alert variant="warning">
                <ShieldOff className="h-4 w-4" aria-hidden="true" />
                <AlertTitle>{t("state.disabled.heading")}</AlertTitle>
                <AlertDescription>{t("state.disabled.body")}</AlertDescription>
              </Alert>

              {/* Legal claim */}
              <div className="rounded-md bg-muted/50 border border-border px-4 py-3 text-sm">
                <div className="flex items-start gap-2">
                  <Lock className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
                  <p className="text-muted-foreground leading-relaxed">
                    <span className="font-medium text-foreground">Hukuki Güvence: </span>
                    {t("state.disabled.legalClaim")}
                  </p>
                </div>
              </div>
            </>
          )}
        </CardContent>
      </Card>

      {/* Ceremony instructions — shown when DLP is not active */}
      {!isActive && (
        <Card>
          <CardHeader>
            <CardTitle>{t("ceremony.title")}</CardTitle>
            <CardDescription>{t("ceremony.subtitle")}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-6">
            {/* WARNING: No enable button */}
            <Alert variant="warning" role="note">
              <AlertTriangle className="h-4 w-4" aria-hidden="true" />
              <AlertTitle>Dikkat</AlertTitle>
              <AlertDescription className="font-medium">
                {t("ceremony.warning")}
              </AlertDescription>
            </Alert>

            <Separator />

            {/* Steps */}
            <div className="space-y-4" role="list" aria-label="DLP etkinleştirme adımları">
              {CEREMONY_STEPS.map(({ stepKey, icon: Icon }) => (
                <div
                  key={stepKey}
                  className="flex gap-4"
                  role="listitem"
                >
                  <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full border border-border bg-muted">
                    <Icon className="h-4 w-4 text-muted-foreground" aria-hidden="true" />
                  </div>
                  <div className="flex-1 space-y-1">
                    <p className="font-medium text-sm">
                      {t(`ceremony.steps.${stepKey}.title` as Parameters<typeof t>[0])}
                    </p>
                    <p className="text-sm text-muted-foreground leading-relaxed">
                      {t(`ceremony.steps.${stepKey}.body` as Parameters<typeof t>[0])}
                    </p>
                    {stepKey === "3" && (
                      <div className="mt-2 flex items-center gap-2">
                        <code className="flex-1 rounded bg-muted px-3 py-2 font-mono text-sm">
                          {t("ceremony.steps.3.command" as Parameters<typeof t>[0])}
                        </code>
                        <Button
                          variant="ghost"
                          size="icon"
                          onClick={handleCopyCommand}
                          aria-label="Komutu kopyala"
                          className="shrink-0"
                        >
                          <Copy className="h-4 w-4" aria-hidden="true" />
                        </Button>
                      </div>
                    )}
                  </div>
                </div>
              ))}
            </div>

            <Separator />

            {/* Disable instructions */}
            <div className="space-y-2">
              <h3 className="text-sm font-medium">{t("ceremony.disableTitle")}</h3>
              <p className="text-sm text-muted-foreground">{t("ceremony.disableBody")}</p>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Disable instructions when active */}
      {isActive && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("ceremony.disableTitle")}</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-sm text-muted-foreground">{t("ceremony.disableBody")}</p>
            <div className="mt-3 flex items-center gap-2">
              <code className="flex-1 rounded bg-muted px-3 py-2 font-mono text-sm">
                infra/scripts/dlp-disable.sh
              </code>
              <Button
                variant="ghost"
                size="icon"
                onClick={() => {
                  void navigator.clipboard.writeText("infra/scripts/dlp-disable.sh");
                  toast.success("Komut kopyalandı.");
                }}
                aria-label="Devre dışı bırakma komutunu kopyala"
              >
                <Copy className="h-4 w-4" aria-hidden="true" />
              </Button>
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
