"use client";

import { useMemo, useState } from "react";
import { useTranslations, useLocale } from "next-intl";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  Monitor,
  Plus,
  Eye,
  MoreHorizontal,
  CircleX,
  Trash2,
  ShieldBan,
  PowerOff,
} from "lucide-react";
import {
  listEndpoints,
  revokeEndpoint,
  deleteEndpoint,
  enrollEndpoint,
  bulkEndpointOp,
  isCurrentlyActive,
  endpointKeys,
  BULK_ENDPOINT_LIMIT,
  type EndpointBulkOperation,
} from "@/lib/api/endpoints";
import type { Endpoint, EndpointList, EndpointStatus } from "@/lib/api/types";
import { formatRelativeTR } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Skeleton } from "@/components/ui/skeleton";
import { toUserFacingError } from "@/lib/errors";

const STATUS_BADGE_VARIANTS: Record<
  EndpointStatus,
  "success" | "destructive" | "warning"
> = {
  active: "success",
  revoked: "destructive",
  offline: "warning",
};

interface EndpointsClientProps {
  initialData: EndpointList;
  currentStatus?: EndpointStatus;
  currentPage: number;
}

export function EndpointsClient({
  initialData,
  currentStatus,
  currentPage,
}: EndpointsClientProps): JSX.Element {
  const t = useTranslations("endpoints");
  const locale = useLocale();
  const router = useRouter();
  const qc = useQueryClient();

  const [statusFilter, setStatusFilter] = useState<EndpointStatus | "all">(
    currentStatus ?? "all",
  );
  const [hostnameFilter, setHostnameFilter] = useState("");
  const [osFilter, setOsFilter] = useState<string>("all");
  const [showEnrollDialog, setShowEnrollDialog] = useState(false);
  const [confirmAction, setConfirmAction] = useState<{
    type: "revoke" | "delete";
    endpoint: Endpoint;
  } | null>(null);
  const [enrollHostname, setEnrollHostname] = useState("");
  const [enrolledToken, setEnrolledToken] = useState<{
    secret_id: string;
    role_id: string;
    vault_addr: string;
    expires_at: string;
  } | null>(null);

  // ── Bulk selection state ────────────────────────────────────────────────
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [bulkDialog, setBulkDialog] = useState<EndpointBulkOperation | null>(
    null,
  );
  const [bulkReason, setBulkReason] = useState("");

  const { data, isLoading } = useQuery({
    queryKey: endpointKeys.list({
      status: statusFilter === "all" ? undefined : statusFilter,
      page: currentPage,
    }),
    queryFn: () =>
      listEndpoints({
        status: statusFilter === "all" ? undefined : statusFilter,
        page: currentPage,
        page_size: 50,
      }),
    initialData:
      statusFilter === (currentStatus ?? "all") ? initialData : undefined,
    // Keep the list fresh for the liveness dot — 30s cadence matches the
    // gateway heartbeat.
    refetchInterval: 30_000,
  });

  const revokeMutation = useMutation({
    mutationFn: (id: string) => revokeEndpoint(id),
    onSuccess: () => {
      toast.success("Uç nokta sertifikası iptal edildi.");
      void qc.invalidateQueries({ queryKey: endpointKeys.all });
      setConfirmAction(null);
    },
    onError: (err) => {
      const ue = toUserFacingError(err);
      toast.error(ue.description);
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => deleteEndpoint(id),
    onSuccess: () => {
      toast.success("Uç nokta silindi.");
      void qc.invalidateQueries({ queryKey: endpointKeys.all });
      setConfirmAction(null);
    },
    onError: (err) => {
      const ue = toUserFacingError(err);
      toast.error(ue.description);
    },
  });

  const enrollMutation = useMutation({
    mutationFn: () => enrollEndpoint({ hostname: enrollHostname }),
    onSuccess: (token) => {
      setEnrolledToken(token);
      void qc.invalidateQueries({ queryKey: endpointKeys.all });
    },
    onError: (err) => {
      const ue = toUserFacingError(err);
      toast.error(ue.description);
    },
  });

  const bulkMutation = useMutation({
    mutationFn: (op: EndpointBulkOperation) =>
      bulkEndpointOp(op, Array.from(selectedIds), bulkReason),
    onSuccess: (result) => {
      toast.success(
        t("actions.bulkSuccess", {
          accepted: result.accepted,
          rejected: result.rejected,
        }),
      );
      setSelectedIds(new Set());
      setBulkDialog(null);
      setBulkReason("");
      void qc.invalidateQueries({ queryKey: endpointKeys.all });
    },
    onError: (err) => {
      const ue = toUserFacingError(err);
      toast.error(ue.title, { description: ue.description });
    },
  });

  const handleStatusChange = (value: string) => {
    const newStatus = value as EndpointStatus | "all";
    setStatusFilter(newStatus);
    const params = new URLSearchParams();
    if (newStatus !== "all") params.set("status", newStatus);
    router.push(`/${locale}/endpoints?${params.toString()}`);
  };

  const rawEndpoints = data?.items ?? [];
  const pagination = data?.pagination;

  // Client-side filter for hostname + os (backend doesn't always support).
  const endpoints = useMemo(() => {
    return rawEndpoints.filter((ep) => {
      if (
        hostnameFilter &&
        !ep.hostname.toLowerCase().includes(hostnameFilter.toLowerCase())
      ) {
        return false;
      }
      if (osFilter !== "all") {
        const os = ep.os_version ?? "";
        if (osFilter === "windows" && !/windows/i.test(os)) return false;
        if (osFilter === "macos" && !/mac ?os|darwin/i.test(os)) return false;
        if (osFilter === "linux" && !/linux|ubuntu|debian/i.test(os))
          return false;
      }
      return true;
    });
  }, [rawEndpoints, hostnameFilter, osFilter]);

  const allVisibleSelected =
    endpoints.length > 0 && endpoints.every((ep) => selectedIds.has(ep.id));

  const toggleAll = () => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (allVisibleSelected) {
        endpoints.forEach((ep) => next.delete(ep.id));
      } else {
        endpoints.forEach((ep) => next.add(ep.id));
      }
      return next;
    });
  };

  const toggleOne = (id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const bulkLimitExceeded = selectedIds.size > BULK_ENDPOINT_LIMIT;

  return (
    <div className="space-y-4">
      {/* Filters + actions row */}
      <div className="flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between">
        <div className="flex flex-wrap items-center gap-2">
          <Select value={statusFilter} onValueChange={handleStatusChange}>
            <SelectTrigger className="w-40" aria-label="Durum filtresi">
              <SelectValue placeholder={t("filters.allStatuses")} />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">{t("filters.allStatuses")}</SelectItem>
              <SelectItem value="active">{t("status.active")}</SelectItem>
              <SelectItem value="offline">{t("status.offline")}</SelectItem>
              <SelectItem value="revoked">{t("status.revoked")}</SelectItem>
            </SelectContent>
          </Select>

          <Select value={osFilter} onValueChange={setOsFilter}>
            <SelectTrigger className="w-40" aria-label="OS filtresi">
              <SelectValue placeholder={t("filters.allOs")} />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">{t("filters.allOs")}</SelectItem>
              <SelectItem value="windows">Windows</SelectItem>
              <SelectItem value="macos">macOS</SelectItem>
              <SelectItem value="linux">Linux</SelectItem>
            </SelectContent>
          </Select>

          <Input
            value={hostnameFilter}
            onChange={(e) => setHostnameFilter(e.target.value)}
            placeholder={t("filters.searchPlaceholder")}
            className="w-56"
            aria-label={t("filters.searchPlaceholder")}
          />
        </div>

        <div className="flex items-center gap-2">
          {selectedIds.size > 0 && (
            <>
              <span className="text-xs text-muted-foreground">
                {t("actions.bulkSelected", { count: selectedIds.size })}
              </span>
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={bulkLimitExceeded}
                    aria-label={t("actions.bulkApply")}
                    data-testid="bulk-apply-trigger"
                  >
                    {t("actions.bulkApply")}
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuItem
                    onClick={() => setBulkDialog("deactivate")}
                    className="text-amber-600"
                  >
                    <PowerOff className="h-4 w-4" aria-hidden="true" />
                    {t("actions.deactivate")}
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    onClick={() => setBulkDialog("revoke")}
                    className="text-rose-700"
                  >
                    <ShieldBan className="h-4 w-4" aria-hidden="true" />
                    {t("actions.revoke")}
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem
                    onClick={() => setBulkDialog("wipe")}
                    className="text-red-700"
                  >
                    <Trash2 className="h-4 w-4" aria-hidden="true" />
                    {t("actions.wipe")}
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </>
          )}
          <Button
            onClick={() => {
              setEnrollHostname("");
              setEnrolledToken(null);
              setShowEnrollDialog(true);
            }}
            className="shrink-0"
            aria-label={t("enrollTitle")}
          >
            <Plus className="h-4 w-4" aria-hidden="true" />
            {t("enroll")}
          </Button>
        </div>
      </div>

      {bulkLimitExceeded && (
        <p className="text-sm text-red-600" role="alert">
          {t("actions.bulkLimitExceeded", { limit: BULK_ENDPOINT_LIMIT })}
        </p>
      )}

      {/* Table */}
      <div className="rounded-md border">
        <table
          className="w-full text-sm"
          role="table"
          aria-label="Uç nokta listesi"
        >
          <thead>
            <tr className="border-b bg-muted/50">
              <th className="px-4 py-3 text-left" scope="col">
                <Checkbox
                  checked={allVisibleSelected}
                  onCheckedChange={toggleAll}
                  aria-label="Tümünü seç"
                />
              </th>
              <th
                className="px-4 py-3 text-left font-medium text-muted-foreground"
                scope="col"
              >
                {t("detail.hostname")}
              </th>
              <th
                className="px-4 py-3 text-left font-medium text-muted-foreground hidden md:table-cell"
                scope="col"
              >
                {t("detail.osVersion")}
              </th>
              <th
                className="px-4 py-3 text-left font-medium text-muted-foreground hidden lg:table-cell"
                scope="col"
              >
                {t("detail.agentVersion")}
              </th>
              <th
                className="px-4 py-3 text-left font-medium text-muted-foreground"
                scope="col"
              >
                {t("detail.status")}
              </th>
              <th
                className="px-4 py-3 text-left font-medium text-muted-foreground hidden sm:table-cell"
                scope="col"
              >
                {t("detail.lastSeen")}
              </th>
              <th
                className="px-4 py-3 text-right font-medium text-muted-foreground"
                scope="col"
              >
                <span className="sr-only">İşlemler</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {isLoading ? (
              Array.from({ length: 5 }).map((_, i) => (
                <tr key={i} className="border-b">
                  <td className="px-4 py-3" colSpan={7}>
                    <Skeleton className="h-5 w-full" />
                  </td>
                </tr>
              ))
            ) : endpoints.length === 0 ? (
              <tr>
                <td
                  className="px-4 py-8 text-center text-muted-foreground"
                  colSpan={7}
                >
                  Kayıt bulunamadı.
                </td>
              </tr>
            ) : (
              endpoints.map((endpoint) => {
                const live = isCurrentlyActive(endpoint);
                const selected = selectedIds.has(endpoint.id);
                return (
                  <tr
                    key={endpoint.id}
                    className="border-b transition-colors hover:bg-muted/30"
                    data-selected={selected ? "true" : undefined}
                  >
                    <td className="px-4 py-3">
                      <Checkbox
                        checked={selected}
                        onCheckedChange={() => toggleOne(endpoint.id)}
                        aria-label={`${endpoint.hostname} seç`}
                      />
                    </td>
                    <td className="px-4 py-3 font-medium">
                      <Link
                        href={`/${locale}/endpoints/${endpoint.id}`}
                        className="inline-flex items-center gap-2 hover:text-primary hover:underline focus-visible:text-primary focus-visible:underline"
                      >
                        <Monitor
                          className="h-4 w-4 text-muted-foreground"
                          aria-hidden="true"
                        />
                        <span>{endpoint.hostname}</span>
                        <span
                          className={`inline-flex h-2 w-2 rounded-full ${
                            live
                              ? "bg-green-500 animate-pulse"
                              : endpoint.status === "offline"
                                ? "bg-amber-500"
                                : "bg-muted-foreground/40"
                          }`}
                          aria-label={
                            live
                              ? t("liveness.currentlyActive")
                              : t("liveness.recentlyOffline")
                          }
                          title={
                            live
                              ? t("liveness.currentlyActive")
                              : t("liveness.recentlyOffline")
                          }
                        />
                      </Link>
                    </td>
                    <td className="px-4 py-3 text-muted-foreground hidden md:table-cell">
                      {endpoint.os_version ?? "—"}
                    </td>
                    <td className="px-4 py-3 text-muted-foreground hidden lg:table-cell">
                      {endpoint.agent_version ?? "—"}
                    </td>
                    <td className="px-4 py-3">
                      <Badge variant={STATUS_BADGE_VARIANTS[endpoint.status]}>
                        {t(`status.${endpoint.status}`)}
                      </Badge>
                    </td>
                    <td className="px-4 py-3 text-muted-foreground hidden sm:table-cell">
                      {endpoint.last_seen_at
                        ? formatRelativeTR(endpoint.last_seen_at)
                        : "—"}
                    </td>
                    <td className="px-4 py-3 text-right">
                      <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                          <Button
                            variant="ghost"
                            size="icon"
                            aria-label={`${endpoint.hostname} için işlemler`}
                          >
                            <MoreHorizontal
                              className="h-4 w-4"
                              aria-hidden="true"
                            />
                          </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end">
                          <DropdownMenuItem asChild>
                            <Link
                              href={`/${locale}/endpoints/${endpoint.id}`}
                            >
                              <Eye className="h-4 w-4" aria-hidden="true" />
                              Görüntüle
                            </Link>
                          </DropdownMenuItem>
                          <DropdownMenuSeparator />
                          <DropdownMenuItem
                            onClick={() =>
                              setConfirmAction({
                                type: "revoke",
                                endpoint,
                              })
                            }
                            disabled={endpoint.status === "revoked"}
                            className="text-amber-600"
                          >
                            <CircleX
                              className="h-4 w-4"
                              aria-hidden="true"
                            />
                            {t("actions.revoke")}
                          </DropdownMenuItem>
                          <DropdownMenuItem
                            onClick={() =>
                              setConfirmAction({
                                type: "delete",
                                endpoint,
                              })
                            }
                            className="text-destructive"
                          >
                            <Trash2
                              className="h-4 w-4"
                              aria-hidden="true"
                            />
                            {t("actions.delete")}
                          </DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </td>
                  </tr>
                );
              })
            )}
          </tbody>
        </table>
      </div>

      {/* Pagination info */}
      {pagination && (
        <p className="text-xs text-muted-foreground">
          {pagination.total.toLocaleString("tr-TR")} kayıttan{" "}
          {Math.min(
            (pagination.page - 1) * pagination.page_size + 1,
            pagination.total,
          )}
          –{Math.min(pagination.page * pagination.page_size, pagination.total)}{" "}
          gösteriliyor
        </p>
      )}

      {/* Enroll dialog (unchanged) */}
      <Dialog open={showEnrollDialog} onOpenChange={setShowEnrollDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("enrollTitle")}</DialogTitle>
            <DialogDescription>
              Yeni bir Windows uç noktası için tek kullanımlık kayıt token'ı
              oluşturun.
            </DialogDescription>
          </DialogHeader>

          {!enrolledToken ? (
            <>
              <div className="space-y-2">
                <Label htmlFor="enroll-hostname">
                  {t("enrollment.hostname")}
                </Label>
                <Input
                  id="enroll-hostname"
                  value={enrollHostname}
                  onChange={(e) => setEnrollHostname(e.target.value)}
                  placeholder={t("enrollment.hostnameHint")}
                  autoFocus
                />
              </div>
              <DialogFooter>
                <Button
                  variant="outline"
                  onClick={() => setShowEnrollDialog(false)}
                >
                  İptal
                </Button>
                <Button
                  onClick={() => enrollMutation.mutate()}
                  disabled={
                    !enrollHostname.trim() || enrollMutation.isPending
                  }
                >
                  {enrollMutation.isPending
                    ? "Oluşturuluyor..."
                    : "Token Oluştur"}
                </Button>
              </DialogFooter>
            </>
          ) : (
            <>
              <div className="space-y-3 rounded-md bg-muted p-4">
                <p className="text-sm font-medium text-amber-600">
                  {t("enrollment.tokenWarning")}
                </p>
                <div className="space-y-1">
                  <p className="text-xs text-muted-foreground">
                    {t("enrollment.secretId")}
                  </p>
                  <code className="font-hash text-xs block bg-background rounded p-2 break-all">
                    {enrolledToken.secret_id}
                  </code>
                </div>
                <div className="space-y-1">
                  <p className="text-xs text-muted-foreground">
                    {t("enrollment.roleId")}
                  </p>
                  <code className="font-hash text-xs block bg-background rounded p-2">
                    {enrolledToken.role_id}
                  </code>
                </div>
                <div className="space-y-1">
                  <p className="text-xs text-muted-foreground">
                    {t("enrollment.vaultAddr")}
                  </p>
                  <code className="font-hash text-xs block bg-background rounded p-2">
                    {enrolledToken.vault_addr}
                  </code>
                </div>
              </div>
              <DialogFooter>
                <Button onClick={() => setShowEnrollDialog(false)}>
                  Kapat
                </Button>
              </DialogFooter>
            </>
          )}
        </DialogContent>
      </Dialog>

      {/* Confirm single-endpoint action dialog */}
      <Dialog
        open={!!confirmAction}
        onOpenChange={(o) => !o && setConfirmAction(null)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {confirmAction?.type === "revoke"
                ? t("actions.revoke")
                : t("actions.delete")}
            </DialogTitle>
            <DialogDescription>
              {confirmAction?.type === "revoke"
                ? t("actions.revokeConfirm")
                : t("actions.deleteConfirm")}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmAction(null)}>
              İptal
            </Button>
            <Button
              variant="destructive"
              onClick={() => {
                if (!confirmAction) return;
                if (confirmAction.type === "revoke") {
                  revokeMutation.mutate(confirmAction.endpoint.id);
                } else {
                  deleteMutation.mutate(confirmAction.endpoint.id);
                }
              }}
              disabled={revokeMutation.isPending || deleteMutation.isPending}
            >
              {revokeMutation.isPending || deleteMutation.isPending
                ? "İşleniyor..."
                : "Onayla"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Bulk operation dialog */}
      <Dialog
        open={!!bulkDialog}
        onOpenChange={(o) => {
          if (!o) {
            setBulkDialog(null);
            setBulkReason("");
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {bulkDialog &&
                t("actions.bulkTitle", {
                  operation:
                    bulkDialog === "wipe"
                      ? t("actions.wipe")
                      : bulkDialog === "revoke"
                        ? t("actions.revoke")
                        : t("actions.deactivate"),
                })}
            </DialogTitle>
            <DialogDescription>
              {bulkDialog &&
                t("actions.bulkDesc", {
                  operation:
                    bulkDialog === "wipe"
                      ? t("actions.wipe")
                      : bulkDialog === "revoke"
                        ? t("actions.revoke")
                        : t("actions.deactivate"),
                  count: selectedIds.size,
                })}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor="bulk-reason">
              {t("actions.bulkReasonLabel")}
            </Label>
            <Textarea
              id="bulk-reason"
              value={bulkReason}
              onChange={(e) => setBulkReason(e.target.value)}
              rows={3}
            />
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => {
                setBulkDialog(null);
                setBulkReason("");
              }}
            >
              İptal
            </Button>
            <Button
              variant={
                bulkDialog === "wipe" || bulkDialog === "revoke"
                  ? "destructive"
                  : "default"
              }
              onClick={() => bulkDialog && bulkMutation.mutate(bulkDialog)}
              disabled={
                !bulkReason.trim() ||
                bulkLimitExceeded ||
                bulkMutation.isPending
              }
            >
              {bulkMutation.isPending ? "İşleniyor..." : "Onayla"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
