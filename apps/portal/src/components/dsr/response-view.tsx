import { useTranslations } from "next-intl";
import { Download, MessageSquare } from "lucide-react";
import type { DSRRequest } from "@/lib/api/types";

interface ResponseViewProps {
  dsr: DSRRequest;
}

export function ResponseView({ dsr }: ResponseViewProps): JSX.Element {
  const t = useTranslations("basvuruDetay");

  const hasResponse =
    (dsr.state === "closed" || dsr.state === "rejected") &&
    dsr.response_artifact_ref;

  return (
    <div className="space-y-4">
      <h4 className="text-sm font-medium text-warm-700 flex items-center gap-2">
        <MessageSquare className="w-4 h-4" aria-hidden="true" />
        {t("response")}
      </h4>

      {hasResponse && dsr.response_artifact_ref ? (
        <div className="rounded-xl bg-trust-50 border border-trust-200 p-4">
          <p className="text-sm text-warm-700 mb-3">
            DPO yanıtı hazır. PDF dosyasını indirmek için aşağıdaki butona tıklayın.
          </p>
          <a
            href={`/api/dsr-response/${dsr.id}`}
            download
            className="inline-flex items-center gap-2 text-sm text-portal-600 hover:text-portal-800 font-medium"
          >
            <Download className="w-4 h-4" aria-hidden="true" />
            {t("downloadResponse")}
          </a>
        </div>
      ) : (
        <p className="text-sm text-warm-500 italic">{t("noResponse")}</p>
      )}
    </div>
  );
}
