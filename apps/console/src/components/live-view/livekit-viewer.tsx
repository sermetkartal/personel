"use client";

/**
 * LiveKit Session Viewer
 *
 * Security constraints:
 * - viewer_token is passed as a prop — never stored in localStorage
 * - View-only: no publish permissions requested
 * - Time cap countdown prominently shown
 * - HR can terminate from this view (dual-control action)
 * - Screen capture of the viewer page is not possible to prevent
 *   from the browser side, but the audit trail records all access
 */

import { useEffect, useRef, useState, useCallback } from "react";
import { useTranslations } from "next-intl";
import { useRouter } from "next/navigation";
import { Room, RoomEvent, ConnectionState } from "livekit-client";
import { toast } from "sonner";
import { Monitor, TimerOff, StopCircle, Loader2, WifiOff } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useTerminateLiveViewSession } from "@/lib/hooks/use-live-view";
import { formatDurationTR } from "@/lib/utils";
import { toUserFacingError } from "@/lib/errors";
import { cn } from "@/lib/utils";
import type { LiveViewSession } from "@/lib/api/types";
import { can } from "@/lib/auth/rbac";
import type { Role } from "@/lib/api/types";

interface LiveKitViewerProps {
  session: LiveViewSession;
  livekitUrl: string;
  userRole: Role;
}

type ConnectionStatus = "connecting" | "connected" | "disconnected" | "reconnecting";

