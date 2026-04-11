"use client";

import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useLocale, useTranslations } from "next-intl";
import { useRouter, useSearchParams } from "next/navigation";
import { CheckCircle2 } from "lucide-react";
import Link from "next/link";
import { submitDSR } from "@/lib/api/dsr";
import type { DSRRequestType } from "@/lib/api/types";
import { cn } from "@/lib/utils";

const formSchema = z.object({
  request_type: z.enum([
    "access",
    "rectify",
    "erase",
    "object",
    "restrict",
    "portability",
  ] as const),
  scope: z.string().optional(),
  justification: z.string().optional(),
});

type FormValues = z.infer<typeof formSchema>;

interface NewRequestFormProps {
  accessToken: string;
}

export function NewRequestForm({ accessToken }: NewRequestFormProps): JSX.Element {
  const t = useTranslations("yeniBasvuru");
  const locale = useLocale();
  const router = useRouter();
  const searchParams = useSearchParams();

  const [submittedId, setSubmittedId] = useState<string | null>(null);
  const [serverError, setServerError] = useState<string | null>(null);

  const defaultType = (searchParams.get("type") ?? "") as DSRRequestType | "";

  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<FormValues>({
    resolver: zodResolver(formSchema),
    defaultValues:
      defaultType !== ""
        ? { request_type: defaultType }
        : {},
  });

  async function onSubmit(values: FormValues): Promise<void> {
    setServerError(null);
    try {
      const result = await submitDSR(
        {
          request_type: values.request_type,
          scope: values.scope ?? undefined,
          justification: values.justification ?? undefined,
        },
        accessToken
      );
      setSubmittedId(result.id);
    } catch (err) {
      const message =
        err instanceof Error ? err.message : "Bir hata oluştu. Lütfen tekrar deneyin.";
      setServerError(message);
    }
  }

  // Success state
  if (submittedId) {
    return (
      <div className="card max-w-lg mx-auto text-center py-12">
        <div className="w-14 h-14 rounded-full bg-trust-100 flex items-center justify-center mx-auto mb-4">
          <CheckCircle2 className="w-7 h-7 text-trust-600" aria-hidden="true" />
        </div>
        <h2 className="text-xl font-semibold text-warm-900 mb-2">
          {t("successTitle")}
        </h2>
        <p className="text-sm text-warm-600 leading-relaxed mb-6">
          {t("successText", { id: submittedId.slice(0, 8) })}
        </p>
        <Link
          href={`/${locale}/basvurularim`}
          className="inline-flex items-center gap-2 bg-portal-600 hover:bg-portal-700 text-white font-medium py-2.5 px-5 rounded-xl text-sm transition-colors"
        >
          {t("goToMyApplications")}
        </Link>
      </div>
    );
  }

  const requestTypes: Array<{ value: DSRRequestType; label: string }> = [
    { value: "access", label: t("types.access") },
    { value: "rectify", label: t("types.rectify") },
    { value: "erase", label: t("types.erase") },
    { value: "object", label: t("types.object") },
    { value: "restrict", label: t("types.restrict") },
    { value: "portability", label: t("types.portability") },
  ];

  return (
    <form
      onSubmit={handleSubmit(onSubmit)}
      className="card max-w-lg mx-auto space-y-6"
      noValidate
    >
      {/* Request type */}
      <div>
        <label
          htmlFor="request_type"
          className="block text-sm font-medium text-warm-800 mb-2"
        >
          {t("requestType")}
          <span className="text-red-500 ml-0.5" aria-hidden="true">*</span>
        </label>
        <select
          id="request_type"
          {...register("request_type")}
          className={cn(
            "w-full rounded-xl border border-warm-200 bg-white px-3 py-2.5 text-sm text-warm-900",
            "focus:outline-none focus:ring-2 focus:ring-portal-600 focus:border-portal-600",
            "transition-colors duration-150",
            errors.request_type && "border-red-400 focus:ring-red-400"
          )}
          aria-invalid={!!errors.request_type}
          aria-describedby={errors.request_type ? "request-type-error" : undefined}
        >
          <option value="">{t("requestTypePlaceholder")}</option>
          {requestTypes.map((rt) => (
            <option key={rt.value} value={rt.value}>
              {rt.label}
            </option>
          ))}
        </select>
        {errors.request_type && (
          <p
            id="request-type-error"
            role="alert"
            className="mt-1.5 text-xs text-red-600"
          >
            {t("validation.requestTypeRequired")}
          </p>
        )}
      </div>

      {/* Scope (optional) */}
      <div>
        <label
          htmlFor="scope"
          className="block text-sm font-medium text-warm-800 mb-2"
        >
          {t("scope")}
        </label>
        <input
          id="scope"
          type="text"
          {...register("scope")}
          placeholder={t("scopePlaceholder")}
          className={cn(
            "w-full rounded-xl border border-warm-200 bg-white px-3 py-2.5 text-sm text-warm-900",
            "placeholder:text-warm-400",
            "focus:outline-none focus:ring-2 focus:ring-portal-600 focus:border-portal-600",
            "transition-colors duration-150"
          )}
        />
      </div>

      {/* Justification (optional) */}
      <div>
        <label
          htmlFor="justification"
          className="block text-sm font-medium text-warm-800 mb-2"
        >
          {t("justification")}
        </label>
        <textarea
          id="justification"
          {...register("justification")}
          rows={4}
          placeholder={t("justificationPlaceholder")}
          className={cn(
            "w-full rounded-xl border border-warm-200 bg-white px-3 py-2.5 text-sm text-warm-900",
            "placeholder:text-warm-400 resize-none",
            "focus:outline-none focus:ring-2 focus:ring-portal-600 focus:border-portal-600",
            "transition-colors duration-150"
          )}
        />
      </div>

      {/* SLA note */}
      <div className="rounded-xl bg-portal-50 border border-portal-100 px-4 py-3 text-xs text-portal-700">
        {t("slaNote")}
      </div>

      {/* Server error */}
      {serverError && (
        <div role="alert" className="rounded-xl bg-red-50 border border-red-200 px-4 py-3 text-xs text-red-700">
          {serverError}
        </div>
      )}

      {/* Submit — no double confirmation, no friction */}
      <button
        type="submit"
        disabled={isSubmitting}
        className={cn(
          "w-full bg-portal-600 hover:bg-portal-700 disabled:bg-portal-300",
          "text-white font-medium py-3 px-6 rounded-xl text-sm",
          "transition-colors duration-150",
          "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-portal-600 focus-visible:ring-offset-2"
        )}
        aria-busy={isSubmitting}
      >
        {isSubmitting ? "Gönderiliyor..." : t("submitButton")}
      </button>
    </form>
  );
}
