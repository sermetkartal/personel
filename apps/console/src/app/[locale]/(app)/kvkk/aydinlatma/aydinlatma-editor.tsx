"use client";

import { useState } from "react";
import { useTranslations } from "next-intl";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Send, Eye, Edit3 } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import {
  publishAydinlatma,
  kvkkKeys,
  type AydinlatmaInfo,
} from "@/lib/api/kvkk";
import { toUserFacingError } from "@/lib/errors";
import { formatDateTR } from "@/lib/utils";

interface AydinlatmaEditorProps {
  initial: AydinlatmaInfo | null;
  canPublish: boolean;
}

/**
 * Tiny Markdown → HTML fallback renderer.
 *
 * The console deliberately does NOT carry `react-markdown` as a dependency
 * for this Sprint 2C deliverable — the preview only needs to show headings,
 * paragraphs, and basic emphasis so the editor can see "it parses" not
 * "it renders pixel-perfect". This is intentionally naive: it escapes HTML
 * first, then applies a handful of regex passes. Anything more sophisticated
 * (tables, fenced code, links) falls back to the escaped text and is still
 * legible.
 *
 * If/when a richer renderer is wanted, swap this for `react-markdown` +
 * `remark-gfm`; the editor surface stays identical.
 */
function renderMarkdownNaive(src: string): string {
  // 1. Escape HTML.
  let html = src
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");

  // 2. Headings ###/##/# (line-anchored, run before bold/italic).
  html = html.replace(/^######\s+(.*)$/gm, "<h6>$1</h6>");
  html = html.replace(/^#####\s+(.*)$/gm, "<h5>$1</h5>");
  html = html.replace(/^####\s+(.*)$/gm, "<h4>$1</h4>");
  html = html.replace(/^###\s+(.*)$/gm, "<h3>$1</h3>");
  html = html.replace(/^##\s+(.*)$/gm, "<h2>$1</h2>");
  html = html.replace(/^#\s+(.*)$/gm, "<h1>$1</h1>");

  // 3. Bold/italic — bold first so it consumes double-star before italic.
  html = html.replace(/\*\*(.+?)\*\*/g, "<strong>$1</strong>");
  html = html.replace(/\*(.+?)\*/g, "<em>$1</em>");

  // 4. Unordered list: consecutive `- ` lines become <ul>.
  html = html.replace(
    /(^|\n)((?:- .*(?:\n|$))+)/g,
    (_m, pre: string, block: string) => {
      const items = block
        .trim()
        .split("\n")
        .map((line) => line.replace(/^-\s+/, ""))
        .map((line) => `<li>${line}</li>`)
        .join("");
      return `${pre}<ul>${items}</ul>`;
    },
  );

  // 5. Paragraph breaks: double newline → </p><p>, single newline → <br>.
  const paras = html.split(/\n{2,}/).map((chunk) => {
    // Skip chunks that already begin with a block tag.
    if (/^\s*<(h\d|ul|ol|p|blockquote)/.test(chunk)) return chunk;
    return `<p>${chunk.replace(/\n/g, "<br/>")}</p>`;
  });

  return paras.join("\n");
}

export function AydinlatmaEditor({
  initial,
  canPublish,
}: AydinlatmaEditorProps): JSX.Element {
  const t = useTranslations("kvkk.aydinlatma");
  const qc = useQueryClient();

  const [markdown, setMarkdown] = useState(initial?.markdown ?? "");
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [tab, setTab] = useState<"edit" | "preview">("edit");

  const mutation = useMutation({
    mutationFn: () => publishAydinlatma({ markdown }),
    onSuccess: (info) => {
      void qc.invalidateQueries({ queryKey: kvkkKeys.aydinlatma });
      setConfirmOpen(false);
      toast.success(t("publishSuccess", { version: info.version }));
    },
    onError: (err) => {
      const ufe = toUserFacingError(err);
      toast.error(t("publishError"), { description: ufe.description });
    },
  });

  const publishedAt = initial?.published_at
    ? formatDateTR(initial.published_at, "d MMM yyyy HH:mm")
    : "—";
  const version = initial?.version ?? 0;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3">
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <span className="font-medium">{t("lastPublishedAt")}:</span>
          <span>{publishedAt}</span>
        </div>
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <span className="font-medium">{t("version")}:</span>
          <Badge variant="outline">v{version}</Badge>
        </div>
      </div>

      <Tabs value={tab} onValueChange={(v) => setTab(v as "edit" | "preview")}>
        <TabsList>
          <TabsTrigger value="edit">
            <Edit3 className="mr-2 h-4 w-4" aria-hidden="true" />
            {t("editorLabel")}
          </TabsTrigger>
          <TabsTrigger value="preview">
            <Eye className="mr-2 h-4 w-4" aria-hidden="true" />
            {t("preview")}
          </TabsTrigger>
        </TabsList>
        <TabsContent value="edit" className="mt-3">
          <Textarea
            value={markdown}
            onChange={(e) => setMarkdown(e.target.value)}
            disabled={!canPublish || mutation.isPending}
            rows={20}
            className="font-mono text-xs"
            placeholder="# KVKK m.10 Aydınlatma Metni&#10;&#10;..."
          />
        </TabsContent>
        <TabsContent value="preview" className="mt-3">
          <div
            className="prose prose-sm dark:prose-invert max-w-none rounded-md border bg-muted/20 p-4"
            // Naive renderer output; source is operator-authored and the
            // HTML is escaped before regex replacement, so this is safe.
            dangerouslySetInnerHTML={{ __html: renderMarkdownNaive(markdown) }}
          />
        </TabsContent>
      </Tabs>

      {canPublish && (
        <div className="flex justify-end">
          <Button
            onClick={() => setConfirmOpen(true)}
            disabled={mutation.isPending || markdown.trim().length === 0}
          >
            <Send className="mr-2 h-4 w-4" aria-hidden="true" />
            {t("publish")}
          </Button>
        </div>
      )}

      <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("publish")}</DialogTitle>
            <DialogDescription>{t("publishConfirm")}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setConfirmOpen(false)}
              disabled={mutation.isPending}
            >
              İptal
            </Button>
            <Button
              onClick={() => mutation.mutate()}
              disabled={mutation.isPending}
            >
              {t("publish")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
