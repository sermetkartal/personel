import { useTranslations, useLocale } from "next-intl";
import Link from "next/link";

export function Footer(): JSX.Element {
  const t = useTranslations("nav");
  const locale = useLocale();
  const dpoEmail = process.env["NEXT_PUBLIC_DPO_EMAIL"] ?? "kvkk@musteri.com.tr";

  return (
    <footer className="mt-auto border-t border-warm-200 bg-white">
      <div className="max-w-5xl mx-auto px-6 py-6 flex flex-col sm:flex-row gap-4 items-center justify-between text-sm text-warm-500">
        <div className="flex items-center gap-2">
          <span>Personel Şeffaflık Portalı</span>
          <span aria-hidden="true">·</span>
          <span>KVKK m.10 / m.11</span>
        </div>
        <div className="flex items-center gap-4">
          <Link
            href={`/${locale}/iletisim`}
            className="hover:text-warm-800 transition-colors"
          >
            {t("iletisim")}
          </Link>
          <a
            href={`mailto:${dpoEmail}`}
            className="hover:text-warm-800 transition-colors"
          >
            {dpoEmail}
          </a>
        </div>
      </div>
    </footer>
  );
}
