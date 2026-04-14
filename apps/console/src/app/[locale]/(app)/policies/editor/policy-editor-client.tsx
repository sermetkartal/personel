"use client";

import { useTranslations } from "next-intl";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { useMutation } from "@tanstack/react-query";
import {
  ArrowLeft,
  Plus,
  Trash2,
  Save,
  Send,
  AlertTriangle,
  Shield,
  Ban,
  Folder,
  Link2,
  EyeOff,
  KeySquare,
  CheckCircle2,
  XCircle,
  PlayCircle,
} from "lucide-react";

import type { Policy } from "@/lib/api/types";
import {
  createPolicy,
  emptyRules,
  publishPolicy,
  rulesFromVisual,
  updatePolicy,
  validateGlob,
  validateRegex,
  visualFromRules,
  type VisualRule,
  type VisualRuleKind,
} from "@/lib/api/policy";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";

interface PolicyEditorClientProps {
  locale: string;
  initial: Policy | null;
}

interface RuleKindMeta {
  label: string;
  hint: string;
  icon: React.ElementType;
  placeholder: string;
  validator: (v: string) => string | null;
  tone: "neutral" | "warn" | "danger";
}

const RULE_KIND_META: Record<VisualRuleKind, RuleKindMeta> = {
  app_allowlist: {
    label: "Uygulama İzin Listesi",
    hint: "Çalışanların açıkça 'üretken' sayılacağı uygulamalar",
    icon: CheckCircle2,
    placeholder: "Microsoft Excel",
    validator: (v) => (v.trim() ? null : "empty"),
    tone: "neutral",
  },
  app_distracting: {
    label: "Dikkat Dağıtıcı Uygulamalar",
    hint: "Açıkça 'dikkat dağıtıcı' işaretlenen uygulamalar (engellenmez, raporlanır)",
    icon: Ban,
    placeholder: "Steam",
    validator: (v) => (v.trim() ? null : "empty"),
    tone: "warn",
  },
  path_sensitive: {
    label: "Hassas Dosya Desenleri (KVKK m.6)",
    hint: "Pencere başlığı regex'i — sağlık, sendika, hukuk vb. içerik işareti",
    icon: Folder,
    placeholder: "(?i)(sağlık|tckn|müşteri)",
    validator: validateRegex,
    tone: "danger",
  },
  url_blocklist: {
    label: "URL / Host Engel Listesi",
    hint: "Glob deseni — hassas hedef hostlar",
    icon: Link2,
    placeholder: "*.onlyfans.com",
    validator: validateGlob,
    tone: "warn",
  },
  screenshot_exclude: {
    label: "Ekran Görüntüsü Hariç Tut",
    hint: "Ekran yakalama kesinlikle baskılanacak uygulamalar (parola yöneticileri, bankacılık)",
    icon: EyeOff,
    placeholder: "KeePass",
    validator: (v) => (v.trim() ? null : "empty"),
    tone: "neutral",
  },
  keystroke_dlp_opt_in: {
    label: "Klavye DLP (ADR 0013)",
    hint: "Uyarı: Bu sadece işaretleyici. DLP etkinleştirme Vault töreniyle yapılır, konsol üzerinden değil.",
    icon: KeySquare,
    placeholder: "dpo@firma.com",
    validator: () => null,
    tone: "danger",
  },
};

const PALETTE: VisualRuleKind[] = [
  "app_allowlist",
  "app_distracting",
  "path_sensitive",
  "url_blocklist",
  "screenshot_exclude",
  "keystroke_dlp_opt_in",
];

function newRule(kind: VisualRuleKind): VisualRule {
  return { id: crypto.randomUUID(), kind, values: [] };
}

