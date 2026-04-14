"use client";

/**
 * Endpoint detail client — tabs, lifecycle actions, command history.
 *
 * KVKK / ops notes:
 * - Wipe requires a reason AND typed "WIPE"/"SIL" confirmation.
 * - Wipe path surfaces legal hold blocks (409 CONFLICT) as a yellow banner
 *   instead of a destructive toast so DPO can follow up.
 * - Revoke is a soft cert-only revoke (agent can re-enroll).
 * - Refresh token shows rate-limit remaining so IT ops can tell if they
 *   are being throttled.
 */

import { useState } from "react";
import { useTranslations, useLocale } from "next-intl";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { toast } from "sonner";
import {
  ChevronLeft,
  ShieldBan,
  Trash2,
  RefreshCw,
  PowerOff,
  AlertTriangle,
  CheckCircle2,
  Clock,
} from "lucide-react";
import {
  getEndpoint,
  listEndpointCommands,
  deactivateEndpoint,
  wipeEndpoint,
  refreshEndpointToken,
  revokeEndpoint,
  isCurrentlyActive,
  endpointKeys,
  type EndpointCommand,
  type EndpointCommandList,
} from "@/lib/api/endpoints";
import type { Endpoint, EndpointStatus, Role } from "@/lib/api/types";
import { can } from "@/lib/auth/rbac";
import { formatDateTR, formatRelativeTR } from "@/lib/utils";
import { toUserFacingError } from "@/lib/errors";
import { ApiError } from "@/lib/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Skeleton } from "@/components/ui/skeleton";

const STATUS_VARIANTS: Record<
  EndpointStatus,
  "success" | "destructive" | "warning"
> = {
  active: "success",
  revoked: "destructive",
  offline: "warning",
};

type DialogKind = "deactivate" | "wipe" | "refresh" | "revoke" | null;

interface EndpointDetailClientProps {
  endpoint: Endpoint;
  initialCommands: EndpointCommandList;
  role: Role;
}

