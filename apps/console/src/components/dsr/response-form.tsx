"use client";

/**
 * DSR Response Form — DPO submits the fulfillment artifact reference.
 *
 * KVKK m.13: Response must be within 30 days.
 * The artifact_ref field should be an object store path (MinIO bucket/key)
 * or a secure download URL — not raw personal data inline.
 */

import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useTranslations } from "next-intl";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { respondDSR, dsrKeys } from "@/lib/api/dsr";
import { toUserFacingError } from "@/lib/errors";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Info } from "lucide-react";

const schema = z.object({
  artifact_ref: z
    .string()
    .min(5, "Yanıt artefaktı referansı en az 5 karakter olmalıdır.")
    .max(500, "Referans çok uzun."),
  notes: z.string().max(1000, "Notlar en fazla 1000 karakter.").optional(),
});

type FormValues = z.infer<typeof schema>;

interface ResponseFormProps {
  dsrId: string;
  onSuccess?: () => void;
}

export function DSRResponseForm({ dsrId, onSuccess }: ResponseFormProps): JSX.Element {
  const t = useTranslations("dsr.response");
  const qc = useQueryClient();

  const mutation = useMutation({
    mutationFn: (values: FormValues) =>
      respondDSR(dsrId, {
        artifact_ref: values.artifact_ref,
        notes: values.notes,
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: dsrKeys.detail(dsrId) });
      void qc.invalidateQueries({ queryKey: dsrKeys.all });
      toast.success(t("successToast"));
      onSuccess?.();
    },
    onError: (err) => {
      const ue = toUserFacingError(err);
      toast.error(ue.title, { description: ue.description });
    },
  });

  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<FormValues>({
    resolver: zodResolver(schema),
  });

  return (
    <form
      onSubmit={(e) => void handleSubmit((v) => mutation.mutateAsync(v))(e)}
      className="space-y-4"
      noValidate
    >
      <Alert variant="default" role="note">
        <Info className="h-4 w-4" aria-hidden="true" />
        <AlertDescription className="text-xs">{t("artifactNote")}</AlertDescription>
      </Alert>

      <div className="space-y-2">
        <Label htmlFor="artifact_ref">{t("artifactLabel")}</Label>
        <Input
          id="artifact_ref"
          placeholder={t("artifactPlaceholder")}
          {...register("artifact_ref")}
          aria-describedby={errors.artifact_ref ? "ar-err" : undefined}
        />
        {errors.artifact_ref && (
          <p id="ar-err" className="text-xs text-destructive" role="alert">
            {errors.artifact_ref.message}
          </p>
        )}
        <p className="text-xs text-muted-foreground">{t("artifactHint")}</p>
      </div>

      <div className="space-y-2">
        <Label htmlFor="notes">{t("notesLabel")}</Label>
        <Input
          id="notes"
          placeholder={t("notesPlaceholder")}
          {...register("notes")}
        />
      </div>

      <Button
        type="submit"
        disabled={isSubmitting || mutation.isPending}
      >
        {isSubmitting || mutation.isPending ? t("submitting") : t("submit")}
      </Button>
    </form>
  );
}
