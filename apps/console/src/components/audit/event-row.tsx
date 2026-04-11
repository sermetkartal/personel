"use client";

import { useState } from "react";
import { useTranslations } from "next-intl";
import { ChevronDown, ChevronRight, Hash } from "lucide-react";
import type { AuditRecord } from "@/lib/api/types";
import { formatRelativeTR, formatDateTR, snakeToTitle } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";

interface EventRowProps {
  record: AuditRecord;
  isExpanded?: boolean;
}

// Map audit action types to badge color
function getActionVariant(actionType: string): "default" | "warning" | "destructive" | "info" | "success" {
  if (actionType.includes("login") || actionType.includes("logout")) return "default";
  if (actionType.includes("live_view")) return "info";
  if (actionType.includes("dsr") || actionType.includes("legal_hold")) return "warning";
  if (actionType.includes("deleted") || actionType.includes("revoked") || actionType.includes("terminated")) return "destructive";
  if (actionType.includes("approved") || actionType.includes("created")) return "success";
  return "default";
}

export function EventRow({ record }: EventRowProps): JSX.Element {
  const [expanded, setExpanded] = useState(false);
  const t = useTranslations("audit.event");

  return (
    <div className="border-b last:border-0">
      {/* Main row */}
      <button
        className={cn(
          "flex w-full items-center gap-3 px-4 py-3 text-left text-sm transition-colors hover:bg-muted/30",
          expanded && "bg-muted/20",
        )}
        onClick={() => setExpanded(!expanded)}
        aria-expanded={expanded}
        aria-label={`Denetim kaydı: ${record.type}. ${expanded ? "Kapat" : "Genişlet"}`}
      >
        {/* Toggle icon */}
        <span className="shrink-0 text-muted-foreground" aria-hidden="true">
          {expanded ? (
            <ChevronDown className="h-4 w-4" />
          ) : (
            <ChevronRight className="h-4 w-4" />
          )}
        </span>

        {/* Sequence number */}
        <span className="font-hash w-12 shrink-0 text-xs text-muted-foreground/60">
          #{record.seq}
        </span>

        {/* Action type badge */}
        <Badge variant={getActionVariant(record.type)} className="shrink-0 font-mono text-xs">
          {record.type}
        </Badge>

        {/* Actor */}
        <span className="min-w-0 flex-1 truncate text-muted-foreground">
          {record.actor_id
            ? record.actor_id.slice(0, 8) + "..."
            : "system"}
        </span>

        {/* Timestamp */}
        <time
          dateTime={record.created_at}
          className="shrink-0 text-xs text-muted-foreground"
          title={formatDateTR(record.created_at)}
        >
          {formatRelativeTR(record.created_at)}
        </time>
      </button>

      {/* Expanded detail */}
      {expanded && (
        <div className="border-t border-border/50 bg-muted/10 px-4 py-4 text-sm space-y-4">
          {/* Hash chain */}
          <div className="space-y-2">
            <h4 className="text-xs font-semibold uppercase text-muted-foreground tracking-wider">
              Hash Zinciri
            </h4>
            <div className="space-y-1.5">
              <div className="flex items-start gap-2">
                <Hash className="mt-0.5 h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
                <div className="min-w-0">
                  <p className="text-xs text-muted-foreground">{t("prevHash")}</p>
                  <code className="font-hash text-xs break-all text-muted-foreground/80">
                    {record.prev_hash}
                  </code>
                </div>
              </div>
              <div className="flex items-start gap-2">
                <Hash className="mt-0.5 h-3.5 w-3.5 shrink-0 text-foreground" aria-hidden="true" />
                <div className="min-w-0">
                  <p className="text-xs text-muted-foreground">{t("hash")}</p>
                  <code className="font-hash text-xs break-all font-semibold">
                    {record.this_hash}
                  </code>
                </div>
              </div>
            </div>
          </div>

          {/* Subject */}
          {record.subject_id && (
            <div>
              <p className="text-xs text-muted-foreground mb-1">{t("subject")}</p>
              <code className="font-hash text-xs">{record.subject_id}</code>
            </div>
          )}

          {/* Payload */}
          {Object.keys(record.payload_json).length > 0 && (
            <div>
              <h4 className="text-xs font-semibold uppercase text-muted-foreground tracking-wider mb-2">
                Yük Verisi
              </h4>
              <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-xs">
                {Object.entries(record.payload_json).map(([key, value]) => (
                  <div key={key}>
                    <dt className="text-muted-foreground">{snakeToTitle(key)}</dt>
                    <dd className="font-medium truncate">
                      {typeof value === "object"
                        ? JSON.stringify(value)
                        : String(value)}
                    </dd>
                  </div>
                ))}
              </dl>
            </div>
          )}

          {/* Full timestamp */}
          <div>
            <p className="text-xs text-muted-foreground">{t("timestamp")}</p>
            <time dateTime={record.created_at} className="text-xs">
              {formatDateTR(record.created_at)}
            </time>
          </div>
        </div>
      )}
    </div>
  );
}
