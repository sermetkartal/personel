"use client";

/**
 * Live View Request Form
 *
 * Per ADR 0013 / KVKK m.4 proportionality:
 * - Reason code is required and logged to the audit trail
 * - Duration is capped at 60 minutes (configurable server-side)
 * - The requester cannot approve their own request (dual-control)
 * - All submitted requests enter REQUESTED state awaiting HR approval
 */

import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useTranslations } from "next-intl";
import { useRouter } from "next/navigation";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { listEndpoints, endpointKeys } from "@/lib/api/endpoints";
import { useRequestLiveView } from "@/lib/hooks/use-live-view";
import { toUserFacingError } from "@/lib/errors";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Info } from "lucide-react";

// KVKK-compliant reason codes — must match the API enum
const REASON_CODES = [
  { value: "performance_investigation", label: "Performans Araştırması" },
  { value: "security_incident", label: "Güvenlik Olayı" },
  { value: "policy_violation", label: "Politika İhlali" },
  { value: "technical_support", label: "Teknik Destek" },
  { value: "disciplinary_review", label: "Disiplin Soruşturması" },
] as const;

// Duration options — max 60 min per proportionality principle
const DURATION_OPTIONS = [15, 30, 45, 60] as const;

const requestSchema = z.object({
  endpoint_id: z.string().uuid("Geçerli bir uç nokta seçin."),
  reason_code: z.string().min(1, "Bir neden kodu seçmelisiniz."),
  duration_minutes: z
    .number()
    .int()
    .min(1, "En az 1 dakika.")
    .max(60, "Orantılılık gereği en fazla 60 dakika."),
});

type RequestFormValues = z.infer<typeof requestSchema>;

export function LiveViewRequestForm(): JSX.Element {
  const t = useTranslations("liveView.request");
  const router = useRouter();
  const requestMutation = useRequestLiveView();

  const {
    register,
    handleSubmit,
    setValue,
    watch,
    formState: { errors, isSubmitting },
  } = useForm<RequestFormValues>({
    resolver: zodResolver(requestSchema),
    defaultValues: {
      duration_minutes: 30,
      reason_code: "",
      endpoint_id: "",
    },
  });

  // Load active endpoints for selection
  const { data: endpointsData, isLoading: endpointsLoading } = useQuery({
    queryKey: endpointKeys.list({ status: "active" }),
    queryFn: () => listEndpoints({ status: "active", page_size: 100 }),
    staleTime: 30_000,
  });

  const endpoints = endpointsData?.items ?? [];

  const onSubmit = async (values: RequestFormValues) => {
    try {
      await requestMutation.mutateAsync(values);
      toast.success(t("successToast"));
      // Navigate to the approvals page so user can track status
      router.push("../approvals");
    } catch (err) {
      const ue = toUserFacingError(err);
      toast.error(ue.title, { description: ue.description });
    }
  };

  return (
    <form
      onSubmit={(e) => void handleSubmit(onSubmit)(e)}
      className="space-y-6"
      noValidate
    >
      {/* Dual-control notice */}
      <Alert variant="default" role="note">
        <Info className="h-4 w-4" aria-hidden="true" />
        <AlertDescription className="text-sm">
          {t("dualControlNote")}
        </AlertDescription>
      </Alert>

      {/* Endpoint */}
      <div className="space-y-2">
        <Label htmlFor="endpoint_id">{t("endpointLabel")}</Label>
        {endpointsLoading ? (
          <Skeleton className="h-10 w-full" />
        ) : (
          <Select
            value={watch("endpoint_id")}
            onValueChange={(v) => setValue("endpoint_id", v, { shouldValidate: true })}
          >
            <SelectTrigger id="endpoint_id" aria-describedby={errors.endpoint_id ? "ep-err" : undefined}>
              <SelectValue placeholder={t("endpointPlaceholder")} />
            </SelectTrigger>
            <SelectContent>
              {endpoints.length === 0 ? (
                <SelectItem value="__none" disabled>{t("noEndpoints")}</SelectItem>
              ) : (
                endpoints.map((ep) => (
                  <SelectItem key={ep.id} value={ep.id}>
                    {ep.hostname}
                  </SelectItem>
                ))
              )}
            </SelectContent>
          </Select>
        )}
        {errors.endpoint_id && (
          <p id="ep-err" className="text-xs text-destructive" role="alert">
            {errors.endpoint_id.message}
          </p>
        )}
      </div>

      {/* Reason code */}
      <div className="space-y-2">
        <Label htmlFor="reason_code">{t("reasonLabel")}</Label>
        <Select
          value={watch("reason_code")}
          onValueChange={(v) => setValue("reason_code", v, { shouldValidate: true })}
        >
          <SelectTrigger id="reason_code" aria-describedby={errors.reason_code ? "rc-err" : undefined}>
            <SelectValue placeholder={t("reasonPlaceholder")} />
          </SelectTrigger>
          <SelectContent>
            {REASON_CODES.map((rc) => (
              <SelectItem key={rc.value} value={rc.value}>
                {rc.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {errors.reason_code && (
          <p id="rc-err" className="text-xs text-destructive" role="alert">
            {errors.reason_code.message}
          </p>
        )}
      </div>

      {/* Duration */}
      <div className="space-y-2">
        <Label htmlFor="duration_minutes">{t("durationLabel")}</Label>
        <div className="flex items-center gap-2 flex-wrap">
          {DURATION_OPTIONS.map((d) => (
            <button
              key={d}
              type="button"
              onClick={() => setValue("duration_minutes", d, { shouldValidate: true })}
              className={`rounded-md border px-4 py-2 text-sm transition-colors ${
                watch("duration_minutes") === d
                  ? "border-primary bg-primary text-primary-foreground"
                  : "border-border hover:bg-muted"
              }`}
              aria-pressed={watch("duration_minutes") === d}
            >
              {d} {t("minutes")}
            </button>
          ))}
          {/* Custom duration */}
          <Input
            id="duration_minutes"
            type="number"
            min={1}
            max={60}
            className="w-24"
            {...register("duration_minutes", { valueAsNumber: true })}
            aria-label={t("durationCustom")}
            aria-describedby={errors.duration_minutes ? "dur-err" : undefined}
          />
          <span className="text-sm text-muted-foreground">{t("minutes")}</span>
        </div>
        {errors.duration_minutes && (
          <p id="dur-err" className="text-xs text-destructive" role="alert">
            {errors.duration_minutes.message}
          </p>
        )}
        <p className="text-xs text-muted-foreground">{t("durationNote")}</p>
      </div>

      {/* Submit */}
      <div className="flex items-center gap-3 pt-2">
        <Button
          type="submit"
          disabled={isSubmitting || requestMutation.isPending}
        >
          {isSubmitting || requestMutation.isPending
            ? t("submitting")
            : t("submit")}
        </Button>
        <Button
          type="button"
          variant="outline"
          onClick={() => router.back()}
          disabled={isSubmitting}
        >
          {t("cancel")}
        </Button>
      </div>
    </form>
  );
}
