"use client";

import { useTranslations } from "next-intl";
import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { toast } from "sonner";
import { Save } from "lucide-react";

import type { Tenant } from "@/lib/api/types";
import { updateTenant } from "@/lib/api/settings";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";

interface TenantEditFormProps {
  tenant: Tenant;
}

export function TenantEditForm({ tenant }: TenantEditFormProps): JSX.Element {
  const t = useTranslations("settings.tenants");
  const [displayName, setDisplayName] = useState(tenant.display_name);
  const [maxRetention, setMaxRetention] = useState(
    String(tenant.settings?.max_screenshot_retention_days ?? 180),
  );
  const [liveViewRestricted, setLiveViewRestricted] = useState(
    tenant.settings?.live_view_history_restricted ?? true,
  );

  const mutation = useMutation({
    mutationFn: async () => {
      const days = parseInt(maxRetention, 10);
      return updateTenant(tenant.id, {
        display_name: displayName.trim() || undefined,
        settings: {
          max_screenshot_retention_days: Number.isFinite(days) ? days : undefined,
          live_view_history_restricted: liveViewRestricted,
        },
      });
    },
    onSuccess: () => {
      toast.success(t("saveSuccess"));
    },
    onError: (err: unknown) => {
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(`${t("saveError")}: ${msg}`);
    },
  });

  return (
    <form
      className="space-y-4"
      onSubmit={(e) => {
        e.preventDefault();
        mutation.mutate();
      }}
    >
      <div className="grid gap-4 md:grid-cols-2">
        <div className="space-y-2">
          <Label htmlFor="tenant-name">{t("displayName")}</Label>
          <Input
            id="tenant-name"
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            maxLength={120}
            required
          />
        </div>
        <div className="space-y-2">
          <Label htmlFor="tenant-slug">{t("slug")}</Label>
          <Input
            id="tenant-slug"
            value={tenant.slug}
            disabled
            className="font-mono"
          />
          <p className="text-[11px] text-muted-foreground">{t("slugImmutable")}</p>
        </div>
        <div className="space-y-2">
          <Label htmlFor="retention">{t("screenshotRetentionDays")}</Label>
          <Input
            id="retention"
            type="number"
            min={1}
            max={3650}
            value={maxRetention}
            onChange={(e) => setMaxRetention(e.target.value)}
          />
          <p className="text-[11px] text-muted-foreground">
            {t("retentionHintKvkk")}
          </p>
        </div>
        <div className="space-y-2">
          <Label
            htmlFor="lv-restricted"
            className="flex cursor-pointer items-center justify-between gap-3 rounded-md border p-3"
          >
            <div>
              <div className="font-medium">{t("liveViewHistoryRestricted")}</div>
              <div className="text-[11px] text-muted-foreground">
                {t("liveViewHistoryRestrictedHint")}
              </div>
            </div>
            <Switch
              id="lv-restricted"
              checked={liveViewRestricted}
              onCheckedChange={setLiveViewRestricted}
            />
          </Label>
        </div>
      </div>
      <div className="flex justify-end">
        <Button type="submit" disabled={mutation.isPending}>
          <Save className="mr-2 h-4 w-4" aria-hidden="true" />
          {mutation.isPending ? t("saving") : t("save")}
        </Button>
      </div>
    </form>
  );
}
