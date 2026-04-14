"use client";

import { useTranslations } from "next-intl";
import { useState } from "react";
import { useRouter } from "next/navigation";
import { useMutation } from "@tanstack/react-query";
import { toast } from "sonner";
import { Plus } from "lucide-react";

import { createTenant } from "@/lib/api/settings";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

interface NewTenantFormProps {
  locale: string;
}

const SLUG_RE = /^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$/;

export function NewTenantForm({ locale }: NewTenantFormProps): JSX.Element {
  const t = useTranslations("settings.tenants");
  const router = useRouter();
  const [displayName, setDisplayName] = useState("");
  const [slug, setSlug] = useState("");
  const [slugErr, setSlugErr] = useState<string | null>(null);

  const mutation = useMutation({
    mutationFn: async () =>
      createTenant({
        display_name: displayName.trim(),
        slug: slug.trim(),
      }),
    onSuccess: (created) => {
      toast.success(t("createSuccess"));
      router.push(`/${locale}/settings/tenants/${created.id}`);
    },
    onError: (err: unknown) => {
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(`${t("createError")}: ${msg}`);
    },
  });

  function handleSlugChange(v: string): void {
    const normalised = v.toLowerCase().replace(/[^a-z0-9-]/g, "-");
    setSlug(normalised);
    setSlugErr(
      normalised && !SLUG_RE.test(normalised) ? "slugInvalid" : null,
    );
  }

  const canSubmit =
    displayName.trim().length >= 2 &&
    slug.length >= 2 &&
    SLUG_RE.test(slug) &&
    !mutation.isPending;

  return (
    <form
      className="space-y-4"
      onSubmit={(e) => {
        e.preventDefault();
        if (canSubmit) mutation.mutate();
      }}
    >
      <div className="space-y-2">
        <Label htmlFor="new-name">{t("displayName")}</Label>
        <Input
          id="new-name"
          value={displayName}
          onChange={(e) => setDisplayName(e.target.value)}
          placeholder={t("displayNamePlaceholder")}
          maxLength={120}
          required
        />
      </div>
      <div className="space-y-2">
        <Label htmlFor="new-slug">{t("slug")}</Label>
        <Input
          id="new-slug"
          value={slug}
          onChange={(e) => handleSlugChange(e.target.value)}
          placeholder="acme-corp"
          className={
            slugErr
              ? "border-red-500 focus-visible:ring-red-500 font-mono"
              : "font-mono"
          }
          aria-invalid={slugErr !== null}
          aria-describedby={slugErr ? "new-slug-err" : undefined}
          required
        />
        {slugErr && (
          <p id="new-slug-err" className="text-[11px] text-red-500">
            {t(`errors.${slugErr}`)}
          </p>
        )}
        <p className="text-[11px] text-muted-foreground">{t("slugHint")}</p>
      </div>
      <div className="flex justify-end gap-2">
        <Button type="submit" disabled={!canSubmit}>
          <Plus className="mr-2 h-4 w-4" aria-hidden="true" />
          {mutation.isPending ? t("creating") : t("create")}
        </Button>
      </div>
    </form>
  );
}
