import Link from "next/link";

export default function NotFound(): JSX.Element {
  return (
    <html lang="tr">
      <body className="min-h-screen bg-background flex items-center justify-center">
        <div className="text-center space-y-4 max-w-md p-8">
          <div className="text-6xl font-bold text-muted-foreground">404</div>
          <h1 className="text-2xl font-semibold">Sayfa Bulunamadı</h1>
          <p className="text-muted-foreground">
            Aradığınız sayfa mevcut değil veya taşınmış olabilir.
          </p>
          <Link
            href="/tr/dashboard"
            className="inline-flex items-center justify-center rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 focus-visible:outline-2 focus-visible:outline-ring"
          >
            Ana Sayfaya Dön
          </Link>
        </div>
      </body>
    </html>
  );
}
