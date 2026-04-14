"use client";

import { useState } from "react";
import { useTranslations } from "next-intl";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Upload, Download, CheckCircle2, Clock } from "lucide-react";

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
import { uploadDpia, kvkkKeys, type DpiaInfo } from "@/lib/api/kvkk";
import { toUserFacingError } from "@/lib/errors";
import { formatDateTR } from "@/lib/utils";

interface DpiaFormProps {
  initial: DpiaInfo | null;
  canUpload: boolean;
}

const MAX_PDF_SIZE = 10 * 1024 * 1024; // 10 MB

function toRfc3339(dateInput: string): string {
  if (!dateInput) return "";
  return `${dateInput}T00:00:00Z`;
}

export function DpiaForm({ initial, canUpload }: DpiaFormProps): JSX.Element {
  const t = useTranslations("kvkk.dpia");
  const qc = useQueryClient();

  const [file, setFile] = useState<File | null>(null);
  const [completedAt, setCompletedAt] = useState("");
  const [fileError, setFileError] = useState<string | null>(null);

  const isCompleted = Boolean(initial?.amendment_key);

  const mutation = useMutation({
    mutationFn: () => {
      if (!file) throw new Error("file_required");
      return uploadDpia({ file, completed_at: toRfc3339(completedAt) });
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: kvkkKeys.dpia });
      toast.success("DPIA yüklendi.");
      setFile(null);
    },
    onError: (err) => {
      const ufe = toUserFacingError(err);
      toast.error(ufe.title, { description: ufe.description });
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

  function handleSubmit(e: React.FormEvent): void {
    e.preventDefault();
    if (!canUpload || !file || !completedAt) return;
    mutation.mutate();
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center gap-3">
        {isCompleted ? (
          <Badge variant="success">
            <CheckCircle2 className="mr-1 h-3 w-3" aria-hidden="true" />
            {t("statusCompleted")}
          </Badge>
        ) : (
          <Badge variant="warning">
            <Clock className="mr-1 h-3 w-3" aria-hidden="true" />
            {t("statusPending")}
          </Badge>
        )}
        {initial?.completed_at && (
          <span className="text-xs text-muted-foreground">
            {formatDateTR(initial.completed_at, "d MMM yyyy")}
          </span>
        )}
      </div>

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
              <Label htmlFor="dpia-file">{t("upload")}</Label>
              <Input
                id="dpia-file"
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
              <Label htmlFor="dpia-completed-at">Tamamlanma Tarihi</Label>
              <Input
                id="dpia-completed-at"
                type="date"
                value={completedAt}
                onChange={(e) => setCompletedAt(e.target.value)}
                disabled={mutation.isPending}
              />
            </div>
          </div>

          <div className="flex justify-end">
            <Button
              type="submit"
              disabled={
                mutation.isPending ||
                !file ||
                !completedAt ||
                Boolean(fileError)
              }
            >
              <Upload className="mr-2 h-4 w-4" aria-hidden="true" />
              {t("upload")}
            </Button>
          </div>
        </form>
      )}
    </div>
  );
}
