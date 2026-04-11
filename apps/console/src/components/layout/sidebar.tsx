"use client";

import Link from "next/link";
import { usePathname, useParams } from "next/navigation";
import { useTranslations } from "next-intl";
import {
  LayoutDashboard,
  Monitor,
  Users,
  BarChart3,
  Eye,
  FileText,
  Shield,
  Trash2,
  ClipboardList,
  Settings,
  Activity,
  Lock,
  ChevronDown,
  ChevronRight,
} from "lucide-react";
import { useState } from "react";
import { cn } from "@/lib/utils";
import { useCurrentUser } from "@/lib/hooks/use-current-user";
import { can } from "@/lib/auth/rbac";
import type { Action } from "@/lib/auth/rbac";
import type { Role } from "@/lib/api/types";

interface NavItem {
  key: string;
  icon: React.ElementType;
  href: string;
  requiredAction?: Action;
  children?: NavItem[];
}

function buildNavItems(locale: string): NavItem[] {
  return [
    {
      key: "dashboard",
      icon: LayoutDashboard,
      href: `/${locale}/dashboard`,
    },
    {
      key: "endpoints",
      icon: Monitor,
      href: `/${locale}/endpoints`,
      requiredAction: "manage:endpoints",
    },
    {
      key: "employees",
      icon: Users,
      href: `/${locale}/employees`,
    },
    {
      key: "reports",
      icon: BarChart3,
      href: `/${locale}/reports`,
      requiredAction: "view:reports",
    },
    {
      key: "liveView",
      icon: Eye,
      href: `/${locale}/live-view`,
      requiredAction: "view:live-view-sessions",
    },
    {
      key: "dsr",
      icon: FileText,
      href: `/${locale}/dsr`,
      requiredAction: "manage:dsr",
    },
    {
      key: "legalHold",
      icon: Lock,
      href: `/${locale}/legal-hold`,
      requiredAction: "place:legal-hold",
    },
    {
      key: "destructionReports",
      icon: Trash2,
      href: `/${locale}/destruction-reports`,
      requiredAction: "view:destruction-reports",
    },
    {
      key: "audit",
      icon: ClipboardList,
      href: `/${locale}/audit`,
      requiredAction: "view:audit-trail",
    },
    {
      key: "policies",
      icon: Shield,
      href: `/${locale}/policies`,
      requiredAction: "manage:policies",
    },
    {
      key: "silence",
      icon: Activity,
      href: `/${locale}/silence`,
      requiredAction: "view:silence",
    },
    {
      key: "settings",
      icon: Settings,
      href: `/${locale}/settings`,
      children: [
        {
          key: "settingsMenu.dlp",
          icon: Shield,
          href: `/${locale}/settings/dlp`,
        },
        {
          key: "settingsMenu.tenants",
          icon: Settings,
          href: `/${locale}/settings/tenants`,
          requiredAction: "manage:tenants",
        },
        {
          key: "settingsMenu.users",
          icon: Users,
          href: `/${locale}/settings/users`,
          requiredAction: "manage:users",
        },
      ],
    },
  ];
}

interface NavLinkProps {
  item: NavItem;
  userRole: Role;
  depth?: number;
}

function NavLink({ item, userRole, depth = 0 }: NavLinkProps): JSX.Element | null {
  const t = useTranslations("nav");
  const pathname = usePathname();
  const [open, setOpen] = useState(false);

  // Check permission
  if (item.requiredAction && !can(userRole, item.requiredAction)) {
    return null;
  }

  const isActive =
    pathname === item.href ||
    (item.href !== "/" && pathname.startsWith(item.href));

  const hasChildren = item.children && item.children.length > 0;

  const label = t(item.key as Parameters<typeof t>[0]);

  if (hasChildren) {
    return (
      <div>
        <button
          onClick={() => setOpen(!open)}
          className={cn(
            "flex w-full items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors",
            "text-sidebar-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
            depth > 0 && "pl-8",
          )}
          aria-expanded={open}
          aria-label={label}
        >
          <item.icon className="h-4 w-4 shrink-0" aria-hidden="true" />
          <span className="flex-1 text-left">{label}</span>
          {open ? (
            <ChevronDown className="h-3 w-3" aria-hidden="true" />
          ) : (
            <ChevronRight className="h-3 w-3" aria-hidden="true" />
          )}
        </button>
        {open && (
          <div className="mt-0.5 space-y-0.5">
            {item.children?.map((child) => (
              <NavLink
                key={child.key}
                item={child}
                userRole={userRole}
                depth={depth + 1}
              />
            ))}
          </div>
        )}
      </div>
    );
  }

  return (
    <Link
      href={item.href}
      className={cn(
        "flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors",
        isActive
          ? "bg-sidebar-primary text-sidebar-primary-foreground"
          : "text-sidebar-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
        depth > 0 && "pl-8",
      )}
      aria-current={isActive ? "page" : undefined}
      aria-label={label}
    >
      <item.icon className="h-4 w-4 shrink-0" aria-hidden="true" />
      <span>{label}</span>
    </Link>
  );
}

export function Sidebar(): JSX.Element {
  const { user } = useCurrentUser();
  const params = useParams();
  const locale = (params.locale as string | undefined) ?? "tr";
  const t = useTranslations("common");

  const navItems = buildNavItems(locale);

  return (
    <aside
      className="flex h-full w-sidebar flex-col bg-sidebar"
      role="navigation"
      aria-label="Ana navigasyon"
    >
      {/* Logo + brand */}
      <div className="flex h-14 items-center gap-2 border-b border-sidebar-border px-4">
        <div
          className="flex h-8 w-8 items-center justify-center rounded-md bg-sidebar-primary text-sidebar-primary-foreground font-bold text-sm"
          aria-hidden="true"
        >
          P
        </div>
        <div className="flex flex-col">
          <span className="text-sm font-semibold text-sidebar-foreground">
            {t("appName")}
          </span>
          <span className="text-xs text-sidebar-foreground/60">
            Yönetici Konsolu
          </span>
        </div>
      </div>

      {/* Navigation items */}
      <nav className="flex-1 overflow-y-auto px-2 py-3 space-y-0.5 scrollbar-thin">
        {user
          ? navItems.map((item) => (
              <NavLink key={item.key} item={item} userRole={user.role} />
            ))
          : null}
      </nav>

      {/* Version footer */}
      <div className="border-t border-sidebar-border px-4 py-3">
        <p className="text-xs text-sidebar-foreground/40">
          v{process.env.NEXT_PUBLIC_APP_VERSION ?? "0.1.0"}
        </p>
      </div>
    </aside>
  );
}