export function PolicyEditorClient({
  locale,
  initial,
}: PolicyEditorClientProps): JSX.Element {
  const t = useTranslations("policies.editor");
  const router = useRouter();

  const [name, setName] = useState(initial?.name ?? "");
  const [description, setDescription] = useState(initial?.description ?? "");
  const [rules, setRules] = useState<VisualRule[]>(
    initial ? visualFromRules(initial.rules) : [],
  );
  const [publishOpen, setPublishOpen] = useState(false);
  const [dlpWarnOpen, setDlpWarnOpen] = useState(false);

  const projected = useMemo(() => rulesFromVisual(rules), [rules]);

  const saveMutation = useMutation({
    mutationFn: async (publishAfter: boolean) => {
      const payload = {
        name: name.trim(),
        description: description.trim() || undefined,
        rules: projected,
      };
      const saved = initial
        ? await updatePolicy(initial.id, payload)
        : await createPolicy(payload);
      if (publishAfter) {
        await publishPolicy(saved.id);
      }
      return saved;
    },
    onSuccess: (_saved, publishAfter) => {
      if (publishAfter) {
        toast.success(t("publishSuccess"));
        setPublishOpen(false);
        router.push(`/${locale}/policies`);
      } else {
        toast.success(t("saveDraftSuccess"));
      }
    },
    onError: (err: unknown) => {
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(`${t("saveError")}: ${msg}`);
    },
  });

  const canSave = name.trim().length > 0 && !saveMutation.isPending;

  function addRule(kind: VisualRuleKind): void {
    if (kind === "keystroke_dlp_opt_in") {
      setDlpWarnOpen(true);
      return;
    }
    setRules((prev) => [...prev, newRule(kind)]);
  }

  function removeRule(id: string): void {
    setRules((prev) => prev.filter((r) => r.id !== id));
  }

  function updateRuleValues(id: string, values: string[]): void {
    setRules((prev) =>
      prev.map((r) => (r.id === id ? { ...r, values } : r)),
    );
  }

  return (
    <div className="space-y-6 animate-fade-in">
      {/* Top bar */}
      <div className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
        <div className="space-y-1">
          <Link
            href={`/${locale}/policies`}
            className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
          >
            <ArrowLeft className="h-4 w-4" aria-hidden="true" />
            {t("backToList")}
          </Link>
          <h1 className="text-2xl font-bold tracking-tight">
            {initial ? t("editTitle") : t("newTitle")}
          </h1>
          <p className="text-muted-foreground text-sm">
            {t("subtitle")}
            {initial && (
              <>
                {" · "}
                <span className="font-mono text-xs">
                  v{initial.version} · {initial.id.slice(0, 8)}
                </span>
              </>
            )}
          </p>
        </div>
        <div className="flex gap-2">
          <Button
            variant="outline"
            disabled={!canSave}
            onClick={() => saveMutation.mutate(false)}
          >
            <Save className="mr-2 h-4 w-4" aria-hidden="true" />
            {t("saveDraft")}
          </Button>
          <Button
            disabled={!canSave || rules.length === 0}
            onClick={() => setPublishOpen(true)}
          >
            <Send className="mr-2 h-4 w-4" aria-hidden="true" />
            {t("publish")}
          </Button>
        </div>
      </div>

      {/* Metadata */}
      <Card>
        <CardHeader>
          <CardTitle>{t("meta")}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="policy-name">{t("name")}</Label>
            <Input
              id="policy-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t("namePlaceholder")}
              maxLength={120}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="policy-desc">{t("description")}</Label>
            <Textarea
              id="policy-desc"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t("descPlaceholder")}
              rows={3}
              maxLength={500}
            />
          </div>
        </CardContent>
      </Card>

      <div className="grid gap-4 lg:grid-cols-[220px_1fr_320px]">
        {/* Palette */}
        <Card className="lg:sticky lg:top-4 lg:self-start">
          <CardHeader>
            <CardTitle className="text-sm">{t("palette")}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2">
            {PALETTE.map((kind) => {
              const meta = RULE_KIND_META[kind];
              const Icon = meta.icon;
              return (
                <button
                  key={kind}
                  type="button"
                  onClick={() => addRule(kind)}
                  className="group flex w-full items-start gap-2 rounded-md border p-2 text-left text-xs transition-colors hover:border-primary hover:bg-muted/40 focus:outline-none focus:ring-2 focus:ring-ring"
                  aria-label={`${t("add")}: ${meta.label}`}
                >
                  <Icon
                    className={
                      meta.tone === "danger"
                        ? "mt-0.5 h-4 w-4 shrink-0 text-red-500"
                        : meta.tone === "warn"
                        ? "mt-0.5 h-4 w-4 shrink-0 text-amber-500"
                        : "mt-0.5 h-4 w-4 shrink-0 text-muted-foreground"
                    }
                    aria-hidden="true"
                  />
                  <div className="space-y-0.5">
                    <div className="font-medium">{meta.label}</div>
                    <div className="text-[10px] text-muted-foreground">
                      {meta.hint}
                    </div>
                  </div>
                </button>
              );
            })}
          </CardContent>
        </Card>

        {/* Rule list */}
        <div className="space-y-3">
          {rules.length === 0 && (
            <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-16 text-center">
              <Shield className="mb-3 h-10 w-10 text-muted-foreground/40" aria-hidden="true" />
              <p className="text-sm text-muted-foreground">{t("emptyRules")}</p>
              <p className="mt-1 text-xs text-muted-foreground/80">
                {t("emptyRulesHint")}
              </p>
            </div>
          )}
          {rules.map((rule, idx) => (
            <RuleCard
              key={rule.id}
              rule={rule}
              index={idx}
              onChange={(values) => updateRuleValues(rule.id, values)}
              onRemove={() => removeRule(rule.id)}
            />
          ))}
        </div>

        {/* JSON preview */}
        <Card className="lg:sticky lg:top-4 lg:self-start">
          <CardHeader className="flex flex-row items-center justify-between space-y-0">
            <CardTitle className="text-sm">{t("preview")}</CardTitle>
            <Badge variant="outline" className="text-[10px]">
              {t("readOnly")}
            </Badge>
          </CardHeader>
          <CardContent>
            <pre className="max-h-[520px] overflow-auto rounded-md bg-muted/50 p-3 text-[11px] leading-relaxed">
              {JSON.stringify(projected, null, 2)}
            </pre>
            <Button
              variant="ghost"
              size="sm"
              className="mt-2 w-full"
              onClick={() => toast.info(t("testNotImplemented"))}
            >
              <PlayCircle className="mr-2 h-4 w-4" aria-hidden="true" />
              {t("test")}
            </Button>
          </CardContent>
        </Card>
      </div>

      {/* Publish confirmation */}
      <Dialog open={publishOpen} onOpenChange={setPublishOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("publishConfirmTitle")}</DialogTitle>
            <DialogDescription>{t("publishConfirmBody")}</DialogDescription>
          </DialogHeader>
          <Alert variant="destructive">
            <AlertTriangle className="h-4 w-4" />
            <AlertTitle>{t("publishWarningTitle")}</AlertTitle>
            <AlertDescription>{t("publishWarningBody")}</AlertDescription>
          </Alert>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setPublishOpen(false)}
              disabled={saveMutation.isPending}
            >
              {t("cancel")}
            </Button>
            <Button
              onClick={() => saveMutation.mutate(true)}
              disabled={saveMutation.isPending}
            >
              <Send className="mr-2 h-4 w-4" aria-hidden="true" />
              {t("confirmPublish")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* DLP opt-in warning (ADR 0013) */}
      <Dialog open={dlpWarnOpen} onOpenChange={setDlpWarnOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("dlpWarnTitle")}</DialogTitle>
            <DialogDescription>{t("dlpWarnBody")}</DialogDescription>
          </DialogHeader>
          <Alert variant="destructive">
            <KeySquare className="h-4 w-4" />
            <AlertTitle>ADR 0013</AlertTitle>
            <AlertDescription>{t("dlpWarnAdr")}</AlertDescription>
          </Alert>
          <DialogFooter>
            <Button onClick={() => setDlpWarnOpen(false)}>
              {t("understood")}
            </Button>
            <Link href={`/${locale}/kvkk/dlp`}>
              <Button variant="outline">{t("goToDlpSettings")}</Button>
            </Link>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

