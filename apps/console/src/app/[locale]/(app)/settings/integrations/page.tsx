import { getTranslations } from "next-intl/server";
import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { can } from "@/lib/auth/rbac";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import {
  Users2,
  Building2,
  Shield,
  Activity,
  Plug,
  TestTube2,
} from "lucide-react";

interface IntegrationsSettingsPageProps {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata() {
  const t = await getTranslations("settings.integrations");
  return { title: t("title") };
}

export default async function IntegrationsSettingsPage({
  params,
}: IntegrationsSettingsPageProps): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();

  if (!session?.user || !can(session.user.role, "view:settings")) {
    redirect(`/${locale}/unauthorized`);
  }

  const t = await getTranslations("settings.integrations");

  return (
    <TooltipProvider>
      <div className="space-y-6">
        <div className="flex items-start justify-between">
          <div>
            <h2 className="text-xl font-semibold tracking-tight">{t("title")}</h2>
            <p className="text-sm text-muted-foreground">{t("subtitle")}</p>
          </div>
          <Badge variant="warning" className="shrink-0">
            {t("scaffoldBadge")}
          </Badge>
        </div>

        {/* HRIS */}
        <section className="space-y-3">
          <div className="flex items-center gap-2">
            <Users2 className="h-5 w-5 text-muted-foreground" aria-hidden="true" />
            <h3 className="text-lg font-semibold">{t("hrisTitle")}</h3>
          </div>
          <p className="text-sm text-muted-foreground">{t("hrisHint")}</p>

          <div className="grid gap-4 md:grid-cols-2">
            <HrisConnectorCard
              title="BambooHR"
              icon={Building2}
              statusLabel={t("statusDisconnected")}
              statusVariant="outline"
              description={t("bambooDescription")}
              fields={[
                { id: "bamboo-subdomain", label: t("bambooSubdomain"), placeholder: "mycompany" },
                { id: "bamboo-api-key", label: t("apiKey"), placeholder: "••••••••", type: "password" },
              ]}
            />
            <HrisConnectorCard
              title="Logo Tiger"
              icon={Building2}
              statusLabel={t("statusDisconnected")}
              statusVariant="outline"
              description={t("logoTigerDescription")}
              fields={[
                { id: "logo-base-url", label: t("baseUrl"), placeholder: "https://logo.firma.com" },
                { id: "logo-client-id", label: t("clientId"), placeholder: "personel-sync" },
                { id: "logo-client-secret", label: t("clientSecret"), placeholder: "••••••••", type: "password" },
              ]}
            />
          </div>
        </section>

        {/* SIEM */}
        <section className="space-y-3">
          <div className="flex items-center gap-2">
            <Shield className="h-5 w-5 text-muted-foreground" aria-hidden="true" />
            <h3 className="text-lg font-semibold">{t("siemTitle")}</h3>
          </div>
          <p className="text-sm text-muted-foreground">{t("siemHint")}</p>

          <div className="grid gap-4 md:grid-cols-2">
            <SiemExporterCard
              title="Splunk HEC"
              description={t("splunkDescription")}
              fields={[
                { id: "splunk-url", label: t("hecUrl"), placeholder: "https://splunk:8088/services/collector" },
                { id: "splunk-token", label: t("hecToken"), placeholder: "••••••••", type: "password" },
              ]}
            />
            <SiemExporterCard
              title="Microsoft Sentinel"
              description={t("sentinelDescription")}
              fields={[
                { id: "sentinel-dce", label: t("dceUrl"), placeholder: "https://mydce.eastus-1.ingest.monitor.azure.com" },
                { id: "sentinel-dcr", label: t("dcrImmutableId"), placeholder: "dcr-xxxxxxxx" },
                { id: "sentinel-stream", label: t("streamName"), placeholder: "Custom-Personel_CL" },
              ]}
            />
          </div>
        </section>

        {/* Observability */}
        <section className="space-y-3">
          <div className="flex items-center gap-2">
            <Activity className="h-5 w-5 text-muted-foreground" aria-hidden="true" />
            <h3 className="text-lg font-semibold">{t("observabilityTitle")}</h3>
          </div>
          <Card>
            <CardContent className="space-y-3 pt-6 text-sm">
              <ReadonlyRow label="Prometheus" value="http://prometheus:9090" />
              <ReadonlyRow label="Loki" value="http://loki:3100" />
              <ReadonlyRow label="Tempo" value="http://tempo:3200" />
              <p className="text-xs text-muted-foreground">{t("observabilityHint")}</p>
            </CardContent>
          </Card>
        </section>
      </div>
    </TooltipProvider>
  );
}

