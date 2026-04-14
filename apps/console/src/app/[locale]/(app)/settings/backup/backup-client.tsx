"use client";

import { useState } from "react";
import { useTranslations } from "next-intl";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  HardDrive,
  Cloud,
  Plus,
  Play,
  Trash2,
  Loader2,
  CheckCircle2,
  XCircle,
  Clock,
  ChevronDown,
  ChevronUp,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

import {
  listBackupTargets,
  createBackupTarget,
  updateBackupTarget,
  deleteBackupTarget,
  triggerBackupRun,
  listBackupRuns,
  settingsKeys,
  BACKUP_KINDS,
  BACKUP_SCHEMAS,
  type BackupKind,
  type BackupTarget,
  type CreateTargetRequest,
} from "@/lib/api/settings-extended";
import { toUserFacingError } from "@/lib/errors";

interface Props {
  token?: string;
}

export function BackupClient({ token }: Props): JSX.Element {
  const t = useTranslations("settings.backup");
  const qc = useQueryClient();

  const query = useQuery({
    queryKey: settingsKeys.backupTargets,
    queryFn: ({ signal }) => listBackupTargets({ token, signal }),
  });

  const [addOpen, setAddOpen] = useState(false);

  if (query.isLoading) {
    return (
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" />
        {t("loading")}
      </div>
    );
  }

  const all = query.data?.items ?? [];
  const inSite = all.find((x) => x.kind === "in_site_local") ?? null;
  const offsite = all.filter((x) => x.kind !== "in_site_local");

  function invalidate(): void {
    void qc.invalidateQueries({ queryKey: settingsKeys.backupTargets });
  }

  return (
    <div className="space-y-6">
      {/* ── In-site section ───────────────────────────────────────────── */}
      <section className="space-y-3">
        <div className="flex items-center gap-2">
          <HardDrive className="h-5 w-5 text-muted-foreground" />
          <h3 className="text-lg font-semibold">{t("inSite.title")}</h3>
        </div>
        <p className="text-sm text-muted-foreground">{t("inSite.hint")}</p>

        {inSite ? (
          <InSiteCard target={inSite} token={token} onChanged={invalidate} />
        ) : (
          <Card>
            <CardContent className="pt-6 text-sm text-muted-foreground">
              {t("inSite.notConfigured")}{" "}
              <Button
                variant="link"
                size="sm"
                className="h-auto p-0"
                onClick={() => setAddOpen(true)}
              >
                {t("inSite.addNow")}
              </Button>
            </CardContent>
          </Card>
        )}
      </section>

      {/* ── Off-site section ──────────────────────────────────────────── */}
      <section className="space-y-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Cloud className="h-5 w-5 text-muted-foreground" />
            <h3 className="text-lg font-semibold">{t("offsite.title")}</h3>
          </div>
          <Button size="sm" onClick={() => setAddOpen(true)}>
            <Plus className="mr-1.5 h-3 w-3" />
            {t("offsite.add")}
          </Button>
        </div>
        <p className="text-sm text-muted-foreground">{t("offsite.hint")}</p>

        {offsite.length === 0 ? (
          <Card>
            <CardContent className="pt-6 text-sm text-muted-foreground">
              {t("offsite.empty")}
            </CardContent>
          </Card>
        ) : (
          <div className="space-y-2">
            {offsite.map((target) => (
              <TargetRow
                key={target.id}
                target={target}
                token={token}
                onChanged={invalidate}
              />
            ))}
          </div>
        )}
      </section>

      <AddTargetDialog
        open={addOpen}
        onOpenChange={setAddOpen}
        token={token}
        onCreated={invalidate}
      />
    </div>
  );
}

// ────────────────────────────────────────────────────────────────────────────
// In-site card
// ────────────────────────────────────────────────────────────────────────────

