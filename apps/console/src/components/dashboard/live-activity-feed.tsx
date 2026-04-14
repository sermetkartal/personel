"use client";

import { useState } from "react";
import { useTranslations } from "next-intl";
import { Activity, Clock, WifiOff, Radio } from "lucide-react";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useAuditStream, type StreamFilter } from "@/lib/hooks/use-audit-stream";
import { formatRelativeTR, formatDateTR, cn } from "@/lib/utils";

interface LiveActivityFeedProps {
  /** Bearer token for WebSocket auth (access_token from session). */
  token?: string;
  /** Initial filter — defaults to high-value categories. */
  initialFilter?: StreamFilter;
  /** Max entries to show in the scrollable list. */
  maxVisible?: number;
}

const DEFAULT_FILTER: StreamFilter = {
  actions: ["live_view", "dsr", "policy", "endpoint", "legal_hold"],
};

// Filter presets the user can switch between
const FILTER_PRESETS = [
  { key: "all", filter: {} },
  { key: "filterAll", filter: DEFAULT_FILTER },
  { key: "filterLiveView", filter: { actions: ["live_view"] } },
  { key: "filterDSR", filter: { actions: ["dsr"] } },
  { key: "filterPolicy", filter: { actions: ["policy", "dlp"] } },
] as const;

type PresetKey = (typeof FILTER_PRESETS)[number]["key"];

function getActionVariant(
  type: string,
): "default" | "warning" | "destructive" | "info" | "success" {
  if (type.includes("login") || type.includes("logout")) return "default";
  if (type.includes("live_view")) return "info";
  if (type.includes("dsr") || type.includes("legal_hold")) return "warning";
  if (
    type.includes("deleted") ||
    type.includes("revoked") ||
    type.includes("terminated") ||
    type.includes("denied")
  )
    return "destructive";
  if (type.includes("approved") || type.includes("created")) return "success";
  return "default";
}

export function LiveActivityFeed({
  token,
  initialFilter = DEFAULT_FILTER,
  maxVisible = 30,
}: LiveActivityFeedProps): JSX.Element {
  const t = useTranslations("dashboard.live");
  const [presetKey, setPresetKey] = useState<PresetKey>("filterAll");

  const activeFilter =
    FILTER_PRESETS.find((p) => p.key === presetKey)?.filter ?? initialFilter;

  const { entries, state, error } = useAuditStream(activeFilter, {
    token,
    bufferSize: 200,
  });

  // Connection indicator: green/yellow/red dot
  const dotClass =
    state === "connected"
      ? "bg-green-500"
      : state === "polling-fallback"
        ? "bg-blue-500"
        : state === "reconnecting" || state === "connecting"
          ? "bg-amber-500 animate-pulse"
          : "bg-red-500";

  const statusLabel =
    state === "connected"
      ? t("connected")
      : state === "polling-fallback"
        ? t("pollingFallback")
        : state === "reconnecting"
          ? t("reconnecting")
          : state === "connecting"
            ? t("connecting")
            : t("disconnected");

  const visible = entries.slice(0, maxVisible);

  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between gap-4">
        <div className="min-w-0 flex-1">
          <CardTitle className="flex items-center gap-2 text-base">
            <Radio className="h-4 w-4 text-muted-foreground" aria-hidden="true" />
            {t("title")}
          </CardTitle>
          <CardDescription>{t("description")}</CardDescription>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          {/* Filter preset selector */}
          <Select
            value={presetKey}
            onValueChange={(v) => setPresetKey(v as PresetKey)}
          >
            <SelectTrigger className="h-8 w-44 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {FILTER_PRESETS.map((p) => (
                <SelectItem key={p.key} value={p.key}>
                  {t(p.key as Parameters<typeof t>[0])}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>

          {/* Connection dot */}
          <div
            className="flex items-center gap-1.5 rounded-full border bg-background px-2 py-1 text-xs"
            title={statusLabel}
            aria-live="polite"
          >
            <span
              className={cn("h-2 w-2 rounded-full", dotClass)}
              aria-hidden="true"
            />
            <span className="text-muted-foreground">{statusLabel}</span>
          </div>
        </div>
      </CardHeader>
      <CardContent>
        {error && state === "disconnected" && (
          <div className="mb-3 flex items-center gap-2 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700">
            <WifiOff className="h-3.5 w-3.5" aria-hidden="true" />
            {error}
          </div>
        )}

        {visible.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-8 text-center">
            <Activity
              className="mb-2 h-6 w-6 text-muted-foreground/50"
              aria-hidden="true"
            />
            <p className="text-sm text-muted-foreground">
              {state === "connected"
                ? t("waitingEvents")
                : t("noEventsYet")}
            </p>
          </div>
        ) : (
          <div
            className="max-h-96 space-y-1.5 overflow-y-auto scrollbar-thin pr-1"
            role="list"
            aria-label={t("title")}
          >
            {visible.map((record) => (
              <div
                key={record.id}
                role="listitem"
                className="flex items-center gap-3 rounded-md border border-border/50 px-3 py-2 text-xs animate-fade-in"
              >
                <Badge
                  variant={getActionVariant(record.type)}
                  className="shrink-0 font-mono text-[10px]"
                >
                  {record.type}
                </Badge>
                <span className="min-w-0 flex-1 truncate text-muted-foreground">
                  {record.actor_id
                    ? record.actor_id.slice(0, 8) + "..."
                    : "system"}
                  {record.subject_id ? (
                    <>
                      {" → "}
                      <span className="font-mono">
                        {record.subject_id.slice(0, 12)}
                      </span>
                    </>
                  ) : null}
                </span>
                <div className="flex items-center gap-1 text-[10px] text-muted-foreground shrink-0">
                  <Clock className="h-3 w-3" aria-hidden="true" />
                  <time
                    dateTime={record.created_at}
                    title={formatDateTR(record.created_at)}
                  >
                    {formatRelativeTR(record.created_at)}
                  </time>
                </div>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