export function LiveKitViewer({
  session,
  livekitUrl,
  userRole,
}: LiveKitViewerProps): JSX.Element {
  const t = useTranslations("liveView.viewer");
  const router = useRouter();
  const containerRef = useRef<HTMLDivElement>(null);
  const roomRef = useRef<Room | null>(null);

  const [connectionStatus, setConnectionStatus] = useState<ConnectionStatus>("connecting");
  const [secondsRemaining, setSecondsRemaining] = useState(session.time_cap_seconds);
  const [terminateOpen, setTerminateOpen] = useState(false);
  const [terminateReason, setTerminateReason] = useState("");

  const terminateMutation = useTerminateLiveViewSession();
  const canTerminate = can(userRole, "terminate:live-view");

  // Countdown timer
  useEffect(() => {
    if (secondsRemaining <= 0) return;
    const interval = setInterval(() => {
      setSecondsRemaining((prev) => {
        if (prev <= 1) {
          clearInterval(interval);
          return 0;
        }
        return prev - 1;
      });
    }, 1000);
    return () => clearInterval(interval);
  }, [secondsRemaining]);

  // Warn when 2 minutes remain
  useEffect(() => {
    if (secondsRemaining === 120) {
      toast.warning(t("timeWarning"));
    }
  }, [secondsRemaining, t]);

  // Detect placeholder/unreachable LiveKit URLs. In the vm3 dev pilot the
  // SFU is not deployed, so we render a clear "simulation" placeholder
  // instead of bubbling up a misleading connection error toast.
  const livekitAvailable =
    livekitUrl && !livekitUrl.includes("localhost:7880") && !livekitUrl.startsWith("disabled://");

  // Connect to LiveKit room
  useEffect(() => {
    if (!livekitAvailable) {
      setConnectionStatus("disconnected");
      return;
    }

    const room = new Room({
      // View-only: do not request publish permissions
      adaptiveStream: true,
      dynacast: true,
    });

    roomRef.current = room;

    room.on(RoomEvent.Connected, () => setConnectionStatus("connected"));
    room.on(RoomEvent.Disconnected, () => {
      setConnectionStatus("disconnected");
      // Session ended — navigate back
      setTimeout(() => router.push("../"), 2000);
    });
    room.on(RoomEvent.Reconnecting, () => setConnectionStatus("reconnecting"));
    room.on(RoomEvent.Reconnected, () => setConnectionStatus("connected"));

    void room.connect(livekitUrl, session.viewer_token).catch((err: unknown) => {
      const ue = toUserFacingError(err);
      toast.error(ue.title, { description: ue.description });
      setConnectionStatus("disconnected");
    });

    return () => {
      void room.disconnect();
      roomRef.current = null;
    };
    // viewer_token intentionally not in deps — it's a one-time credential
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [livekitUrl, router, livekitAvailable]);

  const handleTerminate = useCallback(async () => {
    if (!terminateReason.trim()) return;
    try {
      await terminateMutation.mutateAsync({
        sessionId: session.id,
        reason: terminateReason.trim(),
      });
      toast.success(t("terminatedToast"));
      router.push("../");
    } catch (err) {
      const ue = toUserFacingError(err);
      toast.error(ue.title, { description: ue.description });
    }
  }, [terminateReason, terminateMutation, session.id, router, t]);

  const isTimeCritical = secondsRemaining <= 120;

  return (
    <div className="flex flex-col gap-4">
      {/* Status bar */}
      <div className="flex items-center justify-between gap-4 rounded-lg border bg-card px-4 py-3">
        {/* Connection badge */}
        <div className="flex items-center gap-2">
          {connectionStatus === "connecting" || connectionStatus === "reconnecting" ? (
            <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" aria-hidden="true" />
          ) : connectionStatus === "connected" ? (
            <Monitor className="h-4 w-4 text-green-500" aria-hidden="true" />
          ) : (
            <WifiOff className="h-4 w-4 text-destructive" aria-hidden="true" />
          )}
          <Badge
            variant={
              connectionStatus === "connected"
                ? "success"
                : connectionStatus === "disconnected"
                  ? "destructive"
                  : "warning"
            }
            className="text-xs"
          >
            {t(`status.${connectionStatus}`)}
          </Badge>
        </div>

        {/* Time cap countdown */}
        <div
          className={cn(
            "flex items-center gap-2 text-sm font-mono tabular-nums",
            isTimeCritical && "text-destructive font-semibold",
            secondsRemaining === 0 && "text-muted-foreground",
          )}
          role="timer"
          aria-live={isTimeCritical ? "polite" : "off"}
          aria-label={`${t("timeRemaining")}: ${formatDurationTR(secondsRemaining)}`}
        >
          <TimerOff
            className={cn("h-4 w-4", isTimeCritical && "text-destructive")}
            aria-hidden="true"
          />
          <span>
            {secondsRemaining > 0
              ? formatDurationTR(secondsRemaining)
              : t("sessionEnded")}
          </span>
        </div>

        {/* Terminate button (HR/DPO only) */}
        {canTerminate && (
          <Button
            variant="destructive"
            size="sm"
            onClick={() => setTerminateOpen(true)}
            disabled={connectionStatus === "disconnected"}
          >
            <StopCircle className="mr-2 h-4 w-4" aria-hidden="true" />
            {t("terminate")}
          </Button>
        )}
      </div>

      {/* Video container */}
      <div
        ref={containerRef}
        className="relative aspect-video w-full overflow-hidden rounded-lg border bg-black"
        aria-label={t("screenLabel")}
        role="region"
      >
        {/* LiveKit server unavailable (dev pilot) — render a friendly placeholder
            instead of the generic "disconnected" error state. */}
        {!livekitAvailable && (
          <div className="absolute inset-0 flex flex-col items-center justify-center gap-3 bg-muted p-6 text-center">
            <Monitor className="h-10 w-10 text-muted-foreground/40" aria-hidden="true" />
            <p className="text-sm font-medium text-foreground">
              {t("serverUnavailableTitle")}
            </p>
            <p className="max-w-sm text-xs text-muted-foreground">
              {t("serverUnavailableBody")}
            </p>
          </div>
        )}
        {livekitAvailable && connectionStatus === "connecting" && (
          <div className="absolute inset-0 flex flex-col items-center justify-center gap-3 text-white">
            <Loader2 className="h-8 w-8 animate-spin" aria-hidden="true" />
            <p className="text-sm">{t("connecting")}</p>
          </div>
        )}
        {livekitAvailable && connectionStatus === "disconnected" && (
          <div className="absolute inset-0 flex flex-col items-center justify-center gap-3 text-white">
            <WifiOff className="h-8 w-8" aria-hidden="true" />
            <p className="text-sm">{t("disconnected")}</p>
          </div>
        )}
        {/* LiveKit renders video tracks here via the Room's VideoRenderer.
            In a full implementation, use @livekit/components-react <RoomContext.Provider>
            + <VideoTrack> — omitted here to avoid adding another dep. */}
        {connectionStatus === "connected" && (
          <div
            id="livekit-video-mount"
            className="h-full w-full"
            aria-label={t("screenLabel")}
          />
        )}
      </div>

      {/* Terminate dialog */}
      <Dialog open={terminateOpen} onOpenChange={setTerminateOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("terminateTitle")}</DialogTitle>
            <DialogDescription>{t("terminateDesc")}</DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor="terminate-reason">{t("terminateReasonLabel")}</Label>
            <Input
              id="terminate-reason"
              value={terminateReason}
              onChange={(e) => setTerminateReason(e.target.value)}
              placeholder={t("terminateReasonPlaceholder")}
              autoFocus
            />
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setTerminateOpen(false)}
              disabled={terminateMutation.isPending}
            >
              {t("cancel")}
            </Button>
            <Button
              variant="destructive"
              disabled={!terminateReason.trim() || terminateMutation.isPending}
              onClick={() => void handleTerminate()}
            >
              {terminateMutation.isPending ? t("terminating") : t("confirmTerminate")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
