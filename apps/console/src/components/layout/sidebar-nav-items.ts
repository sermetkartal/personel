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
  Scale,
  BookOpen,
  FilePen,
  FileCheck2,
  FileSignature,
  ShieldAlert,
  Plug,
  Clock,
  HardDrive,
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
      key: "kvkk",
      icon: Scale,
      href: `/${locale}/kvkk/guide`,
      children: [
        {
          key: "kvkkMenu.guide",
          icon: BookOpen,
          href: `/${locale}/kvkk/guide`,
        },
        {
          key: "kvkkMenu.dsr",
          icon: FileText,
          href: `/${locale}/kvkk/dsr`,
          requiredAction: "manage:dsr",
        },
        {
          key: "kvkkMenu.legalHold",
          icon: Lock,
          href: `/${locale}/kvkk/legal-hold`,
          requiredAction: "place:legal-hold",
        },
        {
          key: "kvkkMenu.destructionReports",
          icon: Trash2,
          href: `/${locale}/kvkk/destruction-reports`,
          requiredAction: "view:destruction-reports",
        },
        {
          key: "kvkkMenu.dlp",
          icon: ShieldAlert,
          href: `/${locale}/kvkk/dlp`,
        },
        {
          key: "kvkkMenu.verbis",
          icon: FileCheck2,
          href: `/${locale}/kvkk/verbis`,
          requiredAction: "manage:kvkk",
        },
        {
          key: "kvkkMenu.aydinlatma",
          icon: FilePen,
          href: `/${locale}/kvkk/aydinlatma`,
          requiredAction: "manage:kvkk",
        },
        {
          key: "kvkkMenu.dpa",
          icon: FileSignature,
          href: `/${locale}/kvkk/dpa`,
          requiredAction: "manage:kvkk",
        },
        {
          key: "kvkkMenu.dpia",
          icon: FileSignature,
          href: `/${locale}/kvkk/dpia`,
          requiredAction: "manage:kvkk",
        },
        {
          key: "kvkkMenu.acikRiza",
          icon: FileSignature,
          href: `/${locale}/kvkk/acik-riza`,
          requiredAction: "manage:kvkk",
        },
      ],
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
        {
          key: "settingsMenu.externalServices",
          icon: Plug,
          href: `/${locale}/settings/integrations`,
          requiredAction: "view:settings",
        },
        {
          key: "settingsMenu.tls",
          icon: Lock,
          href: `/${locale}/settings/security/tls`,
          requiredAction: "view:settings",
        },
        {
          key: "settingsMenu.retention",
          icon: Clock,
          href: `/${locale}/settings/retention`,
          requiredAction: "view:settings",
        },
        {
          key: "settingsMenu.backup",
          icon: HardDrive,
          href: `/${locale}/settings/backup`,
          requiredAction: "view:settings",
        },
      ],
    },
  ];
}
