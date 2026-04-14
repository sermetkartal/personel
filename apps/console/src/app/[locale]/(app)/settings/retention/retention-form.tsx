"use client";

import { useState } from "react";
import { useTranslations } from "next-intl";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Save, RotateCcw, Loader2, AlertTriangle } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent } from "@/components/ui/card";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";

import {
  updateRetention,
  settingsKeys,
  DEFAULT_KVKK_RETENTION,
  type RetentionPolicy,
  type UpdateRetentionRequest,
} from "@/lib/api/settings-extended";
import { toUserFacingError } from "@/lib/errors";

interface Props {
  initial: RetentionPolicy;
  token?: string;
}

/**
 * Per-field config: key on the policy object, label i18n key,
 * unit suffix (yıl / gün), and the KVKK floor.
 */
const FIELDS: Array<{
  key: keyof UpdateRetentionRequest;
  labelKey: string;
  unitKey: "year" | "day";
  min: number;
}> = [
  { key: "audit_years", labelKey: "auditYears", unitKey: "year", min: 5 },
  { key: "event_days", labelKey: "eventDays", unitKey: "day", min: 365 },
  {
    key: "screenshot_days",
    labelKey: "screenshotDays",
    unitKey: "day",
    min: 30,
  },
  {
    key: "keystroke_days",
    labelKey: "keystrokeDays",
    unitKey: "day",
    min: 180,
  },
  {
    key: "live_view_days",
    labelKey: "liveViewDays",
    unitKey: "day",
    min: 30,
  },
  { key: "dsr_days", labelKey: "dsrDays", unitKey: "day", min: 3650 },
];

export function RetentionForm({ initial, token }: Props): JSX.Element {
  const t = useTranslations("settings.retention");
  const qc = useQueryClient();

  const [values, setValues] = useState<UpdateRetentionRequest>({
    audit_years: initial.audit_years,
    event_days: initial.event_days,
    screenshot_days: initial.screenshot_days,
    keystroke_days: initial.keystroke_days,
    live_view_days: initial.live_view_days,
    dsr_days: initial.dsr_days,
  });
  const [errors, setErrors] = useState<
    Partial<Record<keyof UpdateRetentionRequest, string>>
  >({});

  const mutation = useMutation({
    mutationFn: (payload: UpdateRetentionRequest) =>
      updateRetention(payload, { token }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: settingsKeys.retention });
      toast.success(t("saveSuccess"));
    },
    onError: (err) => {
      const ufe = toUserFacingError(err);
      toast.error(t("saveError"), { description: ufe.description });
    },
  });

  function validate(): boolean {
    const next: typeof errors = {};
    for (const f of FIELDS) {
      const v = values[f.key];
      if (!Number.isInteger(v) || v < f.min) {
        next[f.key] = t("belowMinimum", { min: f.min });
      }
    }
    setErrors(next);
    return Object.keys(next).length === 0;
  }

  function handleSubmit(e: React.FormEvent): void {
    e.preventDefault();
    if (!validate()) return;
    mutation.mutate(values);
  }

  function resetToDefault(): void {
    setValues({
      audit_years: DEFAULT_KVKK_RETENTION.audit_years,
      event_days: DEFAULT_KVKK_RETENTION.event_days,
      screenshot_days: DEFAULT_KVKK_RETENTION.screenshot_days,
      keystroke_days: DEFAULT_KVKK_RETENTION.keystroke_days,
      live_view_days: DEFAULT_KVKK_RETENTION.live_view_days,
      dsr_days: DEFAULT_KVKK_RETENTION.dsr_days,
    });
    setErrors({});
    toast.info(t("resetDone"));
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-6">
      <Alert variant="info">
        <AlertTriangle className="h-4 w-4" />
        <AlertTitle>{t("kvkkFloorTitle")}</AlertTitle>
        <AlertDescription>{t("kvkkFloorBody")}</AlertDescription>
      </Alert>

      <Card>
        <CardContent className="grid gap-4 pt-6 md:grid-cols-2">
          {FIELDS.map((f) => {
            const id = `ret-${f.key}`;
            const err = errors[f.key];
            return (
              <div key={f.key} className="space-y-1.5">
                <Label htmlFor={id}>{t(`field.${f.labelKey}.label`)}</Label>
                <div className="flex items-center gap-2">
                  <Input
                    id={id}
                    type="number"
                    min={f.min}
                    value={values[f.key] ?? 0}
                    onChange={(e) =>
                      setValues((v) => ({
                        ...v,
                        [f.key]: Number(e.target.value) || 0,
                      }))
                    }
                    aria-invalid={Boolean(err)}
                    aria-describedby={err ? `${id}-err` : `${id}-hint`}
                    className="max-w-[160px]"
                  />
                  <span className="text-sm text-muted-foreground">
                    {t(`unit.${f.unitKey}`)}
                  </span>
                </div>
                <p id={`${id}-hint`} className="text-[11px] text-muted-foreground">
                  {t("kvkkMinimum", {
                    min: f.min,
                    unit: t(`unit.${f.unitKey}`),
                  })}
                </p>
                {err && (
                  <p
                    id={`${id}-err`}
                    className="text-xs text-destructive"
                    role="alert"
                  >
                    {err}
                  </p>
                )}
              </div>
            );
          })}
        </CardContent>
      </Card>

      <Alert variant="warning">
        <AlertTriangle className="h-4 w-4" />
        <AlertTitle>{t("ttlWarning.title")}</AlertTitle>
        <AlertDescription>{t("ttlWarning.body")}</AlertDescription>
      </Alert>

      <div className="flex flex-wrap justify-end gap-2">
        <Button type="button" variant="outline" onClick={resetToDefault}>
          <RotateCcw className="mr-2 h-4 w-4" />
          {t("resetToMinimum")}
        </Button>
        <Button type="submit" disabled={mutation.isPending}>
          {mutation.isPending ? (
            <Loader2 className="mr-2 h-4 w-4 animate-spin" />
          ) : (
            <Save className="mr-2 h-4 w-4" />
          )}
          {t("save")}
        </Button>
      </div>
    </form>
  );
}
