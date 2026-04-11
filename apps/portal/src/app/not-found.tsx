import Link from "next/link";

/**
 * Global 404 page — welcoming, not alarming.
 * Directs users to home and suggests IT support for access issues.
 */
export default function NotFound(): JSX.Element {
  return (
    <html lang="tr">
      <body className="min-h-screen bg-warm-50 flex items-center justify-center px-4 font-sans">
        <div className="text-center max-w-sm">
          <p className="text-6xl font-bold text-warm-200 mb-4" aria-hidden="true">
            404
          </p>
          <h1 className="text-xl font-semibold text-warm-900 mb-2">
            Sayfa Bulunamadı
          </h1>
          <p className="text-sm text-warm-500 leading-relaxed mb-6">
            Aradığınız sayfa mevcut değil ya da taşınmış olabilir. Ana sayfaya
            dönebilir veya kurumunuzun BT destek birimiyle iletişime geçebilirsiniz.
          </p>
          <Link
            href="/tr"
            className="inline-flex items-center gap-2 bg-portal-600 hover:bg-portal-700 text-white font-medium py-2.5 px-5 rounded-xl text-sm transition-colors"
          >
            Ana Sayfaya Dön
          </Link>
        </div>
      </body>
    </html>
  );
}
