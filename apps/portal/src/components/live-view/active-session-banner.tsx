"use client";

import { useEffect, useMemo, useState } from "react";
import { useTranslations } from "next-intl";
import { Eye, AlertTriangle } from "lucide-react";
import { getActiveLiveViewSession } from "@/lib/api/live-view";
import type { ActiveLiveViewSession } from "@/lib/api/types";

interface ActiveSessionBannerProps {
  accessToken: string;
}

/**
 * Persistent banner at the top of the canlı-izleme page. Polls
 * /v1/me/live-view-active every 10s. When a session is active, shows
 * requester role, approver role, reason category, and a live countdown
 * to expires_at.
 *
 * Per live-view-protocol.md only role labels are surfaced — never names.
 */
export function ActiveSessionBanner({
  accessToken,
}: ActiveSessionBannerProps): JSX.Element | null {
  const t = useTranslations("canliIzleme.activeBanner");
  const tRoles = useTranslations("oturumGecmisi.roles");
  const tReasons = useTranslations("oturumGecmisi.reasonCategories");

  const [session, setSession] = useState<ActiveLiveViewSession | null>(null);
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    let cancelled = false;

    async function poll(): Promise<void> {
      try {
        const resp = await getActiveLiveViewSession(accessToken);
        if (!cancelled) {
          setSession(resp.active ? resp.session : null);
        }
      } catch {
        if (!cancelled) setSession(null);
      }
    }

    void poll();
    const iv = setInterval(() => {
      void poll();
    }, 10_000);
    return () => {
      cancelled = true;
      clearInterval(iv);
    };
  }, [accessToken]);

  useEffect(() => {
    if (!session) return;
    const iv = setInterval(() => setNow(Date.now()), 1_000);
    return () => clearInterval(iv);
  }, [session]);

  const remaining = useMemo(() => {
    if (!session) return null;
    const expires = new Date(session.expires_at).getTime();
    const ms = Math.max(0, expires - now);
    const mins = Math.floor(ms / 60_000);
    const secs = Math.floor((ms % 60_000) / 1_000);
    return `${mins.toString().padStart(2, "0")}:${secs.toString().padStart(2, "0")}`;
  }, [session, now]);

  if (!session) return null;

  return (
    <div
      role="alert"
      aria-live="polite"
      className="rounded-xl border-2 border-amber-400 bg-amber-50 px-5 py-4 shadow-sm"
    >
      <div className="flex items-start gap-3">
        <div className="relative flex-shrink-0">
          <Eye className="w-6 h-6 text-amber-600" aria-hidden="true" />
          <span className="absolute -top-1 -right-1 w-2.5 h-2.5 rounded-full bg-red-500 animate-pulse" aria-hidden="true" />
        </div>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 mb-1">
            <AlertTriangle className="w-4 h-4 text-amber-700" aria-hidden="true" />
            <h2 className="text-sm font-bold text-amber-900 uppercase tracking-wide">
              {t("title")}
            </h2>
          </div>
          <p className="text-sm text-amber-900 leading-relaxed mb-2">
            {t("description")}
          </p>
          <dl className="grid grid-cols-1 sm:grid-cols-2 gap-x-5 gap-y-1 text-xs text-amber-900">
            <div className="flex gap-2">
              <dt className="font-medium">{t("requester")}:</dt>
              <dd>{tRoles(session.requester_role)}</dd>
            </div>
            <div className="flex gap-2">
              <dt className="font-medium">{t("approver")}:</dt>
              <dd>{tRoles(session.approver_role)}</dd>
            </div>
            <div className="flex gap-2 sm:col-span-2">
              <dt className="font-medium">{t("reason")}:</dt>
              <dd>{tReasons(session.reason_category)}</dd>
            </div>
            <div className="flex gap-2 sm:col-span-2">
              <dt className="font-medium">{t("remaining")}:</dt>
              <dd className="font-mono font-bold">{remaining}</dd>
            </div>
          </dl>
        </div>
      </div>
    </div>
  );
}
