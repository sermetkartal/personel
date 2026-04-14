"use client";

import { useTranslations } from "next-intl";
import { useState } from "react";
import { toast } from "sonner";
import { Save, Camera } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  updateTenantScreenshotPreset,
  type ScreenshotPreset,
} from "@/lib/api/settings";

interface GeneralSettingsFormProps {
  initialDisplayName: string;
  initialSlug: string;
  initialLocale: string;
  initialTimezone: string;
  initialScreenshotPreset: ScreenshotPreset;
  canEdit: boolean;
  canEditScreenshot: boolean;
  token?: string;
}

const SCREENSHOT_PRESETS: ScreenshotPreset[] = [
  "minimal",
  "low",
  "medium",
  "high",
  "max",
];

const LOCALES = [
  { value: "tr", label: "Türkçe" },
  { value: "en", label: "English" },
];

// Common TR + neighbouring timezones; full IANA list is overkill for the
// admin console. If ops need a wider set this can be data-driven.
const TIMEZONES = [
  "Europe/Istanbul",
  "Europe/Berlin",
  "Europe/London",
  "UTC",
];

export function GeneralSettingsForm({
  initialDisplayName,
  initialSlug,
  initialLocale,
  initialTimezone,
  initialScreenshotPreset,
  canEdit,
  canEditScreenshot,
  token,
}: GeneralSettingsFormProps): JSX.Element {
  const t = useTranslations("settings.general");
  const [displayName, setDisplayName] = useState(initialDisplayName);
  const [defaultLocale, setDefaultLocale] = useState(initialLocale);
  const [timezone, setTimezone] = useState(initialTimezone);
  const [screenshotPreset, setScreenshotPreset] =
    useState<ScreenshotPreset>(initialScreenshotPreset);
  const [savingScreenshot, setSavingScreenshot] = useState(false);
  const [saving, setSaving] = useState(false);

  async function handleScreenshotPresetChange(next: ScreenshotPreset): Promise<void> {
    if (!canEditScreenshot || next === screenshotPreset) {
      return;
    }
    const previous = screenshotPreset;
    setScreenshotPreset(next);
    setSavingScreenshot(true);
    try {
      await updateTenantScreenshotPreset(next, { token });
      toast.success(t("screenshot.saveSuccess", { preset: t(`screenshot.preset.${next}`) }));
    } catch (err) {
      setScreenshotPreset(previous); // rollback local state on failure
      toast.error(t("screenshot.saveError"));
      // eslint-disable-next-line no-console
      console.error("updateTenantScreenshotPreset failed", err);
    } finally {
      setSavingScreenshot(false);
    }
  }

  async function handleSubmit(e: React.FormEvent): Promise<void> {
    e.preventDefault();
    // General tenant-wide preferences are not yet exposed on /v1/tenants/me
    // or a dedicated preferences endpoint — this scaffold captures input
    // shape so the wiring is a thin mutation addition once the backend
    // column lands. Ops surface the same info via `settings/tenants/[id]`
    // for now.
    setSaving(true);
    await new Promise((r) => setTimeout(r, 400));
    setSaving(false);
    toast.info(t("saveScaffold"));
  }

  return (
    <form className="space-y-4" onSubmit={handleSubmit}>
      <div className="grid gap-4 md:grid-cols-2">
        <div className="space-y-2">
          <Label htmlFor="gen-name">{t("displayName")}</Label>
          <Input
            id="gen-name"
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            placeholder={t("displayNamePlaceholder")}
            disabled={!canEdit}
            maxLength={120}
          />
        </div>
        <div className="space-y-2">
          <Label htmlFor="gen-locale">{t("defaultLocale")}</Label>
          <Select
            value={defaultLocale}
            onValueChange={setDefaultLocale}
            disabled={!canEdit}
          >
            <SelectTrigger id="gen-locale">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {LOCALES.map((l) => (
                <SelectItem key={l.value} value={l.value}>
                  {l.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-2 md:col-span-2">
          <Label htmlFor="gen-tz">{t("timezone")}</Label>
          <Select
            value={timezone}
            onValueChange={setTimezone}
            disabled={!canEdit}
          >
            <SelectTrigger id="gen-tz">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {TIMEZONES.map((tz) => (
                <SelectItem key={tz} value={tz}>
                  {tz}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <p className="text-[11px] text-muted-foreground">{t("timezoneHint")}</p>
        </div>
      </div>
      {canEdit && (
        <div className="flex justify-end">
          <Button type="submit" disabled={saving}>
            <Save className="mr-2 h-4 w-4" aria-hidden="true" />
            {saving ? t("saving") : t("save")}
          </Button>
        </div>
      )}

      {/* ── Screenshot capture preset ──────────────────────────────── */}
      <section className="mt-8 border-t pt-6 space-y-4">
        <div className="flex items-start gap-2">
          <Camera className="h-5 w-5 text-primary mt-0.5" aria-hidden="true" />
          <div className="flex-1">
            <h3 className="text-sm font-semibold">{t("screenshot.title")}</h3>
            <p className="text-xs text-muted-foreground mt-1">
              {t("screenshot.description")}
            </p>
          </div>
        </div>
        <div className="grid gap-4 md:grid-cols-2">
          <div className="space-y-2">
            <Label htmlFor="screenshot-preset">
              {t("screenshot.presetLabel")}
            </Label>
            <Select
              value={screenshotPreset}
              onValueChange={(v) => handleScreenshotPresetChange(v as ScreenshotPreset)}
              disabled={!canEditScreenshot || savingScreenshot}
            >
              <SelectTrigger id="screenshot-preset">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {SCREENSHOT_PRESETS.map((p) => (
                  <SelectItem key={p} value={p}>
                    {t(`screenshot.preset.${p}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p className="text-[11px] text-muted-foreground">
              {t(`screenshot.hint.${screenshotPreset}`)}
            </p>
          </div>
          <div className="space-y-2">
            <Label className="text-muted-foreground">
              {t("screenshot.currentLabel")}
            </Label>
            <div className="rounded-md border bg-muted/30 px-3 py-2 text-xs font-mono">
              {t(`screenshot.current.${screenshotPreset}`)}
            </div>
          </div>
        </div>
        {!canEditScreenshot && (
          <p className="text-[11px] text-muted-foreground italic">
            {t("screenshot.rbacNote")}
          </p>
        )}
      </section>
    </form>
  );
}
