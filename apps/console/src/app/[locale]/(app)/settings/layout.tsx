import { getSession } from "@/lib/auth/session";
import { redirect } from "next/navigation";
import { SettingsNav } from "./settings-nav";

interface SettingsLayoutProps {
  children: React.ReactNode;
  params: Promise<{ locale: string }>;
}

export default async function SettingsLayout({
  children,
  params,
}: SettingsLayoutProps): Promise<JSX.Element> {
  const { locale } = await params;
  const session = await getSession();

  // Layout only enforces authentication — individual pages perform their
  // own RBAC checks. This lets an employee reach /settings/profile without
  // needing the broader `view:settings` scope.
  if (!session?.user) {
    redirect(`/${locale}/login`);
  }

  return (
    <div className="animate-fade-in">
      <div className="grid gap-6 lg:grid-cols-[220px_1fr]">
        <aside className="lg:sticky lg:top-4 lg:self-start">
          <SettingsNav locale={locale} role={session.user.role} />
        </aside>
        <main className="min-w-0">{children}</main>
      </div>
    </div>
  );
}
