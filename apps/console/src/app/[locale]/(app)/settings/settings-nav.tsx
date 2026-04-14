"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useTranslations } from "next-intl";
import {
  Settings2,
  ShieldOff,
  Building2,
  Users,
  Plug,
  FileArchive,
  UserCircle,
  Lock,
  Clock,
  HardDrive,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { can } from "@/lib/auth/rbac";
import type { Role } from "@/lib/api/types";

interface SettingsNavProps {
  locale: string;
  role: Role;
}

interface NavItem {
  href: string;
  labelKey: string;
  icon: React.ElementType;
  requiredAction?: Parameters<typeof can>[1];
}

export function SettingsNav({ locale, role }: SettingsNavProps): JSX.Element {
  const t = useTranslations("settings.tabs");
  const pathname = usePathname();

  const items: NavItem[] = [
    {
      href: `/${locale}/settings/general`,
      labelKey: "general",
      icon: Settings2,
    },
    {
      href: `/${locale}/settings/dlp`,
      labelKey: "dlp",
      icon: ShieldOff,
      requiredAction: "view:dlp-settings",
    },
    {
      href: `/${locale}/settings/tenants`,
      labelKey: "tenants",
      icon: Building2,
      requiredAction: "manage:tenants",
    },
    {
      href: `/${locale}/settings/users`,
      labelKey: "users",
      icon: Users,
      requiredAction: "manage:users",
    },
    {
      href: `/${locale}/settings/integrations`,
      labelKey: "integrations",
      icon: Plug,
      requiredAction: "view:settings",
    },
    {
      href: `/${locale}/settings/security/tls`,
      labelKey: "tls",
      icon: Lock,
      requiredAction: "view:settings",
    },
    {
      href: `/${locale}/settings/retention`,
      labelKey: "retention",
      icon: Clock,
      requiredAction: "view:settings",
    },
    {
      href: `/${locale}/settings/backup`,
      labelKey: "backup",
      icon: HardDrive,
      requiredAction: "view:settings",
    },
    {
      href: `/${locale}/evidence`,
      labelKey: "evidence",
      icon: FileArchive,
      requiredAction: "view:evidence",
    },
    {
      href: `/${locale}/settings/profile`,
      labelKey: "profile",
      icon: UserCircle,
    },
  ];

  const visible = items.filter(
    (item) => !item.requiredAction || can(role, item.requiredAction),
  );

  return (
    <nav className="space-y-1" aria-label="Settings sections">
      {visible.map((item) => {
        const active = pathname?.startsWith(item.href) ?? false;
        const Icon = item.icon;
        return (
          <Link
            key={item.href}
            href={item.href}
            className={cn(
              "flex items-center gap-2 rounded-md px-3 py-2 text-sm transition-colors",
              active
                ? "bg-muted font-medium text-foreground"
                : "text-muted-foreground hover:bg-muted/50 hover:text-foreground",
            )}
            aria-current={active ? "page" : undefined}
          >
            <Icon className="h-4 w-4" aria-hidden="true" />
            {t(item.labelKey)}
          </Link>
        );
      })}
    </nav>
  );
}
