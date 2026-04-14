import type { ReactNode } from "react";
import { redirect } from "next/navigation";
import { getSession } from "@/lib/auth/session";
import { Sidebar } from "@/components/layout/sidebar";
import { Header } from "@/components/layout/header";

interface AppLayoutProps {
  children: ReactNode;
  params: Promise<{ locale: string }>;
}

export default async function AppLayout({
  children,
  params,
}: AppLayoutProps): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();

  if (!session?.user) {
    redirect(`/${locale}/login`);
  }

  return (
    <div className="flex h-screen overflow-hidden">
      {/* Skip-link target for keyboard navigation (WCAG 2.4.1) */}
      <a
        href="#main-content"
        className="sr-only focus:not-sr-only focus:fixed focus:left-2 focus:top-2 focus:z-[100] focus:rounded-md focus:bg-primary focus:px-3 focus:py-2 focus:text-primary-foreground"
      >
        İçeriğe geç
      </a>

      {/* Sidebar — hidden on mobile; MobileNav drawer is in Header */}
      <Sidebar />

      {/* Main content area — min-w-0 lets flex children shrink below the
          sidebar width so wide tables can scroll inside the main region
          instead of overflowing the viewport. */}
      <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
        <Header />
        <main
          className="flex-1 overflow-y-auto bg-background p-4 sm:p-6 scrollbar-thin"
          id="main-content"
          role="main"
          tabIndex={-1}
        >
          {children}
        </main>
      </div>
    </div>
  );
}
