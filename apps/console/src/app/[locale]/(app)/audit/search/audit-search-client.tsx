"use client";

import { useMemo, useState, useEffect } from "react";
import { useQuery, keepPreviousData } from "@tanstack/react-query";
import { useTranslations } from "next-intl";
import {
  Search,
  X,
  RefreshCw,
  Download,
  ChevronDown,
  ChevronRight,
  AlertTriangle,
  FileText,
} from "lucide-react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { searchAudit, auditKeys } from "@/lib/api/audit";
import type { AuditSearchQuery, AuditHit } from "@/lib/api/audit";
import { formatDateTR, formatRelativeTR, cn } from "@/lib/utils";

// Quick date range presets — ISO timestamps computed at render time.
type DateRangeKey = "24h" | "7d" | "30d" | "custom";

function computeRange(key: DateRangeKey): { from?: string; to?: string } {
  const now = new Date();
  const to = now.toISOString();
  if (key === "24h") {
    return { from: new Date(now.getTime() - 24 * 3600 * 1000).toISOString(), to };
  }
  if (key === "7d") {
    return { from: new Date(now.getTime() - 7 * 24 * 3600 * 1000).toISOString(), to };
  }
  if (key === "30d") {
    return { from: new Date(now.getTime() - 30 * 24 * 3600 * 1000).toISOString(), to };
  }
  return {};
}

// Action filter groups — match the existing audit filter bar conventions.
const ACTION_GROUPS = [
  { value: "", key: "allActions" },
  { value: "login", key: "login" },
  { value: "live_view", key: "liveView" },
  { value: "dsr", key: "dsr" },
  { value: "legal_hold", key: "legalHold" },
  { value: "policy", key: "policy" },
  { value: "endpoint", key: "endpoint" },
  { value: "user", key: "user" },
  { value: "dlp", key: "dlp" },
  { value: "screenshot", key: "screenshot" },
] as const;

interface AuditSearchClientProps {
  token?: string;
}

// Strip defensively-sensitive payload keys on the client — defense in depth
// even though the server already scrubs.
const SENSITIVE_KEYS = new Set([
  "content",
  "keystroke_content",
  "body",
  "raw_keystrokes",
  "password",
  "secret",
  "token",
  "api_key",
]);

function sanitize(payload: unknown): unknown {
  if (payload === null || typeof payload !== "object") return payload;
  if (Array.isArray(payload)) return payload.map(sanitize);
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(payload as Record<string, unknown>)) {
    if (SENSITIVE_KEYS.has(k.toLowerCase())) {
      out[k] = "[redacted]";
    } else {
      out[k] = sanitize(v);
    }
  }
  return out;
}

function useDebounced<T>(value: T, delay: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setDebounced(value), delay);
    return () => clearTimeout(t);
  }, [value, delay]);
  return debounced;
}

