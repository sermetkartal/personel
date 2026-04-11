"use client";

import { useTranslations } from "next-intl";
import type { Role } from "@/lib/api/types";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";

interface RoleBadgeProps {
  role: Role;
  className?: string;
  size?: "sm" | "md";
}

const ROLE_STYLES: Record<Role, string> = {
  admin: "bg-purple-100 text-purple-700 border-purple-200 dark:bg-purple-900/30 dark:text-purple-400",
  manager: "bg-blue-100 text-blue-700 border-blue-200 dark:bg-blue-900/30 dark:text-blue-400",
  hr: "bg-pink-100 text-pink-700 border-pink-200 dark:bg-pink-900/30 dark:text-pink-400",
  dpo: "bg-orange-100 text-orange-700 border-orange-200 dark:bg-orange-900/30 dark:text-orange-400",
  investigator: "bg-red-100 text-red-700 border-red-200 dark:bg-red-900/30 dark:text-red-400",
  auditor: "bg-teal-100 text-teal-700 border-teal-200 dark:bg-teal-900/30 dark:text-teal-400",
  employee: "bg-gray-100 text-gray-700 border-gray-200 dark:bg-gray-800/50 dark:text-gray-400",
};

export function RoleBadge({ role, className, size = "md" }: RoleBadgeProps): JSX.Element {
  const t = useTranslations("common.roles");

  return (
    <Badge
      variant="outline"
      className={cn(
        ROLE_STYLES[role],
        size === "sm" ? "text-xs px-1.5 py-0" : "",
        className,
      )}
      aria-label={`Rol: ${t(role)}`}
    >
      {t(role)}
    </Badge>
  );
}