function InSiteCard({
  target,
  token,
  onChanged,
}: {
  target: BackupTarget;
  token?: string;
  onChanged: () => void;
}): JSX.Element {
  const t = useTranslations("settings.backup");

  const toggle = useMutation({
    mutationFn: (next: boolean) =>
      updateBackupTarget(target.id, { enabled: next }, { token }),
    onSuccess: () => {
      toast.success(t("inSite.toggleSuccess"));
      onChanged();
    },
    onError: (err) => {
      const ufe = toUserFacingError(err);
      toast.error(t("inSite.toggleError"), { description: ufe.description });
    },
  });

  const run = useMutation({
    mutationFn: () =>
      triggerBackupRun(target.id, { kind: target.kind }, { token }),
    onSuccess: () => {
      toast.success(t("run.success"));
      onChanged();
    },
    onError: (err) => {
      const ufe = toUserFacingError(err);
      toast.error(t("run.error"), { description: ufe.description });
    },
  });

  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between space-y-0 pb-3">
        <div>
          <CardTitle className="text-sm">{target.name}</CardTitle>
          <p className="mt-1 font-mono text-xs text-muted-foreground">
            {target.config.path ?? "—"}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Switch
            checked={target.enabled}
            onCheckedChange={(v) => toggle.mutate(v)}
          />
          <Label className="text-xs">{t("inSite.enabled")}</Label>
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="grid gap-4 text-xs text-muted-foreground md:grid-cols-3">
          <Field label={t("retentionDays")} value={`${target.retention_days}`} />
          <Field
            label={t("lastRun")}
            value={target.last_run_at ?? t("neverRun")}
          />
          <Field
            label={t("lastStatus")}
            value={<StatusBadge status={target.last_run_status} />}
          />
        </div>
        <div className="flex gap-2">
          <Button
            size="sm"
            onClick={() => run.mutate()}
            disabled={run.isPending}
          >
            {run.isPending ? (
              <Loader2 className="mr-1.5 h-3 w-3 animate-spin" />
            ) : (
              <Play className="mr-1.5 h-3 w-3" />
            )}
            {t("runNow")}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}

// ────────────────────────────────────────────────────────────────────────────
// Off-site target row (with expandable runs history)
// ────────────────────────────────────────────────────────────────────────────