function exportCsv(hits: AuditHit[]): void {
  const header = ["timestamp", "action", "actor_id", "actor_username", "target", "payload"];
  const rows = hits.map((h) => [
    h.timestamp,
    h.action,
    h.actor_id,
    h.actor_username ?? "",
    h.target,
    JSON.stringify(sanitize(h.payload)),
  ]);
  const escape = (v: string): string => {
    if (/[",\n]/.test(v)) return `"${v.replace(/"/g, '""')}"`;
    return v;
  };
  const csv = [header, ...rows].map((r) => r.map(escape).join(",")).join("\n");
  const blob = new Blob([csv], { type: "text/csv;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `audit-search-${new Date().toISOString().slice(0, 10)}.csv`;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}

export function AuditSearchClient({ token }: AuditSearchClientProps): JSX.Element {
  const t = useTranslations("audit.search");

  // Filter state — local, not URL-synced (search is ephemeral).
  const [qInput, setQInput] = useState("");
  const q = useDebounced(qInput, 500);
  const [action, setAction] = useState<string>("");
  const [actorId, setActorId] = useState<string>("");
  const [rangeKey, setRangeKey] = useState<DateRangeKey>("7d");
  const [customFrom, setCustomFrom] = useState<string>("");
  const [customTo, setCustomTo] = useState<string>("");
  const [page, setPage] = useState(1);
  const [expandedId, setExpandedId] = useState<string | null>(null);

  const range = useMemo(() => {
    if (rangeKey === "custom") {
      return {
        from: customFrom ? new Date(customFrom).toISOString() : undefined,
        to: customTo ? new Date(customTo).toISOString() : undefined,
      };
    }
    return computeRange(rangeKey);
  }, [rangeKey, customFrom, customTo]);

  const query: AuditSearchQuery = useMemo(
    () => ({
      q: q || undefined,
      action: action || undefined,
      actor_id: actorId || undefined,
      from: range.from,
      to: range.to,
      page,
      page_size: 25,
    }),
    [q, action, actorId, range.from, range.to, page],
  );

  // Reset page on filter changes
  useEffect(() => {
    setPage(1);
  }, [q, action, actorId, rangeKey, customFrom, customTo]);

  const { data, isLoading, isFetching, isError, refetch } = useQuery({
    queryKey: auditKeys.search(query),
    queryFn: () => searchAudit(query, token ? { token } : {}),
    placeholderData: keepPreviousData,
    staleTime: 10_000,
  });

  const hits = data?.hits ?? [];
  const total = data?.total ?? 0;
  const isDegraded = data?.degraded ?? false;
  const totalPages = Math.max(1, Math.ceil(total / 25));
  const hasFilters = q || action || actorId || rangeKey !== "7d";

  const clearFilters = (): void => {
    setQInput("");
    setAction("");
    setActorId("");
    setRangeKey("7d");
    setCustomFrom("");
    setCustomTo("");
    setPage(1);
  };

  return (
    <div className="space-y-4">
      {/* Degraded banner — OpenSearch offline, fallback served */}
      {isDegraded && (
        <Alert variant="warning" role="status">
          <AlertTriangle className="h-4 w-4" aria-hidden="true" />
          <AlertTitle>{t("degradedTitle")}</AlertTitle>
          <AlertDescription>{t("offline")}</AlertDescription>
        </Alert>
      )}

      {/* Search bar */}
      <div className="relative">
        <Search
          className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground"
          aria-hidden="true"
        />
        <Input
          value={qInput}
          onChange={(e) => setQInput(e.target.value)}
          placeholder={t("placeholder")}
          className="pl-9"
          aria-label={t("placeholder")}
        />
      </div>

      {/* Filter row */}
      <div className="flex flex-wrap items-center gap-3">
        <Select value={action} onValueChange={setAction}>
          <SelectTrigger className="w-44" aria-label={t("filters.action")}>
            <SelectValue placeholder={t("filters.action")} />
          </SelectTrigger>
          <SelectContent>
            {ACTION_GROUPS.map((g) => (
              <SelectItem key={g.key} value={g.value}>
                {t(`filters.${g.key}` as Parameters<typeof t>[0])}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <div className="relative">
          <Input
            className="w-52"
            placeholder={t("filters.actor")}
            value={actorId}
            onChange={(e) => setActorId(e.target.value)}
            aria-label={t("filters.actor")}
          />
        </div>

        <Select
          value={rangeKey}
          onValueChange={(v) => setRangeKey(v as DateRangeKey)}
        >
          <SelectTrigger className="w-40" aria-label={t("filters.dateRange")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="24h">{t("filters.last24h")}</SelectItem>
            <SelectItem value="7d">{t("filters.last7d")}</SelectItem>
            <SelectItem value="30d">{t("filters.last30d")}</SelectItem>
            <SelectItem value="custom">{t("filters.custom")}</SelectItem>
          </SelectContent>
        </Select>

        {rangeKey === "custom" && (
          <div className="flex items-center gap-1.5">
            <Input
              type="datetime-local"
              className="w-48 text-xs"
              value={customFrom}
              onChange={(e) => setCustomFrom(e.target.value)}
              aria-label={t("filters.customFrom")}
            />
            <span className="text-xs text-muted-foreground" aria-hidden="true">—</span>
            <Input
              type="datetime-local"
              className="w-48 text-xs"
              value={customTo}
              onChange={(e) => setCustomTo(e.target.value)}
              aria-label={t("filters.customTo")}
            />
          </div>
        )}

        {hasFilters && (
          <Button
            variant="ghost"
            size="sm"
            onClick={clearFilters}
            className="h-9 px-2 text-xs text-muted-foreground hover:text-foreground"
          >
            <X className="mr-1 h-3.5 w-3.5" aria-hidden="true" />
            {t("clearFilters")}
          </Button>
        )}

        <div className="ml-auto flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => exportCsv(hits)}
            disabled={hits.length === 0}
          >
            <Download className="mr-1 h-4 w-4" aria-hidden="true" />
            {t("exportCsv")}
          </Button>
          <Button
            variant="ghost"
            size="icon"
            className="h-9 w-9"
            onClick={() => void refetch()}
            disabled={isFetching}
            aria-label={t("refresh")}
          >
            <RefreshCw
              className={cn("h-4 w-4", isFetching && "animate-spin")}
              aria-hidden="true"
            />
          </Button>
        </div>
      </div>

      {/* Result count */}
      {!isLoading && data && (
        <p className="text-xs text-muted-foreground">
          {t("totalHits", { total: total.toLocaleString("tr-TR"), ms: data.took_ms })}
        </p>
      )}

      {/* Results table */}
      <div
        className="rounded-lg border bg-card"
        role="list"
        aria-label={t("resultsLabel")}
        aria-busy={isLoading}
      >
        {isLoading ? (
          <div className="divide-y">
            {Array.from({ length: 10 }).map((_, i) => (
              <div key={i} className="flex items-center gap-3 px-4 py-3">
                <Skeleton className="h-4 w-4 rounded" />
                <Skeleton className="h-5 w-28 rounded-full" />
                <Skeleton className="h-4 flex-1" />
                <Skeleton className="h-4 w-24" />
              </div>
            ))}
          </div>
        ) : isError ? (
          <div className="flex flex-col items-center justify-center py-12 text-center">
            <FileText className="mb-3 h-8 w-8 text-muted-foreground/50" aria-hidden="true" />
            <p className="font-medium">{t("error")}</p>
            <Button variant="ghost" size="sm" className="mt-2" onClick={() => void refetch()}>
              {t("retry")}
            </Button>
          </div>
        ) : hits.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-12 text-center">
            <FileText className="mb-3 h-8 w-8 text-muted-foreground/50" aria-hidden="true" />
            <p className="font-medium">{t("noResults")}</p>
            <p className="mt-1 text-sm text-muted-foreground">{t("noResultsHint")}</p>
          </div>
        ) : (
          <div role="list">
            {hits.map((hit) => (
              <ResultRow
                key={hit.id}
                hit={hit}
                expanded={expandedId === hit.id}
                onToggle={() =>
                  setExpandedId((prev) => (prev === hit.id ? null : hit.id))
                }
              />
            ))}
          </div>
        )}
      </div>

      {/* Pagination */}
      {total > 25 && (
        <div className="flex items-center justify-between gap-4">
          <Button
            variant="outline"
            size="sm"
            disabled={page <= 1}
            onClick={() => setPage((p) => Math.max(1, p - 1))}
          >
            {t("prev")}
          </Button>
          <span className="text-sm text-muted-foreground">
            {t("pageOf", { page, total: totalPages })}
          </span>
          <Button
            variant="outline"
            size="sm"
            disabled={page >= totalPages}
            onClick={() => setPage((p) => p + 1)}
          >
            {t("next")}
          </Button>
        </div>
      )}
    </div>
  );
}

// ── Row ───────────────────────────────────────────────────────────────────────

interface ResultRowProps {
  hit: AuditHit;
  expanded: boolean;
  onToggle: () => void;
}

function getActionVariant(
  action: string,
): "default" | "warning" | "destructive" | "info" | "success" {
  if (action.includes("login") || action.includes("logout")) return "default";
  if (action.includes("live_view")) return "info";
  if (action.includes("dsr") || action.includes("legal_hold")) return "warning";
  if (
    action.includes("deleted") ||
    action.includes("revoked") ||
    action.includes("terminated")
  )
    return "destructive";
  if (action.includes("approved") || action.includes("created")) return "success";
  return "default";
}

function ResultRow({ hit, expanded, onToggle }: ResultRowProps): JSX.Element {
  const sanitizedPayload = useMemo(() => sanitize(hit.payload), [hit.payload]);
  return (
    <div className="border-b last:border-0">
      <button
        type="button"
        onClick={onToggle}
        className={cn(
          "flex w-full items-center gap-3 px-4 py-3 text-left text-sm transition-colors hover:bg-muted/30",
          expanded && "bg-muted/20",
        )}
        aria-expanded={expanded}
      >
        <span className="shrink-0 text-muted-foreground" aria-hidden="true">
          {expanded ? (
            <ChevronDown className="h-4 w-4" />
          ) : (
            <ChevronRight className="h-4 w-4" />
          )}
        </span>
        <Badge
          variant={getActionVariant(hit.action)}
          className="shrink-0 font-mono text-xs"
        >
          {hit.action}
        </Badge>
        <span className="min-w-0 flex-1 truncate text-muted-foreground">
          {hit.actor_username ??
            (hit.actor_id ? hit.actor_id.slice(0, 8) + "..." : "system")}
          {hit.target ? (
            <>
              {" → "}
              <span className="font-mono">{hit.target.slice(0, 16)}</span>
            </>
          ) : null}
        </span>
        <time
          dateTime={hit.timestamp}
          className="shrink-0 text-xs text-muted-foreground"
          title={formatDateTR(hit.timestamp)}
        >
          {formatRelativeTR(hit.timestamp)}
        </time>
      </button>
      {expanded && (
        <div className="border-t border-border/50 bg-muted/10 px-4 py-4 text-xs space-y-2">
          <div>
            <span className="text-muted-foreground">ID:</span>{" "}
            <code className="font-mono">{hit.id}</code>
          </div>
          {hit.actor_id && (
            <div>
              <span className="text-muted-foreground">Actor:</span>{" "}
              <code className="font-mono">{hit.actor_id}</code>
            </div>
          )}
          {hit.target && (
            <div>
              <span className="text-muted-foreground">Target:</span>{" "}
              <code className="font-mono">{hit.target}</code>
            </div>
          )}
          <div>
            <p className="text-muted-foreground mb-1">Payload:</p>
            <pre className="overflow-x-auto rounded bg-background/50 p-2 font-mono text-[11px]">
              {JSON.stringify(sanitizedPayload, null, 2)}
            </pre>
          </div>
        </div>
      )}
    </div>
  );
}
