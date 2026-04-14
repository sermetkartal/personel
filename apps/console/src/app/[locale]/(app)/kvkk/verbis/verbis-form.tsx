"use client";

import { useState } from "react";
import { useTranslations } from "next-intl";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Save, CheckCircle2, Clock } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { updateVerbis, kvkkKeys, type VerbisInfo } from "@/lib/api/kvkk";
import { toUserFacingError } from "@/lib/errors";

interface VerbisFormProps {
  initial: VerbisInfo | null;
  canEdit: boolean;
}

/**
 * Extracts the YYYY-MM-DD slice of an RFC3339 date string so that it can
 * be fed into a native `<input type="date">`. Returns empty string on
 * invalid / missing input rather than throwing.
 */
function toDateInput(value: string | undefined | null): string {
  if (!value) return "";
  const idx = value.indexOf("T");
  return idx > 0 ? value.slice(0, idx) : value.slice(0, 10);
}

/** Converts a YYYY-MM-DD date input to an RFC3339 midnight UTC string. */
function toRfc3339(dateInput: string): string {
  if (!dateInput) return "";
  return `${dateInput}T00:00:00Z`;
}

export function VerbisForm({ initial, canEdit }: VerbisFormProps): JSX.Element {
  const t = useTranslations("kvkk.verbis");
  const qc = useQueryClient();

  const [registrationNumber, setRegistrationNumber] = useState(
    initial?.registration_number ?? "",
  );
  const [registeredAt, setRegisteredAt] = useState(
    toDateInput(initial?.registered_at),
  );
  const [errors, setErrors] = useState<{ number?: string; date?: string }>({});

  const isRegistered = Boolean(initial?.registration_number);

  const mutation = useMutation({
    mutationFn: () =>
      updateVerbis({
        registration_number: registrationNumber.trim(),
        registered_at: toRfc3339(registeredAt),
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: kvkkKeys.verbis });
      toast.success(t("saveSuccess"));
    },
    onError: (err) => {
      const ufe = toUserFacingError(err);
      toast.error(t("saveError"), { description: ufe.description });
    },
  });

  function validate(): boolean {
    const next: { number?: string; date?: string } = {};
    if (!registrationNumber.trim()) {
      next.number = "Gerekli";
    }
    if (!registeredAt) {
      next.date = "Gerekli";
    }
    setErrors(next);
    return Object.keys(next).length === 0;
  }

  function handleSubmit(e: React.FormEvent): void {
    e.preventDefault();
    if (!canEdit || !validate()) return;
    mutation.mutate();
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4" noValidate>
      <div className="flex items-center gap-2">
        {isRegistered ? (
          <Badge variant="success">
            <CheckCircle2 className="mr-1 h-3 w-3" aria-hidden="true" />
            {t("statusRegistered")}
          </Badge>
        ) : (
          <Badge variant="warning">
            <Clock className="mr-1 h-3 w-3" aria-hidden="true" />
            {t("statusPending")}
          </Badge>
        )}
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <div className="space-y-2">
          <Label htmlFor="verbis-number">{t("registrationNumberLabel")}</Label>
          <Input
            id="verbis-number"
            value={registrationNumber}
            onChange={(e) => setRegistrationNumber(e.target.value)}
            placeholder={t("registrationNumberPlaceholder")}
            disabled={!canEdit || mutation.isPending}
            aria-invalid={Boolean(errors.number)}
            aria-describedby={errors.number ? "verbis-number-err" : undefined}
            maxLength={64}
          />
          {errors.number && (
            <p id="verbis-number-err" className="text-xs text-destructive" role="alert">
              {errors.number}
            </p>
          )}
        </div>

        <div className="space-y-2">
          <Label htmlFor="verbis-date">{t("registeredAtLabel")}</Label>
          <Input
            id="verbis-date"
            type="date"
            value={registeredAt}
            onChange={(e) => setRegisteredAt(e.target.value)}
            disabled={!canEdit || mutation.isPending}
            aria-invalid={Boolean(errors.date)}
            aria-describedby={errors.date ? "verbis-date-err" : undefined}
          />
          {errors.date && (
            <p id="verbis-date-err" className="text-xs text-destructive" role="alert">
              {errors.date}
            </p>
          )}
        </div>
      </div>

      {canEdit && (
        <div className="flex justify-end">
          <Button type="submit" disabled={mutation.isPending}>
            <Save className="mr-2 h-4 w-4" aria-hidden="true" />
            {t("save")}
          </Button>
        </div>
      )}
    </form>
  );
}
