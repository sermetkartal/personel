"use client";

/**
 * Mobile navigation drawer.
 *
 * Visible only below the `md:` breakpoint. Uses the shadcn Sheet primitive
 * (which is Radix Dialog under the hood) so focus trap + Esc + inert
 * background are handled natively — satisfying WCAG 2.1.2 (Focusable) and
 * 2.4.3 (Focus Order) without hand-rolled traps.
 *
 * Shares the exact NavItem tree with the desktop Sidebar; any new navigation
 * entry needs to be added in `sidebar.tsx::buildNavItems`.
 */

import { useState } from "react";
import { useTranslations } from "next-intl";
import { useParams, usePathname } from "next/navigation";
import Link from "next/link";
import { Menu } from "lucide-react";
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { useCurrentUser } from "@/lib/hooks/use-current-user";
import { can } from "@/lib/auth/rbac";
import { buildNavItems, type NavItem } from "./sidebar-nav-items";
import type { Role } from "@/lib/api/types";

function MobileNavLink({
  item,
  userRole,
  pathname,
  depth = 0,
}: {
  item: NavItem;
  userRole: Role;
  pathname: string;
  depth?: number;
}): JSX.Element | null {
  const t = useTranslations("nav");

  if (item.requiredAction && !can(userRole, item.requiredAction)) {
    return null;
  }

  const label = t(item.key as Parameters<typeof t>[0]);
  const isActive =
    pathname === item.href ||
    (item.href !== "/" && pathname.startsWith(item.href));

  if (item.children && item.children.length > 0) {
    return (
      <div className="space-y-0.5">
        <div
          className={cn(
            "flex items-center gap-3 rounded-md px-3 py-2 text-sm font-semibold text-muted-foreground",
            depth > 0 && "pl-6",
          )}
        >
          <item.icon className="h-4 w-4 shrink-0" aria-hidden="true" />
          <span>{label}</span>
        </div>
        <div className="space-y-0.5">
          {item.children.map((child) => (
            <MobileNavLink
              key={child.key}
              item={child}
              userRole={userRole}
              pathname={pathname}
              depth={depth + 1}
            />
          ))}
        </div>
      </div>
    );
  }

  return (
    <SheetClose asChild>
      <Link
        href={item.href}
        className={cn(
          "flex min-h-[44px] items-center gap-3 rounded-md px-3 py-2.5 text-sm font-medium transition-colors",
          isActive
            ? "bg-primary text-primary-foreground"
            : "text-foreground hover:bg-muted",
          depth > 0 && "pl-9",
        )}
        aria-current={isActive ? "page" : undefined}
      >
        <item.icon className="h-4 w-4 shrink-0" aria-hidden="true" />
        <span>{label}</span>
      </Link>
    </SheetClose>
  );
}

export function MobileNav(): JSX.Element {
  const [open, setOpen] = useState(false);
  const { user } = useCurrentUser();
  const params = useParams();
  const pathname = usePathname();
  const locale = (params.locale as string | undefined) ?? "tr";
  const t = useTranslations("common");

  const navItems = buildNavItems(locale);

  return (
    <Sheet open={open} onOpenChange={setOpen}>
      <SheetTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          className="md:hidden"
          aria-label={t("openNavigation")}
        >
          <Menu className="h-5 w-5" aria-hidden="true" />
        </Button>
      </SheetTrigger>
      <SheetContent side="left" className="w-72 p-0">
        <SheetHeader className="border-b px-4 py-3">
          <SheetTitle className="flex items-center gap-2">
            <div
              className="flex h-8 w-8 items-center justify-center rounded-md bg-primary text-primary-foreground font-bold text-sm"
              aria-hidden="true"
            >
              P
            </div>
            <span>{t("appName")}</span>
          </SheetTitle>
        </SheetHeader>
        <nav
          className="flex-1 overflow-y-auto p-2"
          aria-label={t("mainNavigation")}
        >
          {user &&
            navItems.map((item) => (
              <MobileNavLink
                key={item.key}
                item={item}
                userRole={user.role}
                pathname={pathname}
              />
            ))}
        </nav>
      </SheetContent>
    </Sheet>
  );
}
