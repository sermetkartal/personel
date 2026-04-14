"use client";

import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useTranslations } from "next-intl";
import { useRouter } from "next/navigation";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { submitDSR, dsrKeys } from "@/lib/api/dsr";
import { toUserFacingError } from "@/lib/errors";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

const DSR_TYPES = [
  { value: "access", label: "Bilgi Talebi" },
  { value: "rectify", label: "Düzeltme Talebi" },
  { value: "erase", label: "Silme Talebi" },
  { value: "object", label: "İşlemeye İtiraz" },
  { value: "restrict", label: "Kısıtlama Talebi" },
  { value: "portability", label: "Taşınabilirlik Talebi" },
] as const;

const schema = z.object({
  request_type: z.enum(["access", "rectify", "erase", "object", "restrict", "portability"], {
    errorMap: () => ({ message: "Bir talep türü seçmelisiniz." }),
  }),
});

type FormValues = z.infer<typeof schema>;

export function DSRNewForm(): JSX.Element {
  const t = useTranslations("dsr.new");
  const router = useRouter();
  const qc = useQueryClient();

  const mutation = useMutation({
    mutationFn: (values: FormValues) => submitDSR({ request_type: values.request_type }),
    onSuccess: (created) => {
      void qc.invalidateQueries({ queryKey: dsrKeys.all });
      toast.success(t("successToast"));
      router.push(`../${created.id}`);
    },
    onError: (err) => {
      const ufe = toUserFacingError(err);
      toast.error(ufe.title, { description: ufe.description });
    },
  });

  const {
    setValue,
    watch,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<FormValues>({
    resolver: zodResolver(schema),
  });

  return (
    <form
      onSubmit={(e) => void handleSubmit((v) => mutation.mutateAsync(v))(e)}
      className="space-y-6"
      noValidate
    >
      <div className="space-y-2">
        <Label htmlFor="request_type">{t("typeLabel")}</Label>
        <Select
          value={watch("request_type")}
          onValueChange={(v) =>
            setValue("request_type", v as FormValues["request_type"], { shouldValidate: true })
          }
        >
          <SelectTrigger id="request_type" aria-describedby={errors.request_type ? "rt-err" : undefined}>
            <SelectValue placeholder={t("typePlaceholder")} />
          </SelectTrigger>
          <SelectContent>
            {DSR_TYPES.map((dt) => (
              <SelectItem key={dt.value} value={dt.value}>
                {dt.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {errors.request_type && (
          <p id="rt-err" className="text-xs text-destructive" role="alert">
            {errors.request_type.message}
          </p>
        )}
        <p className="text-xs text-muted-foreground">{t("slaNote")}</p>
      </div>

      <div className="flex items-center gap-3">
        <Button type="submit" disabled={isSubmitting || mutation.isPending}>
          {isSubmitting || mutation.isPending ? t("submitting") : t("submit")}
        </Button>
        <Button type="button" variant="outline" onClick={() => router.back()}>
          {t("cancel")}
        </Button>
      </div>
    </form>
  );
}