export function EndpointDetailClient({
  endpoint: initialEndpoint,
  initialCommands,
  role,
}: EndpointDetailClientProps): JSX.Element {
  const t = useTranslations("endpoints");
  const tc = useTranslations("common");
  const locale = useLocale();
  const qc = useQueryClient();

  const [activeTab, setActiveTab] = useState("overview");
  const [dialog, setDialog] = useState<DialogKind>(null);
  const [reason, setReason] = useState("");
  const [wipeConfirmText, setWipeConfirmText] = useState("");
  const [legalHoldBlock, setLegalHoldBlock] = useState<string | null>(null);
  const [refreshQuota, setRefreshQuota] = useState<number | null>(null);

  const canWipe = can(role, "wipe:endpoint");
  const canDeactivate = can(role, "deactivate:endpoint");
  const canRefresh = can(role, "refresh:endpoint-token");
  const canRevoke = can(role, "revoke:endpoint-cert");

  // Live polling — treat the endpoint detail as a live view that refetches
  // every 30 s. Keeps the "currently active" dot honest.
  const { data: endpoint = initialEndpoint } = useQuery({
    queryKey: endpointKeys.detail(initialEndpoint.id),
    queryFn: () => getEndpoint(initialEndpoint.id),
    initialData: initialEndpoint,
    refetchInterval: 30_000,
  });

  const { data: commands = initialCommands } = useQuery({
    queryKey: endpointKeys.commands(initialEndpoint.id),
    queryFn: () => listEndpointCommands(initialEndpoint.id),
    initialData: initialCommands,
    refetchInterval: 30_000,
  });

  const resetDialogState = () => {
    setDialog(null);
    setReason("");
    setWipeConfirmText("");
    setLegalHoldBlock(null);
  };

  const handleMutationError = (err: unknown, kind: DialogKind) => {
    // Legal hold is a hard 409 with a specific shape — surface it as a
    // persistent yellow warning instead of a toast that disappears.
    if (err instanceof ApiError && err.status === 409 && kind === "wipe") {
      setLegalHoldBlock(err.problem.detail ?? t("actions.legalHoldBlock"));
      return;
    }
    if (
      err instanceof ApiError &&
      err.status === 429 &&
      kind === "refresh"
    ) {
      toast.error(t("actions.rateLimitHit"));
      return;
    }
    const ufe = toUserFacingError(err);
    toast.error(ufe.title, { description: ufe.description });
  };

  const deactivateMutation = useMutation({
    mutationFn: () => deactivateEndpoint(endpoint.id, reason),
    onSuccess: () => {
      toast.success(t("actions.deactivate"));
      void qc.invalidateQueries({ queryKey: endpointKeys.all });
      resetDialogState();
    },
    onError: (err) => handleMutationError(err, "deactivate"),
  });

  const wipeMutation = useMutation({
    mutationFn: () => wipeEndpoint(endpoint.id, reason),
    onSuccess: () => {
      toast.success(t("actions.wipe"));
      void qc.invalidateQueries({ queryKey: endpointKeys.all });
      resetDialogState();
    },
    onError: (err) => handleMutationError(err, "wipe"),
  });

  const refreshMutation = useMutation({
    // The console does not own a key pair; in real operation the agent
    // generates the CSR. For IT ops usage we send an empty CSR which the
    // backend treats as "re-issue with existing subject".
    mutationFn: () => refreshEndpointToken(endpoint.id, ""),
    onSuccess: (res) => {
      toast.success(t("actions.refresh"));
      if (typeof res.rate_limit_remaining === "number") {
        setRefreshQuota(res.rate_limit_remaining);
      }
      void qc.invalidateQueries({ queryKey: endpointKeys.all });
      resetDialogState();
    },
    onError: (err) => handleMutationError(err, "refresh"),
  });

  const revokeMutation = useMutation({
    mutationFn: () => revokeEndpoint(endpoint.id),
    onSuccess: () => {
      toast.success(t("actions.revoke"));
      void qc.invalidateQueries({ queryKey: endpointKeys.all });
      resetDialogState();
    },
    onError: (err) => handleMutationError(err, "revoke"),
  });

  const currentlyActive = isCurrentlyActive(endpoint);

  return (
    <div className="space-y-6 animate-fade-in">
      <Button variant="ghost" size="sm" className="-ml-2" asChild>
        <Link href={`/${locale}/endpoints`}>
          <ChevronLeft className="mr-1 h-4 w-4" aria-hidden="true" />
          {t("backToList")}
        </Link>
      </Button>

      {/* Header */}
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="space-y-1">
          <div className="flex items-center gap-3">
            <h1 className="text-2xl font-bold tracking-tight font-mono">
              {endpoint.hostname}
            </h1>
            <span
              className={`inline-flex h-2.5 w-2.5 rounded-full ${
                currentlyActive
                  ? "bg-green-500 animate-pulse"
                  : endpoint.status === "offline"
                    ? "bg-amber-500"
                    : "bg-muted-foreground/40"
              }`}
              aria-label={
                currentlyActive
                  ? t("liveness.currentlyActive")
                  : t("liveness.recentlyOffline")
              }
              title={
                currentlyActive
                  ? t("liveness.currentlyActive")
                  : t("liveness.recentlyOffline")
              }
            />
          </div>
          <code className="text-xs text-muted-foreground">{endpoint.id}</code>
        </div>
        <Badge variant={STATUS_VARIANTS[endpoint.status] ?? "default"}>
          {t(`status.${endpoint.status}`)}
        </Badge>
      </div>

      <Tabs value={activeTab} onValueChange={setActiveTab} className="w-full">
        <TabsList>
          <TabsTrigger value="overview">{t("detailTabs.overview")}</TabsTrigger>
          <TabsTrigger value="commands">{t("detailTabs.commands")}</TabsTrigger>
          <TabsTrigger value="policies">{t("detailTabs.policies")}</TabsTrigger>
          <TabsTrigger value="actions">{t("detailTabs.actions")}</TabsTrigger>
        </TabsList>

        <TabsContent value="overview" className="space-y-4">
          <dl className="grid grid-cols-2 gap-4 rounded-lg border bg-card p-4 text-sm">
            <div>
              <dt className="text-xs text-muted-foreground">
                {t("detail.osVersion")}
              </dt>
              <dd>{endpoint.os_version ?? "—"}</dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">
                {t("detail.agentVersion")}
              </dt>
              <dd>{endpoint.agent_version ?? "—"}</dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">
                {t("detail.enrolledAt")}
              </dt>
              <dd>
                <time dateTime={endpoint.enrolled_at}>
                  {formatDateTR(endpoint.enrolled_at)}
                </time>
              </dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">
                {t("detail.lastSeen")}
              </dt>
              <dd>
                {endpoint.last_seen_at ? (
                  <time dateTime={endpoint.last_seen_at}>
                    {formatRelativeTR(endpoint.last_seen_at)}
                  </time>
                ) : (
                  "—"
                )}
              </dd>
            </div>
          </dl>
        </TabsContent>

        <TabsContent value="commands" className="space-y-4">
          <CommandHistoryTable commands={commands.items} />
        </TabsContent>

        <TabsContent value="policies" className="space-y-4">
          <div className="rounded-lg border bg-card p-4 text-sm">
            <dt className="text-xs text-muted-foreground">
              {t("detail.policy")}
            </dt>
            <dd className="mt-1 font-mono text-xs">
              {endpoint.policy_id ?? "—"}
            </dd>
          </div>
        </TabsContent>

        <TabsContent value="actions" className="space-y-4">
          {legalHoldBlock && (
            <div className="rounded-md border border-amber-300 bg-amber-50 p-4 text-sm text-amber-900 dark:bg-amber-900/20 dark:text-amber-200">
              <div className="flex items-start gap-3">
                <AlertTriangle
                  className="mt-0.5 h-5 w-5 shrink-0"
                  aria-hidden="true"
                />
                <div>
                  <p className="font-medium">
                    {t("actions.legalHoldBlock")}
                  </p>
                  <p className="mt-1 text-xs opacity-80">{legalHoldBlock}</p>
                </div>
              </div>
            </div>
          )}

          <div className="grid gap-3 sm:grid-cols-2">
            <ActionCard
              icon={<PowerOff className="h-5 w-5 text-amber-600" />}
              title={t("actions.deactivate")}
              description={t("actions.deactivateDesc", {
                hostname: endpoint.hostname,
              })}
              disabled={!canDeactivate || endpoint.status !== "active"}
              onClick={() => setDialog("deactivate")}
              variant="warning"
            />

            <ActionCard
              icon={<RefreshCw className="h-5 w-5 text-blue-600" />}
              title={t("actions.refresh")}
              description={t("actions.refreshDesc")}
              disabled={!canRefresh || endpoint.status !== "active"}
              onClick={() => setDialog("refresh")}
              variant="info"
              extra={
                refreshQuota !== null
                  ? t("actions.refreshRateLimit", { remaining: refreshQuota })
                  : undefined
              }
            />

            <ActionCard
              icon={<ShieldBan className="h-5 w-5 text-rose-800" />}
              title={t("actions.revoke")}
              description={t("actions.revokeConfirm")}
              disabled={!canRevoke || endpoint.status === "revoked"}
              onClick={() => setDialog("revoke")}
              variant="danger"
            />

            <ActionCard
              icon={<Trash2 className="h-5 w-5 text-red-700" />}
              title={t("actions.wipe")}
              description={t("actions.confirmWipe", {
                hostname: endpoint.hostname,
              })}
              disabled={!canWipe}
              onClick={() => {
                setLegalHoldBlock(null);
                setDialog("wipe");
              }}
              variant="critical"
            />
          </div>
        </TabsContent>
      </Tabs>

      {/* Deactivate dialog */}
      <Dialog
        open={dialog === "deactivate"}
        onOpenChange={(open) => !open && resetDialogState()}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("actions.deactivateTitle")}</DialogTitle>
            <DialogDescription>
              {t("actions.deactivateDesc", { hostname: endpoint.hostname })}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor="deact-reason">
              {t("actions.deactivateReasonLabel")}
            </Label>
            <Textarea
              id="deact-reason"
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              placeholder={t("actions.deactivateReasonPlaceholder")}
              rows={3}
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={resetDialogState}>
              {tc("cancel")}
            </Button>
            <Button
              onClick={() => deactivateMutation.mutate()}
              disabled={!reason.trim() || deactivateMutation.isPending}
            >
              {deactivateMutation.isPending ? tc("saving") : tc("confirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Wipe dialog */}
      <Dialog
        open={dialog === "wipe"}
        onOpenChange={(open) => !open && resetDialogState()}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="text-red-700">
              {t("actions.wipe")}
            </DialogTitle>
            <DialogDescription className="text-red-900 dark:text-red-200">
              {t("actions.confirmWipe", { hostname: endpoint.hostname })}
            </DialogDescription>
          </DialogHeader>
          {legalHoldBlock && (
            <div
              className="rounded-md border border-amber-300 bg-amber-50 p-3 text-sm text-amber-900 dark:bg-amber-900/20 dark:text-amber-200"
              role="alert"
            >
              <div className="flex items-start gap-2">
                <AlertTriangle className="h-4 w-4 shrink-0" />
                <span>{t("actions.legalHoldBlock")}</span>
              </div>
            </div>
          )}
          <div className="space-y-3">
            <div className="space-y-1">
              <Label htmlFor="wipe-reason">
                {t("actions.wipeReasonLabel")}
              </Label>
              <Textarea
                id="wipe-reason"
                value={reason}
                onChange={(e) => setReason(e.target.value)}
                placeholder={t("actions.wipeReasonPlaceholder")}
                rows={3}
              />
            </div>
            <div className="space-y-1">
              <Label htmlFor="wipe-confirm">
                {t("actions.typeToConfirm")}
              </Label>
              <Input
                id="wipe-confirm"
                value={wipeConfirmText}
                onChange={(e) =>
                  setWipeConfirmText(e.target.value.toUpperCase())
                }
                placeholder={t("actions.typeToConfirmToken")}
                aria-required="true"
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={resetDialogState}>
              {tc("cancel")}
            </Button>
            <Button
              variant="destructive"
              onClick={() => wipeMutation.mutate()}
              disabled={
                !reason.trim() ||
                wipeConfirmText !== t("actions.typeToConfirmToken") ||
                wipeMutation.isPending
              }
              data-testid="confirm-wipe-button"
            >
              {wipeMutation.isPending ? tc("saving") : t("actions.wipe")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Refresh token dialog */}
      <Dialog
        open={dialog === "refresh"}
        onOpenChange={(open) => !open && resetDialogState()}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("actions.refreshTitle")}</DialogTitle>
            <DialogDescription>{t("actions.refreshDesc")}</DialogDescription>
          </DialogHeader>
          {refreshQuota !== null && (
            <p className="text-xs text-muted-foreground">
              {t("actions.refreshRateLimit", { remaining: refreshQuota })}
            </p>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={resetDialogState}>
              {tc("cancel")}
            </Button>
            <Button
              onClick={() => refreshMutation.mutate()}
              disabled={refreshMutation.isPending}
            >
              {refreshMutation.isPending ? tc("saving") : tc("confirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Revoke dialog */}
      <Dialog
        open={dialog === "revoke"}
        onOpenChange={(open) => !open && resetDialogState()}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("actions.revoke")}</DialogTitle>
            <DialogDescription>
              {t("actions.revokeConfirm")}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={resetDialogState}>
              {tc("cancel")}
            </Button>
            <Button
              variant="destructive"
              onClick={() => revokeMutation.mutate()}
              disabled={revokeMutation.isPending}
            >
              {revokeMutation.isPending ? tc("saving") : tc("confirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

// ── Sub-components ───────────────────────────────────────────────────────────

function ActionCard({
  icon,
  title,
  description,
  disabled,
  onClick,
  variant,
  extra,
}: {
  icon: React.ReactNode;
  title: string;
  description: string;
  disabled: boolean;
  onClick: () => void;
  variant: "info" | "warning" | "danger" | "critical";
  extra?: string;
}): JSX.Element {
  const borderClass =
    variant === "critical"
      ? "border-red-300 bg-red-50/30 dark:bg-red-900/10"
      : variant === "danger"
        ? "border-rose-300"
        : variant === "warning"
          ? "border-amber-300"
          : "border-blue-300";
  return (
    <div
      className={`flex flex-col gap-3 rounded-lg border ${borderClass} p-4`}
    >
      <div className="flex items-start gap-3">
        {icon}
        <div className="space-y-1">
          <p className="font-semibold text-sm">{title}</p>
          <p className="text-xs text-muted-foreground">{description}</p>
          {extra && (
            <p className="text-[11px] text-muted-foreground/80">{extra}</p>
          )}
        </div>
      </div>
      <Button
        size="sm"
        variant={variant === "critical" || variant === "danger" ? "destructive" : "outline"}
        onClick={onClick}
        disabled={disabled}
        className="self-end"
      >
        {title}
      </Button>
    </div>
  );
}

function CommandHistoryTable({
  commands,
}: {
  commands: EndpointCommand[];
}): JSX.Element {
  const t = useTranslations("endpoints");

  if (commands.length === 0) {
    return (
      <div className="rounded-md border p-8 text-center text-sm text-muted-foreground">
        {t("commandHistory.empty")}
      </div>
    );
  }

  return (
    <div className="rounded-md border">
      <table
        className="w-full text-sm"
        role="table"
        aria-label="Endpoint command history"
      >
        <thead>
          <tr className="border-b bg-muted/50">
            <th className="px-4 py-3 text-left font-medium text-muted-foreground">
              {t("commandHistory.type")}
            </th>
            <th className="px-4 py-3 text-left font-medium text-muted-foreground">
              {t("commandHistory.status")}
            </th>
            <th className="px-4 py-3 text-left font-medium text-muted-foreground hidden sm:table-cell">
              {t("commandHistory.queuedAt")}
            </th>
            <th className="px-4 py-3 text-left font-medium text-muted-foreground hidden md:table-cell">
              {t("commandHistory.ackedAt")}
            </th>
          </tr>
        </thead>
        <tbody>
          {commands.map((cmd) => (
            <tr key={cmd.id} className="border-b hover:bg-muted/30">
              <td className="px-4 py-3 font-mono text-xs">
                {cmd.command_type}
              </td>
              <td className="px-4 py-3">
                <CommandStatusBadge status={cmd.status} />
              </td>
              <td className="px-4 py-3 text-xs text-muted-foreground hidden sm:table-cell">
                {formatRelativeTR(cmd.queued_at)}
              </td>
              <td className="px-4 py-3 text-xs text-muted-foreground hidden md:table-cell">
                {cmd.acked_at ? formatRelativeTR(cmd.acked_at) : "—"}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function CommandStatusBadge({
  status,
}: {
  status: EndpointCommand["status"];
}): JSX.Element {
  const map: Record<
    EndpointCommand["status"],
    { variant: "success" | "warning" | "destructive" | "outline"; icon: React.ElementType }
  > = {
    queued: { variant: "outline", icon: Clock },
    sent: { variant: "outline", icon: Clock },
    acked: { variant: "success", icon: CheckCircle2 },
    failed: { variant: "destructive", icon: AlertTriangle },
    expired: { variant: "warning", icon: AlertTriangle },
  };
  const { variant, icon: Icon } = map[status];
  return (
    <Badge variant={variant} className="text-xs">
      <Icon className="mr-1 h-3 w-3" aria-hidden="true" />
      {status}
    </Badge>
  );
}

// Keep Skeleton import used in case future suspense fallbacks live here.
void Skeleton;
