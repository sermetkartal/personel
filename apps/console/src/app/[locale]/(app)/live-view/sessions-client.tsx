"use client";

import { useTranslations } from "next-intl";
import { Link } from "@/lib/i18n/navigation";
import { useLiveViewSessions, useLiveViewRequests } from "@/lib/hooks/use-live-view";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Monitor, Clock, ExternalLink } from "lucide-react";
import { formatRelativeTR, formatDurationTR } from "@/lib/utils";
import { cn } from "@/lib/utils";
import type { LiveViewState, Role } from "@/lib/api/types";
import { can } from "@/lib/auth/rbac";

interface SessionsClientProps {
  currentUserId: string;
  userRole: Role;
}

function stateVariant(state: LiveViewState): "default" | "success" | "warning" | "destructive" | "info" {
  if (state === "ACTIVE") return "success";
  if (state === "REQUESTED" || state === "APPROVED") return "warning";
  if (state === "DENIED" || state === "TERMINATED_BY_HR" || state === "TERMINATED_BY_DPO") return "destructive";
  return "default";
}

export function LiveViewSessionsClient({ currentUserId, userRole }: SessionsClientProps): JSX.Element {
  const t = useTranslations("liveView");
  const canWatch = can(userRole, "watch:live-view");

  const { data: sessionsData, isLoading: sessionsLoading } = useLiveViewSessions({
    page_size: 20,
  });

  const { data: requestsData, isLoading: requestsLoading } = useLiveViewRequests({
    page_size: 20,
  });

  const sessions = sessionsData?.items ?? [];
  const requests = requestsData?.items ?? [];
  const myRequests = requests.filter((r) => r.requester_id === currentUserId);

  if (sessionsLoading || requestsLoading) {
    return (
      <div className="space-y-3">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-16 w-full rounded-lg" />
        ))}
      </div>
    );
  }

  return (
    <div className="space-y-8">
      {/* Active / recent sessions */}
      <section>
        <h2 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-3">
          {t("activeSessions")}
        </h2>
        {sessions.length === 0 ? (
          <p className="text-sm text-muted-foreground py-4 text-center border border-dashed rounded-lg">
            {t("noActiveSessions")}
          </p>
        ) : (
          <div className="space-y-2">
            {sessions.map((s) => (
              <div
                key={s.id}
                className={cn(
                  "flex items-center gap-4 rounded-lg border bg-card px-4 py-3 text-sm",
                  s.state === "ACTIVE" && "border-green-500/30 bg-green-500/5",
                )}
              >
                <Monitor
                  className={cn(
                    "h-5 w-5 shrink-0",
                    s.state === "ACTIVE" ? "text-green-500" : "text-muted-foreground",
                  )}
                  aria-hidden="true"
                />
                <div className="flex-1 min-w-0">
                  <p className="truncate font-medium font-mono text-xs">{s.livekit_room}</p>
                  <p className="text-xs text-muted-foreground">
                    {t("timeCap")}: {formatDurationTR(s.time_cap_seconds)}
                  </p>
                </div>
                <Badge variant={stateVariant(s.state)} className="text-xs shrink-0">
                  {s.state}
                </Badge>
                <time className="text-xs text-muted-foreground shrink-0">
                  {formatRelativeTR(s.started_at)}
                </time>
                {canWatch && s.state === "ACTIVE" && (
                  <Button size="sm" variant="outline" asChild>
                    <Link href={`/live-view/${s.id}`}>
                      <ExternalLink className="mr-1 h-3.5 w-3.5" aria-hidden="true" />
                      {t("watch")}
                    </Link>
                  </Button>
                )}
              </div>
            ))}
          </div>
        )}
      </section>

      {/* My pending requests */}
      {myRequests.length > 0 && (
        <section>
          <h2 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-3">
            {t("myRequests")}
          </h2>
          <div className="space-y-2">
            {myRequests.map((r) => (
              <div
                key={r.id}
                className="flex items-center gap-4 rounded-lg border bg-card px-4 py-3 text-sm"
              >
                <Clock className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
                <div className="flex-1 min-w-0">
                  <p className="truncate text-xs text-muted-foreground font-mono">{r.id.slice(0, 8)}...</p>
                  <p className="text-xs text-muted-foreground">{r.reason_code} · {r.duration_minutes} {t("minutes")}</p>
                </div>
                <Badge variant={stateVariant(r.state)} className="text-xs shrink-0">
                  {r.state}
                </Badge>
                <time className="text-xs text-muted-foreground shrink-0">
                  {formatRelativeTR(r.requested_at)}
                </time>
              </div>
            ))}
          </div>
        </section>
      )}
    </div>
  );
}
