"use client";

import { useTranslations } from "next-intl";
import Link from "next/link";
import { useLocale } from "next-intl";
import {
  LogOut,
  User,
  ChevronDown,
} from "lucide-react";
import { useCurrentUser } from "@/lib/hooks/use-current-user";
import { DLPStateIndicator } from "@/components/dlp/state-indicator";
import { RoleBadge } from "./role-badge";
import { MobileNav } from "./mobile-nav";
import { NotificationBell } from "./notification-bell";
import { LocaleSwitcher } from "./locale-switcher";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

interface HeaderProps {
  className?: string;
}

export function Header({ className }: HeaderProps): JSX.Element {
  const t = useTranslations();
  const locale = useLocale();
  const { user, isLoading } = useCurrentUser();

  return (
    <header
      className={cn(
        "flex h-14 items-center justify-between border-b border-border bg-background/95 px-4 backdrop-blur supports-[backdrop-filter]:bg-background/60",
        className,
      )}
      role="banner"
    >
      {/* Left: mobile nav trigger (<md) + breadcrumb placeholder */}
      <div className="flex items-center gap-2">
        <MobileNav />
        <div id="header-breadcrumb" />
      </div>

      {/* Right: DLP badge + locale + notifications + user menu */}
      <div className="flex items-center gap-1 sm:gap-3">
        {/* DLP State — always visible per ADR 0013 */}
        <div className="hidden sm:block">
          <DLPStateIndicator />
        </div>

        <LocaleSwitcher />

        <NotificationBell />

        {/* User menu */}
        {isLoading ? (
          <Skeleton className="h-8 w-32 rounded-full" />
        ) : user ? (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="ghost"
                className="flex items-center gap-2 px-2"
                aria-label={`Kullanıcı menüsü: ${user.username}`}
              >
                <div className="flex h-7 w-7 items-center justify-center rounded-full bg-primary/10 text-primary">
                  <User className="h-4 w-4" aria-hidden="true" />
                </div>
                <div className="hidden flex-col items-start sm:flex">
                  <span className="text-sm font-medium leading-none">
                    {user.username}
                  </span>
                  <span className="text-xs text-muted-foreground">
                    {t(`common.roles.${user.role}`)}
                  </span>
                </div>
                <ChevronDown className="h-3 w-3 opacity-50" aria-hidden="true" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-56">
              <DropdownMenuLabel className="font-normal">
                <div className="flex flex-col space-y-1">
                  <p className="text-sm font-medium leading-none">
                    {user.username}
                  </p>
                  <p className="text-xs leading-none text-muted-foreground">
                    {user.email}
                  </p>
                </div>
              </DropdownMenuLabel>
              <DropdownMenuSeparator />
              <DropdownMenuItem>
                <RoleBadge role={user.role} />
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem asChild>
                <Link
                  href={`/${locale}/settings/profile`}
                  className="flex items-center gap-2"
                >
                  <User className="h-4 w-4" aria-hidden="true" />
                  {t("settings.profile.title")}
                </Link>
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem asChild>
                <Link
                  href="/api/auth/logout"
                  className="flex items-center gap-2 text-destructive"
                >
                  <LogOut className="h-4 w-4" aria-hidden="true" />
                  {t("auth.logout.button")}
                </Link>
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        ) : (
          <Button variant="ghost" size="sm" asChild>
            <Link href={`/${locale}/login`}>Giriş Yap</Link>
          </Button>
        )}
      </div>
    </header>
  );
}
