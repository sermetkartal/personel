"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useLocale, useTranslations } from "next-intl";
import Link from "next/link";
import {
  CheckCircle2,
  Clock,
  Download,
  FileText,
  Info,
  Loader2,
  AlertCircle,
} from "lucide-react";
import { submitDSR, getMyDSR } from "@/lib/api/dsr";
import { ApiError } from "@/lib/api/types";
import type { DSRRequest } from "@/lib/api/types";

interface DataDownloadFlowProps {
  accessToken: string;
}

type FlowState =
  | { kind: "idle" }
  | { kind: "creating" }
  | { kind: "pending"; dsr: DSRRequest }
  | { kind: "ready"; dsr: DSRRequest }
  | { kind: "error"; message: string };

/**
 * Client-side data download flow.
 *
 * Stage 1 — idle: show explainer + "Talep oluştur" button.
 * Stage 2 — creating: POST /v1/me/dsr with type=access + auto justification.
 * Stage 3 — pending: DSR was created, show SLA + poll every 60s for
 *   state transitions. State `closed` with `response_artifact_ref` means
 *   ready for download.
 * Stage 4 — ready: show download link to response_artifact_ref.
 */
export function DataDownloadFlow({
  accessToken,
}: DataDownloadFlowProps): JSX.Element {
  const t = useTranslations("dataDownload");
  const locale = useLocale();
  const [state, setState] = useState<FlowState>({ kind: "idle" });
  const pollTimer = useRef<ReturnType<typeof setInterval> | null>(null);

  const stopPolling = useCallback(() => {
    if (pollTimer.current) {
      clearInterval(pollTimer.current);
      pollTimer.current = null;
    }
  }, []);

  useEffect(() => {
    return stopPolling;
  }, [stopPolling]);

  async function handleCreate(): Promise<void> {
    setState({ kind: "creating" });
    try {
      const result = await submitDSR(
        {
          request_type: "access",
          justification:
            "KVKK m.11/b kapsamında, şeffaflık portalı üzerinden tarafımca yapılan self-service veri döküm talebi.",
        },
        accessToken
      );
      setState({ kind: "pending", dsr: result });
    } catch (err) {
      const msg =
        err instanceof ApiError
          ? err.detail
          : err instanceof Error
            ? err.message
            : t("errorGeneric");
      setState({ kind: "error", message: msg });
    }
  }

  // Poll when in pending state
  useEffect(() => {
    if (state.kind !== "pending") return;

    const dsrId = state.dsr.id;

    const poll = async (): Promise<void> => {
      try {
        const updated = await getMyDSR(dsrId, accessToken);
        if (!updated) return;
        if (updated.state === "closed" && updated.response_artifact_ref) {
          stopPolling();
          setState({ kind: "ready", dsr: updated });
        } else if (updated.state === "rejected") {
          stopPolling();
          setState({
            kind: "error",
            message: t("rejectedMessage"),
          });
        } else {
          setState({ kind: "pending", dsr: updated });
        }
      } catch {
        // Silent retry on next tick; show no user-facing error on a single failed poll
      }
    };

    // Poll immediately + every 60s thereafter
    void poll();
    pollTimer.current = setInterval(() => {
      void poll();
    }, 60_000);

    return () => {
      stopPolling();
    };
  }, [state, accessToken, stopPolling, t]);

  // ── RENDER ──────────────────────────────────────────────────────────────────

  if (state.kind === "idle") {
    return (
      <div className="space-y-5">
        <section className="card" aria-labelledby="idle-heading">
          <div className="flex items-start gap-3 mb-3">
            <Info
              className="w-5 h-5 text-portal-600 mt-0.5 flex-shrink-0"
              aria-hidden="true"
            />
            <div>
              <h2
                id="idle-heading"
                className="text-base font-semibold text-warm-900"
              >
                {t("idleHeading")}
              </h2>
            </div>
          </div>
          <p className="text-sm text-warm-700 leading-relaxed mb-3">
            {t("idleBody")}
          </p>
          <ul className="space-y-2 text-sm text-warm-600 mb-4">
            <li className="flex items-start gap-2">
              <span className="mt-0.5 w-4 h-4 rounded-full bg-trust-100 text-trust-600 flex items-center justify-center text-xs flex-shrink-0" aria-hidden="true">
                ✓
              </span>
              {t("point1")}
            </li>
            <li className="flex items-start gap-2">
              <span className="mt-0.5 w-4 h-4 rounded-full bg-trust-100 text-trust-600 flex items-center justify-center text-xs flex-shrink-0" aria-hidden="true">
                ✓
              </span>
              {t("point2")}
            </li>
            <li className="flex items-start gap-2">
              <span className="mt-0.5 w-4 h-4 rounded-full bg-trust-100 text-trust-600 flex items-center justify-center text-xs flex-shrink-0" aria-hidden="true">
                ✓
              </span>
              {t("point3")}
            </li>
          </ul>
          <button
            type="button"
            onClick={handleCreate}
            className="inline-flex items-center gap-2 bg-portal-600 hover:bg-portal-700 text-white font-medium py-2.5 px-5 rounded-xl text-sm transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-portal-600 focus-visible:ring-offset-2"
          >
            <FileText className="w-4 h-4" aria-hidden="true" />
            {t("idleButton")}
          </button>
        </section>
      </div>
    );
  }

  if (state.kind === "creating") {
    return (
      <div className="card text-center py-12">
        <Loader2 className="w-8 h-8 text-portal-600 animate-spin mx-auto mb-3" aria-hidden="true" />
        <p className="text-sm text-warm-600">{t("creating")}</p>
      </div>
    );
  }

  if (state.kind === "error") {
    return (
      <div
        role="alert"
        className="card border-red-200 bg-red-50"
      >
        <div className="flex items-start gap-3">
          <AlertCircle
            className="w-5 h-5 text-red-600 mt-0.5 flex-shrink-0"
            aria-hidden="true"
          />
          <div className="flex-1">
            <h2 className="text-base font-semibold text-red-800">
              {t("errorHeading")}
            </h2>
            <p className="text-sm text-red-700 mt-1">{state.message}</p>
            <button
              type="button"
              onClick={() => setState({ kind: "idle" })}
              className="mt-3 text-sm text-red-700 underline hover:no-underline"
            >
              {t("retry")}
            </button>
          </div>
        </div>
      </div>
    );
  }

  if (state.kind === "pending") {
    const dsr = state.dsr;
    return (
      <div className="space-y-4">
        <section className="card border-portal-200 bg-portal-50/40">
          <div className="flex items-start gap-3 mb-3">
            <Clock
              className="w-5 h-5 text-portal-600 mt-0.5 flex-shrink-0"
              aria-hidden="true"
            />
            <div>
              <h2 className="text-base font-semibold text-warm-900">
                {t("pendingHeading")}
              </h2>
              <p className="text-xs text-warm-500 mt-1">
                {t("refNumber")}:{" "}
                <code className="font-mono">{dsr.id.slice(0, 8)}</code>
              </p>
            </div>
          </div>
          <p className="text-sm text-warm-700 leading-relaxed mb-3">
            {t("pendingBody")}
          </p>
          <dl className="grid gap-2 text-xs text-warm-600">
            <div className="flex justify-between">
              <dt>{t("state")}</dt>
              <dd className="font-medium text-warm-800">
                {t(`states.${dsr.state}`)}
              </dd>
            </div>
            <div className="flex justify-between">
              <dt>{t("created")}</dt>
              <dd className="font-medium text-warm-800">
                {new Date(dsr.created_at).toLocaleString(locale)}
              </dd>
            </div>
            <div className="flex justify-between">
              <dt>{t("slaDeadline")}</dt>
              <dd className="font-medium text-warm-800">
                {new Date(dsr.sla_deadline).toLocaleDateString(locale)}
              </dd>
            </div>
          </dl>
          <div className="mt-4 flex items-center gap-2 text-xs text-portal-600">
            <Loader2 className="w-3 h-3 animate-spin" aria-hidden="true" />
            {t("polling")}
          </div>
        </section>

        <Link
          href={`/${locale}/basvurularim/${dsr.id}`}
          className="inline-flex items-center gap-2 text-sm text-portal-600 hover:text-portal-800 underline-offset-2 hover:underline"
        >
          {t("viewInApplications")} →
        </Link>
      </div>
    );
  }

  // ready
  const dsr = state.dsr;
  return (
    <div className="card border-trust-200 bg-trust-50/40 text-center py-10">
      <CheckCircle2
        className="w-12 h-12 text-trust-600 mx-auto mb-4"
        aria-hidden="true"
      />
      <h2 className="text-lg font-semibold text-warm-900 mb-2">
        {t("readyHeading")}
      </h2>
      <p className="text-sm text-warm-600 mb-5 max-w-md mx-auto">
        {t("readyBody")}
      </p>
      <Link
        href={`/${locale}/basvurularim/${dsr.id}`}
        className="inline-flex items-center gap-2 bg-portal-600 hover:bg-portal-700 text-white font-medium py-2.5 px-5 rounded-xl text-sm transition-colors"
      >
        <Download className="w-4 h-4" aria-hidden="true" />
        {t("downloadButton")}
      </Link>
    </div>
  );
}