// ── Rule card ────────────────────────────────────────────────────────────────

interface RuleCardProps {
  rule: VisualRule;
  index: number;
  onChange: (values: string[]) => void;
  onRemove: () => void;
}

function RuleCard({
  rule,
  index,
  onChange,
  onRemove,
}: RuleCardProps): JSX.Element {
  const t = useTranslations("policies.editor");
  const meta = RULE_KIND_META[rule.kind];
  const Icon = meta.icon;
  const [draft, setDraft] = useState("");
  const [err, setErr] = useState<string | null>(null);

  function commitDraft(): void {
    const val = draft.trim();
    if (!val) return;
    const v = meta.validator(val);
    if (v !== null) {
      setErr(v);
      return;
    }
    if (rule.values.includes(val)) {
      setErr("duplicate");
      return;
    }
    onChange([...rule.values, val]);
    setDraft("");
    setErr(null);
  }

  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between space-y-0 pb-3">
        <div className="flex items-start gap-2">
          <Icon
            className={
              meta.tone === "danger"
                ? "mt-0.5 h-5 w-5 text-red-500"
                : meta.tone === "warn"
                ? "mt-0.5 h-5 w-5 text-amber-500"
                : "mt-0.5 h-5 w-5 text-muted-foreground"
            }
            aria-hidden="true"
          />
          <div>
            <CardTitle className="text-sm">
              #{index + 1} · {meta.label}
            </CardTitle>
            <p className="mt-0.5 text-xs text-muted-foreground">{meta.hint}</p>
          </div>
        </div>
        <Button
          variant="ghost"
          size="icon"
          onClick={onRemove}
          aria-label={t("removeRule")}
        >
          <Trash2 className="h-4 w-4" aria-hidden="true" />
        </Button>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="flex flex-wrap gap-1.5">
          {rule.values.map((v) => (
            <Badge
              key={v}
              variant="secondary"
              className="gap-1.5 pr-1 font-mono text-[11px]"
            >
              {v}
              <button
                type="button"
                onClick={() =>
                  onChange(rule.values.filter((x) => x !== v))
                }
                className="rounded-full p-0.5 hover:bg-muted-foreground/20"
                aria-label={`${t("remove")}: ${v}`}
              >
                <XCircle className="h-3 w-3" aria-hidden="true" />
              </button>
            </Badge>
          ))}
          {rule.values.length === 0 && (
            <span className="text-xs italic text-muted-foreground">
              {t("noValues")}
            </span>
          )}
        </div>
        <Separator />
        <div className="flex gap-2">
          <div className="flex-1 space-y-1">
            <Input
              value={draft}
              onChange={(e) => {
                setDraft(e.target.value);
                setErr(null);
              }}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault();
                  commitDraft();
                }
              }}
              placeholder={meta.placeholder}
              className={
                err
                  ? "border-red-500 focus-visible:ring-red-500 font-mono text-xs"
                  : "font-mono text-xs"
              }
              aria-invalid={err !== null}
              aria-describedby={err ? `err-${rule.id}` : undefined}
            />
            {err && (
              <p id={`err-${rule.id}`} className="text-[11px] text-red-500">
                {t(`errors.${err}`)}
              </p>
            )}
          </div>
          <Button variant="outline" size="sm" onClick={commitDraft}>
            <Plus className="mr-1 h-3 w-3" aria-hidden="true" />
            {t("add")}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
