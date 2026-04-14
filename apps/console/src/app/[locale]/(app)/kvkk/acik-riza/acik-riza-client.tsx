"use client";

import { useMemo, useState } from "react";
import { useTranslations } from "next-intl";
import {
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { toast } from "sonner";
import {
  Download,
  Upload,
  CheckCircle2,
  Clock,
  MoreVertical,
  Undo2,
  FilePlus2,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  listConsents,
  recordConsent,
  revokeConsent,
  kvkkKeys,
  type ConsentList,
  type ConsentRecord,
} from "@/lib/api/kvkk";
import { listUsers, userKeys, type ListUsersParams } from "@/lib/api/users";
import type { User, UserList } from "@/lib/api/types";
import { toUserFacingError } from "@/lib/errors";
import { formatDateTR } from "@/lib/utils";

// Stable reference so TanStack Query key does not churn per render.
const USERS_PARAMS: ListUsersParams = { page_size: 100 };

interface AcikRizaClientProps {
  initialConsents: ConsentList;
  initialUsers: UserList;
  canManage: boolean;
}

export function AcikRizaClient({
  initialConsents,
  initialUsers,
  canManage,
}: AcikRizaClientProps): JSX.Element {
  const t = useTranslations("kvkk.acikRiza");
  const qc = useQueryClient();

  const [search, setSearch] = useState("");
  const [recordDialog, setRecordDialog] = useState<User | null>(null);
  const [recordSignedAt, setRecordSignedAt] = useState("");

  // Consents + users live queries (initial data from server).
  const { data: consents = initialConsents } = useQuery({
    queryKey: kvkkKeys.consents("dlp"),
    queryFn: () => listConsents("dlp"),
    initialData: initialConsents,
  });

  const { data: users = initialUsers } = useQuery({
    queryKey: userKeys.list(USERS_PARAMS),
    queryFn: () => listUsers(USERS_PARAMS),
    initialData: initialUsers,
  });

  // Active consents (not revoked) indexed by user id.
  const activeByUser = useMemo(() => {
    const map = new Map<string, ConsentRecord>();
    for (const item of consents.items) {
      if (item.consent_type !== "dlp") continue;
      if (item.revoked_at) continue;
      map.set(item.user_id, item);
    }
    return map;
  }, [consents]);

  const totalUsers = users.items.length;
  const signedCount = users.items.filter((u) => activeByUser.has(u.id)).length;
  const pendingCount = totalUsers - signedCount;

  const filteredUsers = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return users.items;
    return users.items.filter(
      (u) =>
        u.username.toLowerCase().includes(q) ||
        u.email.toLowerCase().includes(q),
    );
  }, [users, search]);

  // ── Mutations ──────────────────────────────────────────────────────
  const recordMutation = useMutation({
    mutationFn: (args: { userId: string; signedAt: string }) =>
      recordConsent({
        user_id: args.userId,
        consent_type: "dlp",
        signed_at: `${args.signedAt}T00:00:00Z`,
        // Server backends that accept document_base64 tolerate empty
        // string as "no PDF attached (operator will upload via bulk
        // route later)". If the backend rejects empty, the toast
        // surfaces the validation message.
        document_base64: "",
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: kvkkKeys.consents("dlp") });
      toast.success("Açık rıza kaydı oluşturuldu.");
      setRecordDialog(null);
      setRecordSignedAt("");
    },
    onError: (err) => {
      const ufe = toUserFacingError(err);
      toast.error(ufe.title, { description: ufe.description });
    },
  });

  const revokeMutation = useMutation({
    mutationFn: (userId: string) => revokeConsent(userId, "dlp"),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: kvkkKeys.consents("dlp") });
      toast.success("Açık rıza iptal edildi.");
    },
    onError: (err) => {
      const ufe = toUserFacingError(err);
      toast.error(ufe.title, { description: ufe.description });
    },
  });

  return (
    <div className="space-y-6">
      {/* ── Summary cards ─────────────────────────────────────────── */}
      <div className="grid gap-4 md:grid-cols-3">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-xs font-medium text-muted-foreground">
              {t("signedCount")}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-green-600">
              {signedCount}
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-xs font-medium text-muted-foreground">
              {t("pendingCount")}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-amber-600">
              {pendingCount}
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-xs font-medium text-muted-foreground">
              {t("totalEmployees")}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{totalUsers}</div>
          </CardContent>
        </Card>
      </div>

      {/* ── Actions bar ───────────────────────────────────────────── */}
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex flex-wrap gap-2">
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger asChild>
                <span className="inline-block">
                  <Button variant="outline" disabled>
                    <Download className="mr-2 h-4 w-4" aria-hidden="true" />
                    {t("downloadTemplate")}
                  </Button>
                </span>
              </TooltipTrigger>
              <TooltipContent>Yakında</TooltipContent>
            </Tooltip>
            <Tooltip>
              <TooltipTrigger asChild>
                <span className="inline-block">
                  <Button variant="outline" disabled>
                    <Upload className="mr-2 h-4 w-4" aria-hidden="true" />
                    {t("bulkUpload")}
                  </Button>
                </span>
              </TooltipTrigger>
              <TooltipContent>Yakında</TooltipContent>
            </Tooltip>
          </TooltipProvider>
        </div>
        <div className="w-full max-w-xs">
          <Input
            placeholder={t("searchPlaceholder")}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
        </div>
      </div>

      {/* ── Per-user table ────────────────────────────────────────── */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("perUserTitle")}</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          <table className="w-full text-sm">
            <thead className="border-b bg-muted/30 text-left text-xs uppercase text-muted-foreground">
              <tr>
                <th className="px-4 py-2">Kullanıcı</th>
                <th className="px-4 py-2">E-posta</th>
                <th className="px-4 py-2">Durum</th>
                <th className="px-4 py-2">İmza Tarihi</th>
                <th className="px-4 py-2 text-right">İşlem</th>
              </tr>
            </thead>
            <tbody>
              {filteredUsers.length === 0 ? (
                <tr>
                  <td
                    colSpan={5}
                    className="px-4 py-8 text-center text-muted-foreground"
                  >
                    Kullanıcı bulunamadı.
                  </td>
                </tr>
              ) : (
                filteredUsers.map((user) => {
                  const consent = activeByUser.get(user.id);
                  const signed = Boolean(consent);
                  return (
                    <tr
                      key={user.id}
                      className="border-b hover:bg-muted/30 transition-colors"
                    >
                      <td className="px-4 py-3 font-medium">{user.username}</td>
                      <td className="px-4 py-3 text-muted-foreground">
                        {user.email}
                      </td>
                      <td className="px-4 py-3">
                        {signed ? (
                          <Badge variant="success">
                            <CheckCircle2
                              className="mr-1 h-3 w-3"
                              aria-hidden="true"
                            />
                            {t("signedCount")}
                          </Badge>
                        ) : (
                          <Badge variant="warning">
                            <Clock
                              className="mr-1 h-3 w-3"
                              aria-hidden="true"
                            />
                            {t("pendingCount")}
                          </Badge>
                        )}
                      </td>
                      <td className="px-4 py-3 text-xs text-muted-foreground">
                        {consent
                          ? formatDateTR(consent.signed_at, "d MMM yyyy")
                          : "—"}
                      </td>
                      <td className="px-4 py-3 text-right">
                        {canManage && (
                          <DropdownMenu>
                            <DropdownMenuTrigger asChild>
                              <Button variant="ghost" size="icon">
                                <MoreVertical
                                  className="h-4 w-4"
                                  aria-hidden="true"
                                />
                              </Button>
                            </DropdownMenuTrigger>
                            <DropdownMenuContent align="end">
                              <DropdownMenuItem
                                onClick={() => {
                                  setRecordDialog(user);
                                  setRecordSignedAt(
                                    new Date().toISOString().slice(0, 10),
                                  );
                                }}
                              >
                                <FilePlus2
                                  className="mr-2 h-4 w-4"
                                  aria-hidden="true"
                                />
                                {signed ? "Yeniden Kaydet" : "Kaydet"}
                              </DropdownMenuItem>
                              {signed && (
                                <DropdownMenuItem
                                  onClick={() =>
                                    revokeMutation.mutate(user.id)
                                  }
                                >
                                  <Undo2
                                    className="mr-2 h-4 w-4"
                                    aria-hidden="true"
                                  />
                                  İptal Et
                                </DropdownMenuItem>
                              )}
                            </DropdownMenuContent>
                          </DropdownMenu>
                        )}
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </CardContent>
      </Card>

      {/* ── Record dialog ─────────────────────────────────────────── */}
      <Dialog
        open={recordDialog !== null}
        onOpenChange={(open) => {
          if (!open) {
            setRecordDialog(null);
            setRecordSignedAt("");
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Açık Rıza Kaydet</DialogTitle>
            <DialogDescription>
              {recordDialog?.username} ({recordDialog?.email}) için DLP açık
              rıza kaydı oluşturulacak.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-2 py-2">
            <label
              htmlFor="record-signed-at"
              className="text-sm font-medium"
            >
              İmza Tarihi
            </label>
            <Input
              id="record-signed-at"
              type="date"
              value={recordSignedAt}
              onChange={(e) => setRecordSignedAt(e.target.value)}
            />
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => {
                setRecordDialog(null);
                setRecordSignedAt("");
              }}
              disabled={recordMutation.isPending}
            >
              İptal
            </Button>
            <Button
              onClick={() => {
                if (!recordDialog || !recordSignedAt) return;
                recordMutation.mutate({
                  userId: recordDialog.id,
                  signedAt: recordSignedAt,
                });
              }}
              disabled={
                recordMutation.isPending || !recordSignedAt || !recordDialog
              }
            >
              Kaydet
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
