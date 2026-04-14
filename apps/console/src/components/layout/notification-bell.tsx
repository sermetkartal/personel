"use client";

/**
 * Header notification bell with popover list + unread badge.
 *
 * Polls `/v1/notifications?unread=true` every 30 seconds. The Admin API
 * endpoint may not exist in Phase 1 — a 404 is treated as "empty list"
 * rather than an error so the UI stays quiet until the backend lands.
 *
 * All notification types render with a localized title + relative time.
 * Mark-read is optimistic: we invalidate the query on success to settle
 * drift in case the backend state changed.
 */

import { useMemo } from "react";
import Link from "next/link";
import { useTranslations } from "next-intl";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  Bell,
  AlertTriangle,
  FileText,
  Eye,
  ShieldAlert,
  HardDrive,
  ShieldOff,
  Check,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  listNotifications,
  markNotificationRead,
  markAllNotificationsRead,
  notificationKeys,
  type Notification,
  type NotificationType,
} from "@/lib/api/notifications";
import { ApiError } from "@/lib/api/client";
import { cn } from "@/lib/utils";
import { toast } from "sonner";
import { toUserFacingError } from "@/lib/errors";

const ICON_BY_TYPE: Record<NotificationType, React.ElementType> = {
  dsr_new: FileText,
  dsr_overdue: AlertTriangle,
  live_view_request: Eye,
  policy_violation: ShieldAlert,
  tamper_alert: ShieldOff,
  backup_failed: HardDrive,
};

const ICON_COLOR_BY_TYPE: Record<NotificationType, string> = {
  dsr_new: "text-blue-500",
  dsr_overdue: "text-amber-500",
  live_view_request: "text-purple-500",
  policy_violation: "text-red-500",
  tamper_alert: "text-destructive",
  backup_failed: "text-destructive",
};

interface RelativeParts {
  key: "justNow" | "minutesAgo" | "hoursAgo" | "daysAgo";
  n: number;
}

function relativeParts(iso: string): RelativeParts {
  const then = new Date(iso).getTime();
  const now = Date.now();
  const seconds = Math.max(0, Math.round((now - then) / 1000));
  if (seconds < 60) return { key: "justNow", n: 0 };
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return { key: "minutesAgo", n: minutes };
  const hours = Math.round(minutes / 60);
  if (hours < 24) return { key: "hoursAgo", n: hours };
  const days = Math.round(hours / 24);
  return { key: "daysAgo", n: days };
}

export function NotificationBell(): JSX.Element {
  const t = useTranslations("notifications");
  const qc = useQueryClient();

  const { data } = useQuery({
    queryKey: notificationKeys.list({ unread: true }),
    queryFn: async () => {
      try {
        return await listNotifications({ unread: true });
      } catch (err) {
        // 404/501 = endpoint not yet implemented; treat as empty list quietly.
        if (err instanceof ApiError && (err.status === 404 || err.status === 501)) {
          return { items: [], pagination: { page: 1, page_size: 10, total: 0 } };
        }
        throw err;
      }
    },
    refetchInterval: 30_000,
    refetchOnWindowFocus: true,
    staleTime: 15_000,
  });

  const markReadMutation = useMutation({
    mutationFn: (id: string) => markNotificationRead(id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: notificationKeys.all });
    },
    onError: (err) => {
      const ue = toUserFacingError(err);
      toast.error(ue.title, { description: ue.description });
    },
  });

  const markAllMutation = useMutation({
    mutationFn: () => markAllNotificationsRead(),
    onSuccess: () => {
      toast.success(t("allMarkedRead"));
      void qc.invalidateQueries({ queryKey: notificationKeys.all });
    },
    onError: (err) => {
      const ue = toUserFacingError(err);
      toast.error(ue.title, { description: ue.description });
    },
  });

  const notifications = useMemo(() => data?.items ?? [], [data]);
  const unreadCount = notifications.filter((n: Notification) => !n.read_at).length;

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          aria-label={t("ariaLabel", { count: unreadCount })}
          className="relative"
        >
          <Bell className="h-4 w-4" aria-hidden="true" />
          {unreadCount > 0 && (
            <Badge
              variant="destructive"
              className="absolute -right-1 -top-1 h-4 min-w-[1rem] rounded-full px-1 text-[10px] leading-none"
              aria-hidden="true"
            >
              {unreadCount > 9 ? "9+" : unreadCount}
            </Badge>
          )}
          <span className="sr-only">
            {unreadCount > 0
              ? t("unreadCount", { count: unreadCount })
              : t("noUnread")}
          </span>
        </Button>
      </PopoverTrigger>
      <PopoverContent
        align="end"
        sideOffset={8}
        className="w-[360px] p-0"
      >
        <div className="flex items-center justify-between border-b px-4 py-3">
          <h2 className="text-sm font-semibold">{t("title")}</h2>
          {unreadCount > 0 && (
            <Button
              variant="ghost"
              size="sm"
              className="h-7 text-xs"
              onClick={() => markAllMutation.mutate()}
              disabled={markAllMutation.isPending}
            >
              <Check className="mr-1 h-3 w-3" aria-hidden="true" />
              {t("markAllRead")}
            </Button>
          )}
        </div>
        <ScrollArea className="max-h-[420px]">
          {notifications.length === 0 ? (
            <div className="flex flex-col items-center justify-center gap-2 py-10 text-center">
              <Bell className="h-8 w-8 text-muted-foreground/30" aria-hidden="true" />
              <p className="text-sm text-muted-foreground">{t("empty")}</p>
            </div>
          ) : (
            <ul className="divide-y" role="list">
              {notifications.slice(0, 10).map((n: Notification) => {
                const Icon = ICON_BY_TYPE[n.type] ?? Bell;
                const color = ICON_COLOR_BY_TYPE[n.type] ?? "text-muted-foreground";
                const title = t(`types.${n.type}.title`);
                const rel = relativeParts(n.created_at);
                const relLabel =
                  rel.key === "justNow" ? t("justNow") : t(rel.key, { n: rel.n });
                return (
                  <li
                    key={n.id}
                    className={cn(
                      "flex gap-3 px-4 py-3 transition-colors hover:bg-muted/50",
                      !n.read_at && "bg-primary/5",
                    )}
                  >
                    <Icon
                      className={cn("mt-0.5 h-4 w-4 shrink-0", color)}
                      aria-hidden="true"
                    />
                    <div className="flex-1 space-y-1 min-w-0">
                      <p className="text-sm font-medium leading-snug">
                        {n.title || title}
                      </p>
                      {n.body && (
                        <p className="text-xs text-muted-foreground line-clamp-2">
                          {n.body}
                        </p>
                      )}
                      <div className="flex items-center justify-between gap-2">
                        <time
                          dateTime={n.created_at}
                          className="text-[10px] uppercase text-muted-foreground"
                        >
                          {relLabel}
                        </time>
                        {n.link && (
                          <Link
                            href={n.link}
                            className="text-xs font-medium text-primary hover:underline"
                          >
                            {t("view")}
                          </Link>
                        )}
                      </div>
                    </div>
                    {!n.read_at && (
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-6 w-6 shrink-0"
                        aria-label={t("markRead")}
                        title={t("markRead")}
                        onClick={() => markReadMutation.mutate(n.id)}
                        disabled={markReadMutation.isPending}
                      >
                        <Check className="h-3 w-3" aria-hidden="true" />
                      </Button>
                    )}
                  </li>
                );
              })}
            </ul>
          )}
        </ScrollArea>
      </PopoverContent>
    </Popover>
  );
}
