"use client";

import { useState } from "react";
import { useTranslations } from "next-intl";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  UserCog,
  UserX,
  UserCheck,
  RefreshCw,
  Search,
  Eye,
  Loader2,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import {
  listUsers,
  updateUserRole,
  deactivateUser,
  reactivateUser,
  syncFromKeycloak,
  userKeys,
} from "@/lib/api/users";
import type { Role, User } from "@/lib/api/types";
import { ApiError } from "@/lib/api/client";
import { toUserFacingError } from "@/lib/errors";
import { formatDateTR } from "@/lib/utils";

const ALL_ROLES: Role[] = [
  "admin",
  "dpo",
  "hr",
  "manager",
  "it_manager",
  "it_operator",
  "investigator",
  "auditor",
  "employee",
];

interface UsersClientProps {
  currentUserId: string;
}

export function UsersClient({ currentUserId }: UsersClientProps): JSX.Element {
  const t = useTranslations("settings.users");
  const tCommon = useTranslations("common");
  const qc = useQueryClient();

  const [search, setSearch] = useState("");
  const [roleFilter, setRoleFilter] = useState<Role | "all">("all");
  const [roleDialogUser, setRoleDialogUser] = useState<User | null>(null);
  const [pendingRole, setPendingRole] = useState<Role>("employee");

  const params = {
    search: search || undefined,
    role: roleFilter === "all" ? undefined : roleFilter,
    page_size: 50,
  };

  const { data, isLoading, isFetching, error } = useQuery({
    queryKey: userKeys.list(params),
    queryFn: () => listUsers(params),
  });

  const roleMutation = useMutation({
    mutationFn: ({ id, role }: { id: string; role: Role }) =>
      updateUserRole(id, { role }),
    onSuccess: () => {
      toast.success(t("roleUpdated"));
      setRoleDialogUser(null);
      void qc.invalidateQueries({ queryKey: userKeys.all });
    },
    onError: (err) => {
      const ue = toUserFacingError(err);
      toast.error(ue.title, { description: ue.description });
    },
  });

  const deactivateMutation = useMutation({
    mutationFn: (id: string) => deactivateUser(id),
    onSuccess: () => {
      toast.success(t("deactivated"));
      void qc.invalidateQueries({ queryKey: userKeys.all });
    },
    onError: (err) => {
      const ue = toUserFacingError(err);
      toast.error(ue.title, { description: ue.description });
    },
  });

  const reactivateMutation = useMutation({
    mutationFn: (id: string) => reactivateUser(id),
    onSuccess: () => {
      toast.success(t("reactivated"));
      void qc.invalidateQueries({ queryKey: userKeys.all });
    },
    onError: (err) => {
      const ue = toUserFacingError(err);
      toast.error(ue.title, { description: ue.description });
    },
  });

  const syncMutation = useMutation({
    mutationFn: () => syncFromKeycloak(),
    onSuccess: (r) => {
      toast.success(t("syncSuccess", { count: r.synced }));
      void qc.invalidateQueries({ queryKey: userKeys.all });
    },
    onError: (err) => {
      // 501 Not Implemented is expected in Phase 1 — show softer toast.
      // Some backends respond with 404 when the route is not wired yet.
      if (
        err instanceof ApiError &&
        (err.status === 501 || err.status === 404)
      ) {
        toast.info(t("syncPending"));
        return;
      }
      const ue = toUserFacingError(err);
      toast.error(ue.title, { description: ue.description });
    },
  });

  const users = data?.items ?? [];

  return (
    <div className="space-y-4">
      {/* Filters + Sync button */}
      <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
        <div className="flex flex-1 flex-col gap-2 sm:flex-row sm:items-center">
          <div className="relative flex-1 max-w-sm">
            <Search
              className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground"
              aria-hidden="true"
            />
            <Input
              type="search"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder={t("searchPlaceholder")}
              className="pl-8"
              aria-label={t("searchLabel")}
            />
          </div>
          <Select
            value={roleFilter}
            onValueChange={(v) => setRoleFilter(v as Role | "all")}
          >
            <SelectTrigger
              className="w-full sm:w-48"
              aria-label={t("filterByRole")}
            >
              <SelectValue placeholder={t("filterByRole")} />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">{t("allRoles")}</SelectItem>
              {ALL_ROLES.map((role) => (
                <SelectItem key={role} value={role}>
                  {tCommon(`roles.${role}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <Button
          variant="outline"
          size="sm"
          onClick={() => syncMutation.mutate()}
          disabled={syncMutation.isPending}
        >
          {syncMutation.isPending ? (
            <Loader2 className="mr-2 h-4 w-4 animate-spin" aria-hidden="true" />
          ) : (
            <RefreshCw className="mr-2 h-4 w-4" aria-hidden="true" />
          )}
          {t("syncKeycloak")}
        </Button>
      </div>

      {/* Error state */}
      {error && (
        <div
          role="alert"
          className="rounded-md border border-destructive/40 bg-destructive/5 px-4 py-3 text-sm text-destructive"
        >
          {toUserFacingError(error).title}
        </div>
      )}

      {/* Table — scrollable on mobile */}
      <div className="rounded-md border">
        <div className="overflow-x-auto">
          <table className="w-full text-sm" aria-busy={isFetching}>
            <caption className="sr-only">{t("tableCaption")}</caption>
            <thead className="bg-muted/50 text-xs uppercase text-muted-foreground">
              <tr>
                <th scope="col" className="px-4 py-3 text-left font-medium">
                  {t("username")}
                </th>
                <th scope="col" className="px-4 py-3 text-left font-medium">
                  {t("email")}
                </th>
                <th scope="col" className="px-4 py-3 text-left font-medium">
                  {t("role")}
                </th>
                <th scope="col" className="px-4 py-3 text-left font-medium">
                  {t("status")}
                </th>
                <th scope="col" className="px-4 py-3 text-left font-medium">
                  {t("createdAt")}
                </th>
                <th scope="col" className="px-4 py-3 text-right font-medium">
                  {t("actions")}
                </th>
              </tr>
            </thead>
            <tbody>
              {isLoading ? (
                [...Array(5)].map((_, i) => (
                  <tr key={i} className="border-t">
                    <td colSpan={6} className="px-4 py-3">
                      <Skeleton className="h-5 w-full" />
                    </td>
                  </tr>
                ))
              ) : users.length === 0 ? (
                <tr>
                  <td
                    colSpan={6}
                    className="px-4 py-12 text-center text-muted-foreground"
                  >
                    {t("noUsers")}
                  </td>
                </tr>
              ) : (
                users.map((user) => (
                  <tr key={user.id} className="border-t hover:bg-muted/30">
                    <td className="px-4 py-3 font-medium">
                      {user.username}
                      {user.id === currentUserId && (
                        <Badge variant="outline" className="ml-2 text-xs">
                          {t("you")}
                        </Badge>
                      )}
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {user.email}
                    </td>
                    <td className="px-4 py-3">
                      <Badge variant="secondary">
                        {tCommon(`roles.${user.role}`)}
                      </Badge>
                    </td>
                    <td className="px-4 py-3">
                      {user.disabled ? (
                        <Badge variant="destructive">{t("disabled")}</Badge>
                      ) : (
                        <Badge variant="success">{t("active")}</Badge>
                      )}
                    </td>
                    <td className="px-4 py-3 text-xs text-muted-foreground">
                      <time dateTime={user.created_at}>
                        {formatDateTR(user.created_at)}
                      </time>
                    </td>
                    <td className="px-4 py-3 text-right">
                      <div className="flex items-center justify-end gap-1">
                        <Button
                          variant="ghost"
                          size="icon"
                          aria-label={t("viewDetails")}
                          title={t("viewDetails")}
                          disabled
                        >
                          <Eye className="h-4 w-4" aria-hidden="true" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          aria-label={t("changeRole")}
                          title={t("changeRole")}
                          onClick={() => {
                            setRoleDialogUser(user);
                            setPendingRole(user.role);
                          }}
                          disabled={user.id === currentUserId}
                        >
                          <UserCog className="h-4 w-4" aria-hidden="true" />
                        </Button>
                        {user.disabled ? (
                          <Button
                            variant="ghost"
                            size="icon"
                            aria-label={t("reactivate")}
                            title={t("reactivate")}
                            onClick={() => reactivateMutation.mutate(user.id)}
                            disabled={reactivateMutation.isPending}
                          >
                            <UserCheck
                              className="h-4 w-4 text-green-600"
                              aria-hidden="true"
                            />
                          </Button>
                        ) : (
                          <Button
                            variant="ghost"
                            size="icon"
                            aria-label={t("deactivate")}
                            title={t("deactivate")}
                            onClick={() => deactivateMutation.mutate(user.id)}
                            disabled={
                              user.id === currentUserId ||
                              deactivateMutation.isPending
                            }
                          >
                            <UserX
                              className="h-4 w-4 text-destructive"
                              aria-hidden="true"
                            />
                          </Button>
                        )}
                      </div>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>

      {/* Pagination footer */}
      {data && data.pagination.total > 0 && (
        <p className="text-xs text-muted-foreground">
          {t("totalUsers", { count: data.pagination.total })}
        </p>
      )}

      {/* Role change dialog */}
      <Dialog
        open={roleDialogUser !== null}
        onOpenChange={(open) => !open && setRoleDialogUser(null)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("changeRoleTitle")}</DialogTitle>
            <DialogDescription>
              {t("changeRoleDesc", {
                user: roleDialogUser?.username ?? "",
              })}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor="new-role">{t("newRole")}</Label>
            <Select
              value={pendingRole}
              onValueChange={(v) => setPendingRole(v as Role)}
            >
              <SelectTrigger id="new-role">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {ALL_ROLES.map((role) => (
                  <SelectItem key={role} value={role}>
                    {tCommon(`roles.${role}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">
              {t("roleChangeWarning")}
            </p>
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setRoleDialogUser(null)}
              disabled={roleMutation.isPending}
            >
              {t("cancel")}
            </Button>
            <Button
              onClick={() =>
                roleDialogUser &&
                roleMutation.mutate({
                  id: roleDialogUser.id,
                  role: pendingRole,
                })
              }
              disabled={
                !roleDialogUser ||
                roleMutation.isPending ||
                pendingRole === roleDialogUser.role
              }
            >
              {roleMutation.isPending ? t("saving") : t("save")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
