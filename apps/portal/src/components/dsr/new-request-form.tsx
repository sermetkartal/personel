"use client";

import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useLocale, useTranslations } from "next-intl";
import { useSearchParams } from "next/navigation";
import { CheckCircle2, Paperclip, X } from "lucide-react";
import Link from "next/link";
import { submitDSR } from "@/lib/api/dsr";
import type { DSRRequestType } from "@/lib/api/types";
import { cn } from "@/lib/utils";

const MAX_ATTACHMENT_BYTES = 5 * 1024 * 1024;
const MIN_JUSTIFICATION_CHARS = 50;

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
  justification: z
    .string()
    .min(
      MIN_JUSTIFICATION_CHARS,
      `Başvurunuzu en az ${MIN_JUSTIFICATION_CHARS} karakter ile açıklayın.`
    ),
});

type FormValues = z.infer<typeof formSchema>;

interface NewRequestFormProps {
  accessToken: string;
}

export function NewRequestForm({ accessToken }: NewRequestFormProps): JSX.Element {
  const t = useTranslations("yeniBasvuru");
  const locale = useLocale();
  const searchParams = useSearchParams();

  const [submittedId, setSubmittedId] = useState<string | null>(null);
  const [serverError, setServerError] = useState<string | null>(null);
  const [attachment, setAttachment] = useState<File | null>(null);
  const [attachmentError, setAttachmentError] = useState<string | null>(null);

  const defaultType = (searchParams.get("type") ?? "") as DSRRequestType | "";

  const {
    register,
    handleSubmit,
    watch,
    formState: { errors, isSubmitting },
  } = useForm<FormValues>({
    resolver: zodResolver(formSchema),
    defaultValues:
      defaultType !== ""
        ? { request_type: defaultType }
        : {},
  });

  const justificationValue = watch("justification") ?? "";
  const justificationLen = justificationValue.length;

  function handleAttachmentChange(e: React.ChangeEvent<HTMLInputElement>): void {
    setAttachmentError(null);
    const file = e.target.files?.[0] ?? null;
    if (!file) {
      setAttachment(null);
      return;
    }
    if (file.size > MAX_ATTACHMENT_BYTES) {
      setAttachmentError(t("validation.attachmentTooLarge"));
      setAttachment(null);
      e.target.value = "";
      return;
    }
    setAttachment(file);
  }

  function clearAttachment(): void {
    setAttachment(null);
    setAttachmentError(null);
    const input = document.getElementById("attachment") as HTMLInputElement | null;
    if (input) input.value = "";
  }

  async function onSubmit(values: FormValues): Promise<void> {
    setServerError(null);
    try {
      // NOTE: attachment upload is scaffold — the backend DSR endpoint does
      // not yet accept multipart. For now we include the filename in the
      // justification so the DPO can ask for the file through a side channel.
      const justification = attachment
        ? `${values.justification}\n\n[Ek: ${attachment.name} — ${Math.round(attachment.size / 1024)} KB]`
        : values.justification;
      const result = await submitDSR(
        {
          request_type: values.request_type,
          scope: values.scope ?? undefined,
          justification,
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
    const refShort = submittedId.slice(0, 8);
    return (
      <div className="card max-w-lg mx-auto text-center py-12">
        <div className="w-14 h-14 rounded-full bg-trust-100 flex items-center justify-center mx-auto mb-4">
          <CheckCircle2 className="w-7 h-7 text-trust-600" aria-hidden="true" />
        </div>
        <h2 className="text-xl font-semibold text-warm-900 mb-2">
          {t("successTitle")}
        </h2>
        <p className="text-sm text-warm-600 leading-relaxed mb-4">
          {t("successText", { id: refShort })}
        </p>
        <div className="mb-4 inline-flex flex-col items-center rounded-xl bg-warm-50 border border-warm-200 px-5 py-3">
          <span className="text-xs uppercase tracking-wide text-warm-400">
            {t("refNumberLabel")}
          </span>
          <code className="mt-1 text-base font-mono font-semibold text-warm-900">
            {refShort}
          </code>
        </div>
        <p className="text-xs text-portal-700 bg-portal-50 border border-portal-100 rounded-xl px-4 py-2 max-w-sm mx-auto mb-6">
          {t("slaNote")}
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

      {/* Justification (required, min 50 chars) */}
      <div>
        <label
          htmlFor="justification"
          className="block text-sm font-medium text-warm-800 mb-2"
        >
          {t("justification")}
          <span className="text-red-500 ml-0.5" aria-hidden="true">*</span>
        </label>
        <textarea
          id="justification"
          {...register("justification")}
          rows={5}
          placeholder={t("justificationPlaceholder")}
          aria-invalid={!!errors.justification}
          aria-describedby="justification-counter justification-error"
          className={cn(
            "w-full rounded-xl border border-warm-200 bg-white px-3 py-2.5 text-sm text-warm-900",
            "placeholder:text-warm-400 resize-none",
            "focus:outline-none focus:ring-2 focus:ring-portal-600 focus:border-portal-600",
            "transition-colors duration-150",
            errors.justification && "border-red-400 focus:ring-red-400"
          )}
        />
        <div className="mt-1.5 flex items-center justify-between">
          <span
            id="justification-counter"
            className={cn(
              "text-xs",
              justificationLen < MIN_JUSTIFICATION_CHARS
                ? "text-warm-400"
                : "text-trust-600"
            )}
          >
            {justificationLen}/{MIN_JUSTIFICATION_CHARS}
            {" "}
            {t("charsMin")}
          </span>
          {errors.justification && (
            <span
              id="justification-error"
              role="alert"
              className="text-xs text-red-600"
            >
              {errors.justification.message ?? t("validation.justificationRequired")}
            </span>
          )}
        </div>
      </div>

      {/* Attachment (optional, <=5MB) */}
      <div>
        <label
          htmlFor="attachment"
          className="block text-sm font-medium text-warm-800 mb-2"
        >
          {t("attachment")}
          <span className="ml-1 text-xs text-warm-400">{t("attachmentOptional")}</span>
        </label>
        {attachment ? (
          <div className="flex items-center justify-between gap-3 rounded-xl border border-warm-200 bg-warm-50 px-3 py-2.5 text-sm">
            <div className="flex items-center gap-2 min-w-0">
              <Paperclip className="w-4 h-4 text-warm-500 flex-shrink-0" aria-hidden="true" />
              <span className="truncate text-warm-800">{attachment.name}</span>
              <span className="text-xs text-warm-400 flex-shrink-0">
                {Math.round(attachment.size / 1024)} KB
              </span>
            </div>
            <button
              type="button"
              onClick={clearAttachment}
              className="text-warm-400 hover:text-red-600 flex-shrink-0"
              aria-label={t("attachmentRemove")}
            >
              <X className="w-4 h-4" aria-hidden="true" />
            </button>
          </div>
        ) : (
          <input
            id="attachment"
            type="file"
            onChange={handleAttachmentChange}
            className="block w-full text-sm text-warm-600 file:mr-3 file:py-2 file:px-4 file:rounded-xl file:border-0 file:text-sm file:font-medium file:bg-portal-50 file:text-portal-700 hover:file:bg-portal-100 cursor-pointer"
          />
        )}
        <p className="mt-1 text-xs text-warm-400">{t("attachmentHint")}</p>
        {attachmentError && (
          <p role="alert" className="mt-1 text-xs text-red-600">
            {attachmentError}
          </p>
        )}
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
