"use client";

import { useState } from "react";
import { useTranslations } from "next-intl";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Save, TestTube2, Trash2, Loader2 } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";

import {
  listIntegrations,
  upsertIntegration,
  deleteIntegration,
  settingsKeys,
  SERVICE_NAMES,
  SERVICE_SCHEMAS,
  type ServiceName,
  type IntegrationRecord,
} from "@/lib/api/settings-extended";
import { toUserFacingError } from "@/lib/errors";

interface Props {
  token?: string;
}

/**
 * Loads all 5 external integration records and renders them as a grid
 * of cards. Each card is self-contained: its own form state, its own
 * upsert mutation, and its own confirmation dialog for delete.
 */
export function ExternalIntegrationsClient({ token }: Props): JSX.Element {
  const t = useTranslations("settings.integrations");
  const qc = useQueryClient();

  const query = useQuery({
    queryKey: settingsKeys.integrations,
    queryFn: ({ signal }) => listIntegrations({ token, signal }),
  });

  if (query.isLoading) {
    return (
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" />
        {t("loading")}
      </div>
    );
  }

  // Merge empty placeholders for services that don't exist yet so the
  // grid always renders all five cards in a stable order.
  const byService = new Map<ServiceName, IntegrationRecord>();
  for (const item of query.data?.items ?? []) {
    byService.set(item.service, item);
  }

  return (
    <div className="grid gap-4 md:grid-cols-2">
      {SERVICE_NAMES.map((service) => (
        <IntegrationCard
          key={service}
          service={service}
          record={byService.get(service) ?? null}
          token={token}
          onChanged={() => {
            void qc.invalidateQueries({ queryKey: settingsKeys.integrations });
          }}
        />
      ))}
    </div>
  );
}

// ────────────────────────────────────────────────────────────────────────────
// Per-service card
// ────────────────────────────────────────────────────────────────────────────

interface CardProps {
  service: ServiceName;
  record: IntegrationRecord | null;
  token?: string;
  onChanged: () => void;
}

function IntegrationCard({
  service,
  record,
  token,
  onChanged,
}: CardProps): JSX.Element {
  const t = useTranslations("settings.integrations");
  const schema = SERVICE_SCHEMAS[service];

  // Initialise form state from the masked record. The user must re-enter
  // any password field whose current value is masked (e.g. "sk_••••1234")
  // — we do NOT treat the masked value as a real secret to re-submit.
  const initialConfig = (): Record<string, string> => {
    const next: Record<string, string> = {};
    for (const field of schema.fields) {
      const raw = record?.config[field.key] ?? "";
      // For MaxMind, pre-fill the account ID so the operator doesn't
      // have to retype the static pilot default.
      if (service === "maxmind" && field.key === "account_id" && !raw) {
        next[field.key] = "891169";
      } else {
        next[field.key] = raw;
      }
    }
    return next;
  };

  const [config, setConfig] = useState<Record<string, string>>(initialConfig);
  const [enabled, setEnabled] = useState(record?.enabled ?? false);
  const [confirmDelete, setConfirmDelete] = useState(false);

  const upsert = useMutation({
    mutationFn: () => upsertIntegration(service, { enabled, config }, { token }),
    onSuccess: () => {
      toast.success(t("saveSuccess", { service: schema.label }));
      onChanged();
    },
    onError: (err) => {
      const ufe = toUserFacingError(err);
      toast.error(t("saveError"), { description: ufe.description });
    },
  });

  const del = useMutation({
    mutationFn: () => deleteIntegration(service, { token }),
    onSuccess: () => {
      toast.success(t("deleteSuccess", { service: schema.label }));
      setConfirmDelete(false);
      onChanged();
    },
    onError: (err) => {
      const ufe = toUserFacingError(err);
      toast.error(t("deleteError"), { description: ufe.description });
    },
  });

  const isConfigured = Boolean(record);

  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between space-y-0 pb-3">
        <div className="space-y-1">
          <CardTitle className="text-sm">{schema.label}</CardTitle>
          <p className="text-xs text-muted-foreground">{schema.description}</p>
        </div>
        <Badge variant={isConfigured && enabled ? "success" : "outline"}>
          {isConfigured && enabled ? t("statusEnabled") : t("statusDisabled")}
        </Badge>
      </CardHeader>
      <CardContent className="space-y-3">
        {schema.fields.map((f) => (
          <div key={f.key} className="space-y-1.5">
            <Label htmlFor={`${service}-${f.key}`} className="text-xs">
              {f.label}
            </Label>
            <Input
              id={`${service}-${f.key}`}
              type={f.password ? "password" : "text"}
              placeholder={f.placeholder ?? (f.password ? "••••••••" : "")}
              value={config[f.key] ?? ""}
              onChange={(e) =>
                setConfig((c) => ({ ...c, [f.key]: e.target.value }))
              }
              autoComplete="off"
            />
            {f.password && record?.config[f.key] && (
              <p className="text-[11px] text-muted-foreground">
                {t("passwordMasked")}
              </p>
            )}
          </div>
        ))}

        <div className="flex items-center justify-between pt-2">
          <div className="flex items-center gap-2">
            <Switch
              checked={enabled}
              onCheckedChange={setEnabled}
              id={`${service}-enabled`}
            />
            <Label htmlFor={`${service}-enabled`} className="text-xs">
              {t("enabledLabel")}
            </Label>
          </div>
          {service === "maxmind" && (
            <p className="max-w-[200px] text-[10px] italic text-muted-foreground">
              {t("maxmindHint")}
            </p>
          )}
        </div>

        <div className="flex flex-wrap gap-2 pt-1">
          <Button
            size="sm"
            onClick={() => upsert.mutate()}
            disabled={upsert.isPending}
          >
            {upsert.isPending ? (
              <Loader2 className="mr-1.5 h-3 w-3 animate-spin" />
            ) : (
              <Save className="mr-1.5 h-3 w-3" />
            )}
            {t("save")}
          </Button>
          <Button
            size="sm"
            variant="outline"
            disabled
            title={t("testConnectionSoon")}
          >
            <TestTube2 className="mr-1.5 h-3 w-3" />
            {t("testConnection")}
          </Button>
          {isConfigured && (
            <Button
              size="sm"
              variant="outline"
              className="text-destructive hover:text-destructive"
              onClick={() => setConfirmDelete(true)}
            >
              <Trash2 className="mr-1.5 h-3 w-3" />
              {t("delete")}
            </Button>
          )}
        </div>
      </CardContent>

      <Dialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {t("deleteConfirmTitle", { service: schema.label })}
            </DialogTitle>
            <DialogDescription>{t("deleteConfirmBody")}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setConfirmDelete(false)}
              disabled={del.isPending}
            >
              {t("cancel")}
            </Button>
            <Button
              variant="destructive"
              onClick={() => del.mutate()}
              disabled={del.isPending}
            >
              {del.isPending ? (
                <Loader2 className="mr-1.5 h-3 w-3 animate-spin" />
              ) : (
                <Trash2 className="mr-1.5 h-3 w-3" />
              )}
              {t("deleteConfirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  );
}