interface ConnectorField {
  id: string;
  label: string;
  placeholder: string;
  type?: string;
}

function HrisConnectorCard({
  title,
  icon: Icon,
  statusLabel,
  statusVariant,
  description,
  fields,
}: {
  title: string;
  icon: React.ElementType;
  statusLabel: string;
  statusVariant: "outline" | "success" | "warning";
  description: string;
  fields: ConnectorField[];
}): JSX.Element {
  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between space-y-0 pb-3">
        <div className="flex items-start gap-2">
          <Icon className="mt-0.5 h-5 w-5 text-muted-foreground" aria-hidden="true" />
          <div>
            <CardTitle className="text-sm">{title}</CardTitle>
            <p className="mt-1 text-xs text-muted-foreground">{description}</p>
          </div>
        </div>
        <Badge variant={statusVariant}>{statusLabel}</Badge>
      </CardHeader>
      <CardContent className="space-y-3">
        {fields.map((f) => (
          <div key={f.id} className="space-y-1.5">
            <Label htmlFor={f.id} className="text-xs">
              {f.label}
            </Label>
            <Input
              id={f.id}
              type={f.type ?? "text"}
              placeholder={f.placeholder}
              disabled
            />
          </div>
        ))}
        <div className="flex gap-2 pt-1">
          <ScaffoldButton icon={TestTube2} labelKey="testConnection" />
          <ScaffoldButton icon={Plug} labelKey="connect" variant="default" />
        </div>
      </CardContent>
    </Card>
  );
}

function SiemExporterCard({
  title,
  description,
  fields,
}: {
  title: string;
  description: string;
  fields: ConnectorField[];
}): JSX.Element {
  return (
    <Card>
      <CardHeader className="space-y-1 pb-3">
        <div className="flex items-center justify-between">
          <CardTitle className="text-sm">{title}</CardTitle>
          <Tooltip>
            <TooltipTrigger asChild>
              <div>
                <Switch disabled />
              </div>
            </TooltipTrigger>
            <TooltipContent>Yakında</TooltipContent>
          </Tooltip>
        </div>
        <p className="text-xs text-muted-foreground">{description}</p>
      </CardHeader>
      <CardContent className="space-y-3">
        {fields.map((f) => (
          <div key={f.id} className="space-y-1.5">
            <Label htmlFor={f.id} className="text-xs">
              {f.label}
            </Label>
            <Input
              id={f.id}
              type={f.type ?? "text"}
              placeholder={f.placeholder}
              disabled
            />
          </div>
        ))}
      </CardContent>
    </Card>
  );
}

function ReadonlyRow({
  label,
  value,
}: {
  label: string;
  value: string;
}): JSX.Element {
  return (
    <div className="flex items-center justify-between rounded-md border bg-muted/30 px-3 py-2">
      <span className="text-xs text-muted-foreground">{label}</span>
      <span className="font-mono text-xs">{value}</span>
    </div>
  );
}

function ScaffoldButton({
  icon: Icon,
  labelKey,
  variant = "outline",
}: {
  icon: React.ElementType;
  labelKey: string;
  variant?: "outline" | "default";
}): JSX.Element {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="inline-block">
          <Button variant={variant} size="sm" disabled>
            <Icon className="mr-1.5 h-3 w-3" aria-hidden="true" />
            {labelKey === "testConnection" ? "Bağlantıyı Test Et" : "Bağlan"}
          </Button>
        </span>
      </TooltipTrigger>
      <TooltipContent>Yakında</TooltipContent>
    </Tooltip>
  );
}
