import { notFound } from "next/navigation";
import { NextIntlClientProvider } from "next-intl";
import { getMessages } from "next-intl/server";
import type { Metadata } from "next";
import { routing } from "@/lib/i18n/routing";
import { getSession } from "@/lib/auth/session";
import { getDLPState } from "@/lib/api/dlp-state";
import { Header } from "@/components/layout/header";
import { Nav } from "@/components/layout/nav";
import { Footer } from "@/components/layout/footer";
import { DlpBanner } from "@/components/layout/dlp-banner";
import { FirstLoginGate } from "@/components/onboarding/first-login-gate";
import { Providers } from "@/app/providers";
import type { DLPStateResponse } from "@/lib/api/types";

export const metadata: Metadata = {
  title: {
    default: "Şeffaflık Portalı",
    template: "%s | Şeffaflık Portalı",
  },
};

interface LocaleLayoutProps {
  children: React.ReactNode;
  params: Promise<{ locale: string }>;
}

export default async function LocaleLayout({
  children,
  params,
}: LocaleLayoutProps): Promise<JSX.Element> {
  const { locale } = await params;

  // Validate locale
  if (!routing.locales.includes(locale as "tr" | "en")) {
    notFound();
  }

  const messages = await getMessages();
  const session = await getSession();

  // Fetch DLP state server-side so the banner renders without a client waterfall
  // Defaults to "disabled" per ADR 0013 if the API is unavailable
  let dlpState: DLPStateResponse = { status: "disabled" };
  if (session) {
    try {
      dlpState = await getDLPState(session.accessToken);
    } catch {
      // API endpoint may not yet be deployed — default is safe per ADR 0013
    }
  }

  return (
    <html lang={locale} suppressHydrationWarning>
      <body>
        <NextIntlClientProvider messages={messages}>
          <Providers>
            {session ? (
              <FirstLoginGate
                accessToken={session.accessToken}
                showModal={!session.firstLoginAcknowledged}
              >
                <div className="min-h-screen flex flex-col bg-warm-50">
                  {/* Skip to main content — accessibility */}
                  <a href="#main-content" className="skip-nav">
                    Ana içeriğe geç
                  </a>

                  {/* Sticky header */}
                  <Header session={session} />

                  {/* DLP state banner — always visible, server-rendered */}
                  <DlpBanner state={dlpState} />

                  {/* Three-column layout: sidebar + main */}
                  <div className="flex flex-1 max-w-7xl w-full mx-auto">
                    {/* Left sidebar nav */}
                    <aside
                      className="hidden lg:block w-56 xl:w-64 flex-shrink-0 pt-6 px-4 pb-8"
                      aria-label="Yan menü"
                    >
                      <Nav />
                    </aside>

                    {/* Main content */}
                    <main
                      id="main-content"
                      tabIndex={-1}
                      className="flex-1 min-w-0 px-4 sm:px-6 lg:px-8 py-8"
                      aria-label="Ana içerik"
                    >
                      {children}
                    </main>
                  </div>

                  <Footer />
                </div>
              </FirstLoginGate>
            ) : (
              // Unauthenticated shell — minimal, used for auth pages
              <div className="min-h-screen flex flex-col bg-warm-50">
                <main id="main-content" className="flex-1">
                  {children}
                </main>
              </div>
            )}
          </Providers>
        </NextIntlClientProvider>
      </body>
    </html>
  );
}
