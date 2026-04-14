"use client";

import { useState } from "react";
import { useTranslations } from "next-intl";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  Save,
  Lock,
  Loader2,
  ShieldCheck,
  AlertTriangle,
  Globe,
  Building,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent } from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { cn } from "@/lib/utils";

import {
  updateCaMode,
  settingsKeys,
  type CaMode,
  type CaModeInfo,
} from "@/lib/api/settings-extended";
import { toUserFacingError } from "@/lib/errors";

interface Props {
  initial: CaModeInfo | null;
  token?: string;
  canEdit: boolean;
}

const DNS_PROVIDERS = ["route53", "cloudflare", "digitalocean", "manual"];

export function CaModeForm({ initial, token, canEdit }: Props): JSX.Element {
  const t = useTranslations("settings.tls");
  const qc = useQueryClient();

  const [mode, setMode] = useState<CaMode>(initial?.mode ?? "internal");
  const [config, setConfig] = useState<Record<string, string>>(
    initial?.config ?? {},
  );
  const [confirmOpen, setConfirmOpen] = useState(false);

  const mutation = useMutation({
    mutationFn: () => updateCaMode({ mode, config }, { token }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: settingsKeys.caMode });
      toast.success(t("saveSuccess"));
      setConfirmOpen(false);
    },
    onError: (err) => {
      const ufe = toUserFacingError(err);
      toast.error(t("saveError"), { description: ufe.description });
    },
  });

  function update(key: string, value: string): void {
    setConfig((c) => ({ ...c, [key]: value }));
  }

  function handleSubmit(e: React.FormEvent): void {
    e.preventDefault();
    setConfirmOpen(true);
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-6">
      <div className="grid gap-3 md:grid-cols-3">
        <ModeCard
          active={mode === "letsencrypt"}
          onClick={() => canEdit && setMode("letsencrypt")}
          icon={Globe}
          title={t("mode.letsencrypt.title")}
          description={t("mode.letsencrypt.description")}
          disabled={!canEdit}
        />
        <ModeCard
          active={mode === "internal"}
          onClick={() => canEdit && setMode("internal")}
          icon={ShieldCheck}
          title={t("mode.internal.title")}
          description={t("mode.internal.description")}
          badge={t("mode.internal.badge")}
          disabled={!canEdit}
        />
        <ModeCard
          active={mode === "commercial"}
          onClick={() => canEdit && setMode("commercial")}
          icon={Building}
          title={t("mode.commercial.title")}
          description={t("mode.commercial.description")}
          disabled={!canEdit}
        />
      </div>

      {mode === "letsencrypt" && (
        <Card>
          <CardContent className="space-y-4 pt-6">
            <div className="grid gap-4 md:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="dns-provider">{t("letsencrypt.dnsProvider")}</Label>
                <Select
                  value={config.dns_provider ?? "manual"}
                  onValueChange={(v) => update("dns_provider", v)}
                  disabled={!canEdit}
                >
                  <SelectTrigger id="dns-provider">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {DNS_PROVIDERS.map((p) => (
                      <SelectItem key={p} value={p}>
                        {t(`letsencrypt.provider.${p}`)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-2">
                <Label htmlFor="le-email">{t("letsencrypt.email")}</Label>
                <Input
                  id="le-email"
                  type="email"
                  value={config.email ?? ""}
                  onChange={(e) => update("email", e.target.value)}
                  placeholder="dpo@firma.com.tr"
                  disabled={!canEdit}
                />
              </div>
            </div>
            <p className="text-xs text-muted-foreground">
              {t("letsencrypt.hint")}
            </p>
          </CardContent>
        </Card>
      )}

      {mode === "internal" && (
        <Card>
          <CardContent className="pt-6">
            <div className="flex items-start gap-3">
              <Lock className="mt-0.5 h-5 w-5 text-muted-foreground" />
              <div>
                <p className="text-sm font-medium">
                  {t("internal.headline")}
                </p>
                <p className="mt-1 text-xs text-muted-foreground">
                  {t("internal.body")}
                </p>
              </div>
            </div>
          </CardContent>
        </Card>
      )}

      {mode === "commercial" && (
        <Card>
          <CardContent className="space-y-4 pt-6">
            <div className="space-y-2">
              <Label htmlFor="cert-issuer">{t("commercial.issuer")}</Label>
              <Input
                id="cert-issuer"
                value={config.issuer ?? ""}
                onChange={(e) => update("issuer", e.target.value)}
                placeholder="DigiCert / Sectigo / ..."
                disabled={!canEdit}
              />
            </div>
            <div className="flex flex-wrap gap-2">
              <Button type="button" variant="outline" size="sm" disabled>
                {t("commercial.downloadCsr")}
              </Button>
              <Button type="button" variant="outline" size="sm" disabled>
                {t("commercial.uploadChain")}
              </Button>
            </div>
            <p className="text-xs italic text-muted-foreground">
              {t("commercial.soon")}
            </p>
          </CardContent>
        </Card>
      )}

      <Alert variant="warning">
        <AlertTriangle className="h-4 w-4" />
        <AlertTitle>{t("rotationWarning.title")}</AlertTitle>
        <AlertDescription>{t("rotationWarning.body")}</AlertDescription>
      </Alert>

      {canEdit && (
        <div className="flex justify-end">
          <Button type="submit" disabled={mutation.isPending}>
            <Save className="mr-2 h-4 w-4" />
            {t("save")}
          </Button>
        </div>
      )}

      <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("confirm.title")}</DialogTitle>
            <DialogDescription>{t("confirm.body")}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setConfirmOpen(false)}
              disabled={mutation.isPending}
            >
              {t("confirm.cancel")}
            </Button>
            <Button
              variant="destructive"
              onClick={() => mutation.mutate()}
              disabled={mutation.isPending}
            >
              {mutation.isPending ? (
                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              ) : (
                <Save className="mr-2 h-4 w-4" />
              )}
              {t("confirm.ok")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </form>
  );
}

// ────────────────────────────────────────────────────────────────────────────
// Radio-card presentation (shared Switch has no RadioGroup)
// ────────────────────────────────────────────────────────────────────────────

interface ModeCardProps {
  active: boolean;
  onClick: () => void;
  icon: React.ElementType;
  title: string;
  description: string;
  badge?: string;
  disabled?: boolean;
}

function ModeCard({
  active,
  onClick,
  icon: Icon,
  title,
  description,
  badge,
  disabled,
}: ModeCardProps): JSX.Element {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className={cn(
        "relative rounded-lg border p-4 text-left transition-colors",
        active
          ? "border-primary bg-primary/5 ring-1 ring-primary"
          : "hover:bg-muted/40",
        disabled && "cursor-not-allowed opacity-60",
      )}
      aria-pressed={active}
    >
      <div className="flex items-start gap-2">
        <Icon className="mt-0.5 h-5 w-5 text-primary" />
        <div className="flex-1">
          <div className="flex items-center gap-2">
            <p className="text-sm font-semibold">{title}</p>
            {badge && (
              <span className="rounded-full bg-primary/10 px-2 py-0.5 text-[10px] font-medium text-primary">
                {badge}
              </span>
            )}
          </div>
          <p className="mt-1 text-xs text-muted-foreground">{description}</p>
        </div>
      </div>
    </button>
  );
}