function TargetRow({
  target,
  token,
  onChanged,
}: {
  target: BackupTarget;
  token?: string;
  onChanged: () => void;
}): JSX.Element {
  const t = useTranslations("settings.backup");
  const [expanded, setExpanded] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);

  const toggle = useMutation({
    mutationFn: (next: boolean) =>
      updateBackupTarget(target.id, { enabled: next }, { token }),
    onSuccess: () => {
      toast.success(t("target.toggleSuccess"));
      onChanged();
    },
    onError: (err) => {
      const ufe = toUserFacingError(err);
      toast.error(t("target.toggleError"), { description: ufe.description });
    },
  });

  const run = useMutation({
    mutationFn: () =>
      triggerBackupRun(target.id, { kind: target.kind }, { token }),
    onSuccess: () => {
      toast.success(t("run.success"));
      onChanged();
    },
    onError: (err) => {
      const ufe = toUserFacingError(err);
      toast.error(t("run.error"), { description: ufe.description });
    },
  });

  const del = useMutation({
    mutationFn: () => deleteBackupTarget(target.id, { token }),
    onSuccess: () => {
      toast.success(t("target.deleteSuccess"));
      setConfirmDelete(false);
      onChanged();
    },
    onError: (err) => {
      const ufe = toUserFacingError(err);
      toast.error(t("target.deleteError"), { description: ufe.description });
    },
  });

  return (
    <Card>
      <CardContent className="space-y-3 pt-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="flex-1">
            <div className="flex items-center gap-2">
              <p className="text-sm font-semibold">{target.name}</p>
              <Badge variant="outline" className="text-[10px]">
                {t(`kind.${target.kind}`)}
              </Badge>
              <StatusBadge status={target.last_run_status} />
            </div>
            <p className="mt-1 text-xs text-muted-foreground">
              {t("retentionDays")}: {target.retention_days} ·{" "}
              {t("lastRun")}:{" "}
              {target.last_run_at ?? t("neverRun")}
            </p>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <div className="flex items-center gap-1.5">
              <Switch
                checked={target.enabled}
                onCheckedChange={(v) => toggle.mutate(v)}
              />
            </div>
            <Button
              size="sm"
              variant="outline"
              onClick={() => run.mutate()}
              disabled={run.isPending}
            >
              {run.isPending ? (
                <Loader2 className="mr-1.5 h-3 w-3 animate-spin" />
              ) : (
                <Play className="mr-1.5 h-3 w-3" />
              )}
              {t("runNow")}
            </Button>
            <Button
              size="sm"
              variant="outline"
              onClick={() => setExpanded((x) => !x)}
            >
              {expanded ? (
                <ChevronUp className="mr-1.5 h-3 w-3" />
              ) : (
                <ChevronDown className="mr-1.5 h-3 w-3" />
              )}
              {t("history")}
            </Button>
            <Button
              size="sm"
              variant="outline"
              className="text-destructive hover:text-destructive"
              onClick={() => setConfirmDelete(true)}
            >
              <Trash2 className="h-3 w-3" />
            </Button>
          </div>
        </div>

        {expanded && <RunsHistory targetId={target.id} token={token} />}
      </CardContent>

      <Dialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {t("target.deleteConfirmTitle", { name: target.name })}
            </DialogTitle>
            <DialogDescription>{t("target.deleteConfirmBody")}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setConfirmDelete(false)}
              disabled={del.isPending}
            >
              {t("cancel")}
            </Button>
            <Button
              variant="destructive"
              onClick={() => del.mutate()}
              disabled={del.isPending}
            >
              {del.isPending ? (
                <Loader2 className="mr-1.5 h-3 w-3 animate-spin" />
              ) : (
                <Trash2 className="mr-1.5 h-3 w-3" />
              )}
              {t("deleteConfirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  );
}

// ────────────────────────────────────────────────────────────────────────────
// Runs history accordion
// ────────────────────────────────────────────────────────────────────────────

function RunsHistory({
  targetId,
  token,
}: {
  targetId: string;
  token?: string;
}): JSX.Element {
  const t = useTranslations("settings.backup");
  const query = useQuery({
    queryKey: settingsKeys.backupRuns(targetId),
    queryFn: ({ signal }) => listBackupRuns(targetId, { token, signal }),
  });

  if (query.isLoading) {
    return (
      <div className="flex items-center gap-2 py-4 text-xs text-muted-foreground">
        <Loader2 className="h-3 w-3 animate-spin" />
        {t("loadingRuns")}
      </div>
    );
  }

  const runs = query.data?.items ?? [];
  if (runs.length === 0) {
    return (
      <p className="py-4 text-xs text-muted-foreground">{t("noRuns")}</p>
    );
  }

  return (
    <div className="rounded border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t("startedAt")}</TableHead>
            <TableHead>{t("finishedAt")}</TableHead>
            <TableHead>{t("status")}</TableHead>
            <TableHead>{t("size")}</TableHead>
            <TableHead>{t("sha")}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {runs.map((r) => (
            <TableRow key={r.id}>
              <TableCell className="text-xs">{r.started_at}</TableCell>
              <TableCell className="text-xs">{r.finished_at ?? "—"}</TableCell>
              <TableCell>
                <StatusBadge status={r.status} />
              </TableCell>
              <TableCell className="text-xs">
                {formatBytes(r.size_bytes)}
              </TableCell>
              <TableCell className="font-mono text-[10px]">
                {r.sha256 ? r.sha256.slice(0, 12) + "…" : "—"}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

// ────────────────────────────────────────────────────────────────────────────
// Add target dialog
// ────────────────────────────────────────────────────────────────────────────

function AddTargetDialog({
  open,
  onOpenChange,
  token,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  token?: string;
  onCreated: () => void;
}): JSX.Element {
  const t = useTranslations("settings.backup");

  const [name, setName] = useState("");
  const [kind, setKind] = useState<BackupKind>("offsite_s3");
  const [config, setConfig] = useState<Record<string, string>>({});
  const [retentionDays, setRetentionDays] = useState(30);
  const [enabled, setEnabled] = useState(true);

  const create = useMutation({
    mutationFn: (payload: CreateTargetRequest) =>
      createBackupTarget(payload, { token }),
    onSuccess: () => {
      toast.success(t("add.success"));
      onCreated();
      onOpenChange(false);
      // reset
      setName("");
      setKind("offsite_s3");
      setConfig({});
      setRetentionDays(30);
      setEnabled(true);
    },
    onError: (err) => {
      const ufe = toUserFacingError(err);
      toast.error(t("add.error"), { description: ufe.description });
    },
  });

  const schema = BACKUP_SCHEMAS[kind];

  function handleSubmit(e: React.FormEvent): void {
    e.preventDefault();
    if (!name.trim()) return;
    create.mutate({
      name: name.trim(),
      kind,
      enabled,
      config,
      retention_days: retentionDays,
    });
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-xl max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{t("add.title")}</DialogTitle>
          <DialogDescription>{t("add.description")}</DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="target-name">{t("add.name")}</Label>
            <Input
              id="target-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t("add.namePlaceholder")}
              required
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="target-kind">{t("add.kind")}</Label>
            <Select
              value={kind}
              onValueChange={(v) => {
                setKind(v as BackupKind);
                setConfig({});
              }}
            >
              <SelectTrigger id="target-kind">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {BACKUP_KINDS.map((k) => (
                  <SelectItem key={k} value={k}>
                    {t(`kind.${k}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="space-y-3 rounded-md border bg-muted/20 p-3">
            <p className="text-xs font-semibold text-muted-foreground">
              {t("add.configHeader")}
            </p>
            {schema.map((f) => {
              const id = `cfg-${f.key}`;
              if (f.multiline) {
                return (
                  <div key={f.key} className="space-y-1.5">
                    <Label htmlFor={id} className="text-xs">
                      {f.label}
                    </Label>
                    <Textarea
                      id={id}
                      rows={4}
                      value={config[f.key] ?? ""}
                      onChange={(e) =>
                        setConfig((c) => ({ ...c, [f.key]: e.target.value }))
                      }
                      placeholder={f.placeholder}
                      className="font-mono text-xs"
                    />
                  </div>
                );
              }
              return (
                <div key={f.key} className="space-y-1.5">
                  <Label htmlFor={id} className="text-xs">
                    {f.label}
                  </Label>
                  <Input
                    id={id}
                    type={f.password ? "password" : "text"}
                    value={config[f.key] ?? ""}
                    onChange={(e) =>
                      setConfig((c) => ({ ...c, [f.key]: e.target.value }))
                    }
                    placeholder={f.placeholder}
                    autoComplete="off"
                  />
                </div>
              );
            })}
          </div>

          <div className="grid gap-4 md:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="ret-days">{t("add.retention")}</Label>
              <Input
                id="ret-days"
                type="number"
                min={1}
                value={retentionDays}
                onChange={(e) => setRetentionDays(Number(e.target.value) || 0)}
              />
            </div>
            <div className="flex items-end gap-2 pb-2">
              <Switch checked={enabled} onCheckedChange={setEnabled} />
              <Label className="text-xs">{t("add.enabled")}</Label>
            </div>
          </div>

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
              disabled={create.isPending}
            >
              {t("cancel")}
            </Button>
            <Button type="submit" disabled={create.isPending}>
              {create.isPending ? (
                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              ) : (
                <Plus className="mr-2 h-4 w-4" />
              )}
              {t("add.submit")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// ────────────────────────────────────────────────────────────────────────────
// Helpers
// ────────────────────────────────────────────────────────────────────────────

function Field({
  label,
  value,
}: {
  label: string;
  value: React.ReactNode;
}): JSX.Element {
  return (
    <div>
      <p className="text-[10px] uppercase tracking-wide">{label}</p>
      <div className="mt-0.5 text-foreground">{value}</div>
    </div>
  );
}

function StatusBadge({
  status,
}: {
  status: "success" | "failure" | "running" | null;
}): JSX.Element {
  if (status === "success") {
    return (
      <Badge variant="success">
        <CheckCircle2 className="mr-1 h-3 w-3" />
        OK
      </Badge>
    );
  }
  if (status === "failure") {
    return (
      <Badge variant="destructive">
        <XCircle className="mr-1 h-3 w-3" />
        FAIL
      </Badge>
    );
  }
  if (status === "running") {
    return (
      <Badge variant="warning">
        <Clock className="mr-1 h-3 w-3" />
        RUN
      </Badge>
    );
  }
  return <Badge variant="outline">—</Badge>;
}

function formatBytes(bytes: number): string {
  if (!bytes) return "—";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let n = bytes;
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i++;
  }
  return `${n.toFixed(1)} ${units[i]}`;
}
