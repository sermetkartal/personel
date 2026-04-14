"use client";

import { useState } from "react";
import { useTranslations } from "next-intl";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  Upload,
  Download,
  Plus,
  X,
  CheckCircle2,
  Clock,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import {
  uploadDpa,
  kvkkKeys,
  type DpaInfo,
  type DpaSignatory,
} from "@/lib/api/kvkk";
import { toUserFacingError } from "@/lib/errors";
import { formatDateTR } from "@/lib/utils";

interface DpaFormProps {
  initial: DpaInfo | null;
  canUpload: boolean;
}

const MAX_PDF_SIZE = 10 * 1024 * 1024; // 10 MB

function toDateInput(value: string | undefined | null): string {
  if (!value) return "";
  const idx = value.indexOf("T");
  return idx > 0 ? value.slice(0, idx) : value.slice(0, 10);
}

function toRfc3339(dateInput: string): string {
  if (!dateInput) return "";
  return `${dateInput}T00:00:00Z`;
}

interface DraftSignatory {
  name: string;
  role: string;
  organization: string;
  signedAt: string;
}

function emptySignatory(): DraftSignatory {
  return { name: "", role: "", organization: "", signedAt: "" };
}

export function DpaForm({ initial, canUpload }: DpaFormProps): JSX.Element {
  const t = useTranslations("kvkk.dpa");
  const qc = useQueryClient();

  const [file, setFile] = useState<File | null>(null);
  const [signedAt, setSignedAt] = useState("");
  const [signatories, setSignatories] = useState<DraftSignatory[]>([
    emptySignatory(),
  ]);
  const [fileError, setFileError] = useState<string | null>(null);

  const isSigned = Boolean(initial?.document_key);

  const mutation = useMutation({
    mutationFn: () => {
      if (!file) throw new Error("file_required");
      const mapped: DpaSignatory[] = signatories
        .filter((s) => s.name.trim() !== "")
        .map((s) => ({
          name: s.name.trim(),
          role: s.role.trim(),
          organization: s.organization.trim(),
          signed_at: s.signedAt ? toRfc3339(s.signedAt) : toRfc3339(signedAt),
        }));
      return uploadDpa({
        file,
        signed_at: toRfc3339(signedAt),
        signatories: mapped,
      });
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: kvkkKeys.dpa });
      toast.success(t("uploadSuccess"));
      setFile(null);
    },
    onError: (err) => {
      const ufe = toUserFacingError(err);
      toast.error(t("uploadError"), { description: ufe.description });
    },
  });

  function handleFileChange(e: React.ChangeEvent<HTMLInputElement>): void {
    setFileError(null);
    const picked = e.target.files?.[0] ?? null;
    if (!picked) {
      setFile(null);
      return;
    }
    if (picked.type !== "application/pdf") {
      setFileError("Sadece PDF kabul edilir.");
      setFile(null);
      return;
    }
    if (picked.size > MAX_PDF_SIZE) {
      setFileError("Dosya 10 MB sınırını aşıyor.");
      setFile(null);
      return;
    }
    setFile(picked);
  }

  function updateSignatory(
    idx: number,
    field: keyof DraftSignatory,
    value: string,
  ): void {
    setSignatories((prev) =>
      prev.map((s, i) => (i === idx ? { ...s, [field]: value } : s)),
    );
  }

  function addSignatory(): void {
    setSignatories((prev) => [...prev, emptySignatory()]);
  }

  function removeSignatory(idx: number): void {
    setSignatories((prev) => prev.filter((_, i) => i !== idx));
  }

  function handleSubmit(e: React.FormEvent): void {
    e.preventDefault();
    if (!canUpload || !file || !signedAt) return;
    mutation.mutate();
  }

  return (
    <div className="space-y-6">
      {/* Current status */}
      <div className="flex flex-wrap items-center gap-3">
        {isSigned ? (
          <Badge variant="success">
            <CheckCircle2 className="mr-1 h-3 w-3" aria-hidden="true" />
            {t("statusSigned")}
          </Badge>
        ) : (
          <Badge variant="warning">
            <Clock className="mr-1 h-3 w-3" aria-hidden="true" />
            {t("statusPending")}
          </Badge>
        )}
        {initial?.signed_at && (
          <span className="text-xs text-muted-foreground">
            {t("signedAtLabel")}:{" "}
            {formatDateTR(initial.signed_at, "d MMM yyyy")}
          </span>
        )}
        {initial && initial.signatories.length > 0 && (
          <span className="text-xs text-muted-foreground">
            {t("signatoriesLabel")}: {initial.signatories.length}
          </span>
        )}
      </div>

      {/* Download template (placeholder) */}
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
      </TooltipProvider>

      {canUpload && (
        <form onSubmit={handleSubmit} className="space-y-4 border-t pt-4">
          <div className="grid gap-4 md:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="dpa-file">{t("upload")}</Label>
              <Input
                id="dpa-file"
                type="file"
                accept="application/pdf"
                onChange={handleFileChange}
                disabled={mutation.isPending}
              />
              {file && (
                <p className="text-[11px] text-muted-foreground">
                  {file.name} ({(file.size / 1024).toFixed(0)} KB)
                </p>
              )}
              {fileError && (
                <p className="text-xs text-destructive" role="alert">
                  {fileError}
                </p>
              )}
            </div>

            <div className="space-y-2">
              <Label htmlFor="dpa-signed-at">{t("signedAtLabel")}</Label>
              <Input
                id="dpa-signed-at"
                type="date"
                value={signedAt}
                onChange={(e) => setSignedAt(e.target.value)}
                disabled={mutation.isPending}
              />
            </div>
          </div>

          {/* Signatories — dynamic rows */}
          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <Label>{t("signatoriesLabel")}</Label>
              <Button
                type="button"
                size="sm"
                variant="ghost"
                onClick={addSignatory}
                disabled={mutation.isPending}
              >
                <Plus className="mr-1 h-3 w-3" aria-hidden="true" />
                Satır Ekle
              </Button>
            </div>
            <div className="space-y-2">
              {signatories.map((s, idx) => (
                <div
                  key={idx}
                  className="grid gap-2 rounded-md border p-3 md:grid-cols-[1fr_1fr_1fr_140px_auto]"
                >
                  <Input
                    placeholder="Ad Soyad"
                    value={s.name}
                    onChange={(e) =>
                      updateSignatory(idx, "name", e.target.value)
                    }
                    disabled={mutation.isPending}
                  />
                  <Input
                    placeholder="Unvan (CEO, DPO)"
                    value={s.role}
                    onChange={(e) =>
                      updateSignatory(idx, "role", e.target.value)
                    }
                    disabled={mutation.isPending}
                  />
                  <Input
                    placeholder="Kurum"
                    value={s.organization}
                    onChange={(e) =>
                      updateSignatory(idx, "organization", e.target.value)
                    }
                    disabled={mutation.isPending}
                  />
                  <Input
                    type="date"
                    value={s.signedAt}
                    onChange={(e) =>
                      updateSignatory(idx, "signedAt", e.target.value)
                    }
                    disabled={mutation.isPending}
                  />
                  {signatories.length > 1 && (
                    <Button
                      type="button"
                      size="icon"
                      variant="ghost"
                      onClick={() => removeSignatory(idx)}
                      disabled={mutation.isPending}
                      aria-label="Sil"
                    >
                      <X className="h-4 w-4" aria-hidden="true" />
                    </Button>
                  )}
                </div>
              ))}
            </div>
            <p className="text-[11px] text-muted-foreground">
              {t("signatoriesPlaceholder")}
            </p>
          </div>

          <div className="flex justify-end">
            <Button
              type="submit"
              disabled={
                mutation.isPending || !file || !signedAt || Boolean(fileError)
              }
            >
              <Upload className="mr-2 h-4 w-4" aria-hidden="true" />
              {t("upload")}
            </Button>
          </div>
        </form>
      )}

      {/* Existing signatories listing */}
      {initial && initial.signatories.length > 0 && (
        <div className="rounded-md border">
          <div className="border-b bg-muted/30 px-4 py-2 text-xs font-semibold">
            {t("signatoriesLabel")}
          </div>
          <ul className="divide-y">
            {initial.signatories.map((s, idx) => (
              <li
                key={idx}
                className="flex items-center justify-between px-4 py-2 text-sm"
              >
                <div>
                  <span className="font-medium">{s.name}</span>
                  <span className="ml-2 text-muted-foreground">
                    {s.role}
                    {s.organization ? ` — ${s.organization}` : ""}
                  </span>
                </div>
                <span className="text-xs text-muted-foreground">
                  {formatDateTR(s.signed_at, "d MMM yyyy")}
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}
