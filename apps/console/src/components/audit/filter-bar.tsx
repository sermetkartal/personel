"use client";

import { useCallback, useTransition } from "react";
import { useRouter, useSearchParams, usePathname } from "next/navigation";
import { useTranslations } from "next-intl";
import { Search, X, RefreshCw } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { cn } from "@/lib/utils";

// Common audit action type groups for quick filtering
const ACTION_GROUPS = [
  { value: "", label: "Tüm Olaylar" },
  { value: "login", label: "Giriş / Çıkış" },
  { value: "live_view", label: "Canlı İzleme" },
  { value: "dsr", label: "DSR" },
  { value: "legal_hold", label: "Yasal Durdurma" },
  { value: "policy", label: "Politika" },
  { value: "endpoint", label: "Uç Nokta" },
  { value: "user", label: "Kullanıcı" },
  { value: "dlp", label: "DLP" },
  { value: "screenshot", label: "Ekran Görüntüsü" },
] as const;

export interface AuditFilters {
  action?: string;
  actor_id?: string;
  from?: string;
  to?: string;
}

interface FilterBarProps {
  className?: string;
  isRefetching?: boolean;
  onRefetch?: () => void;
}

export function AuditFilterBar({
  className,
  isRefetching,
  onRefetch,
}: FilterBarProps): JSX.Element {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const t = useTranslations("audit");
  const [isPending, startTransition] = useTransition();

  const action = searchParams.get("action") ?? "";
  const actorId = searchParams.get("actor_id") ?? "";
  const from = searchParams.get("from") ?? "";
  const to = searchParams.get("to") ?? "";

  const hasFilters = action || actorId || from || to;

  const updateParam = useCallback(
    (key: string, value: string) => {
      const params = new URLSearchParams(searchParams.toString());
      if (value) {
        params.set(key, value);
      } else {
        params.delete(key);
      }
      // Reset pagination on filter change
      params.delete("page");
      startTransition(() => {
        router.replace(`${pathname}?${params.toString()}`);
      });
    },
    [router, pathname, searchParams],
  );

  const clearAll = useCallback(() => {
    startTransition(() => {
      router.replace(pathname);
    });
  }, [router, pathname]);

  const isLoading = isPending || isRefetching;

  return (
    <div
      className={cn("flex flex-wrap items-center gap-3", className)}
      role="search"
      aria-label={t("filterLabel")}
    >
      {/* Action type filter */}
      <Select
        value={action}
        onValueChange={(v) => updateParam("action", v)}
      >
        <SelectTrigger className="w-44" aria-label={t("filterAction")}>
          <SelectValue placeholder={t("filterAction")} />
        </SelectTrigger>
        <SelectContent>
          {ACTION_GROUPS.map((group) => (
            <SelectItem key={group.value} value={group.value}>
              {group.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      {/* Actor search */}
      <div className="relative">
        <Search
          className="absolute left-2.5 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground"
          aria-hidden="true"
        />
        <Input
          className="pl-8 w-52"
          placeholder={t("filterActor")}
          value={actorId}
          onChange={(e) => updateParam("actor_id", e.target.value)}
          aria-label={t("filterActor")}
        />
      </div>

      {/* Date range */}
      <div className="flex items-center gap-1.5">
        <Input
          type="date"
          className="w-36 text-xs"
          value={from}
          onChange={(e) => updateParam("from", e.target.value)}
          aria-label={t("filterFrom")}
        />
        <span className="text-xs text-muted-foreground" aria-hidden="true">—</span>
        <Input
          type="date"
          className="w-36 text-xs"
          value={to}
          onChange={(e) => updateParam("to", e.target.value)}
          aria-label={t("filterTo")}
        />
      </div>

      {/* Clear filters */}
      {hasFilters && (
        <Button
          variant="ghost"
          size="sm"
          onClick={clearAll}
          className="h-9 px-2 text-xs text-muted-foreground hover:text-foreground"
          aria-label={t("clearFilters")}
        >
          <X className="mr-1 h-3.5 w-3.5" aria-hidden="true" />
          {t("clearFilters")}
        </Button>
      )}

      {/* Refresh */}
      {onRefetch && (
        <Button
          variant="ghost"
          size="icon"
          className="ml-auto h-9 w-9"
          onClick={onRefetch}
          disabled={isLoading}
          aria-label={t("refresh")}
        >
          <RefreshCw
            className={cn("h-4 w-4", isLoading && "animate-spin")}
            aria-hidden="true"
          />
        </Button>
      )}
    </div>
  );
}
