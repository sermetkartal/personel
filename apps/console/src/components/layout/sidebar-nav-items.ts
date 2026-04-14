/**
 * Shared navigation item tree used by both the desktop Sidebar and the
 * mobile drawer. Keeping this as a plain module (not a component) makes
 * it safe to import into both server and client trees — the icons are
 * typed as component references, not rendered here.
 */

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
  Search,
  Settings,
  Activity,
  Lock,
  FileArchive,
} from "lucide-react";
import type { Action } from "@/lib/auth/rbac";

export interface NavItem {
  key: string;
  icon: React.ElementType;
  href: string;
  requiredAction?: Action;
  children?: NavItem[];
}

export function buildNavItems(locale: string): NavItem[] {
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
      key: "evidence",
      icon: FileArchive,
      href: `/${locale}/evidence`,
      requiredAction: "view:evidence",
    },
    {
      key: "audit",
      icon: ClipboardList,
      href: `/${locale}/audit`,
      requiredAction: "view:audit-trail",
      children: [
        {
          key: "auditMenu.list",
          icon: ClipboardList,
          href: `/${locale}/audit`,
        },
        {
          key: "auditMenu.search",
          icon: Search,
          href: `/${locale}/audit/search`,
          requiredAction: "view:audit-log",
        },
      ],
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
