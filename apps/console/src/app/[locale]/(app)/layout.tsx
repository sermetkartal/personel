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
      {/* Sidebar — always visible on desktop */}
      <Sidebar />

      {/* Main content area */}
      <div className="flex flex-1 flex-col overflow-hidden">
        <Header />
        <main
          className="flex-1 overflow-y-auto bg-background p-6 scrollbar-thin"
          id="main-content"
          role="main"
        >
          {children}
        </main>
      </div>
    </div>
  );
}
