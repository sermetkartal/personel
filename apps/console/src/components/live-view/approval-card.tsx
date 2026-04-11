"use client";

/**
 * Live View Approval Card.
 *
 * Dual-control enforcement:
 * - If the logged-in user is the requester, the Approve button is disabled.
 * - Tooltip explains: "Kendi talebinizi onaylayamazsınız."
 * - The API also enforces this server-side (HTTP 403).
 *
 * This is a KVKK-critical control per live-view-protocol.md.
 */

import { useState } from "react";
import { useTranslations } from "next-intl";
import { toast } from "sonner";
import { CheckCircle, XCircle, User, Monitor, Clock, AlertTriangle } from "lucide-react";
import {
  useApproveLiveView,
  useRejectLiveView,
} from "@/lib/hooks/use-live-view";
import type { LiveViewRequest } from "@/lib/api/types";
import { formatRelativeTR } from "@/lib/utils";
import { toUserFacingError } from "@/lib/errors";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardFooter, CardHeader } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

interface ApprovalCardProps {
  request: LiveViewRequest;
  currentUserId: string;
  endpointHostname?: string;
  requesterName?: string;
  onActionComplete?: () => void;
}

export function ApprovalCard({
  request,
  currentUserId,
  endpointHostname,
  requesterName,
  onActionComplete,
}: ApprovalCardProps): JSX.Element {
  const t = useTranslations("liveView.approval");
  const [showDenyDialog, setShowDenyDialog] = useState(false);
  const [denyReason, setDenyReason] = useState("");

  const approveMutation = useApproveLiveView();
  const rejectMutation = useRejectLiveView();

  // Dual-control guard: requester cannot approve their own request
  const isSelfRequest = request.requester_id === currentUserId;

  const handleApprove = () => {
    approveMutation.mutate(
      { requestId: request.id },
      {
        onSuccess: () => {
          toast.success("Canlı izleme oturumu başlatıldı.");
          onActionComplete?.();
        },
        onError: (err) => {
          const ue = toUserFacingError(err);
          toast.error(ue.description);
        },
      },
    );
  };

  const handleDeny = () => {
    if (!denyReason.trim()) return;
    rejectMutation.mutate(
      { requestId: request.id, reason: denyReason },
      {
        onSuccess: () => {
          toast.success("Talep reddedildi.");
          setShowDenyDialog(false);
          onActionComplete?.();
        },
        onError: (err) => {
          const ue = toUserFacingError(err);
          toast.error(ue.description);
        },
      },
    );
  };

  return (
    <>
      <Card className="border">
        <CardHeader className="pb-3">
          <div className="flex items-start justify-between gap-2">
            <div className="space-y-1">
              {isSelfRequest && (
                <div className="flex items-center gap-1.5 rounded-md bg-amber-50 border border-amber-200 px-2.5 py-1.5 text-xs font-medium text-amber-700 dark:bg-amber-900/20 dark:text-amber-400">
                  <AlertTriangle className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
                  {t("selfApprovalBlocked")}
                </div>
              )}
            </div>
            <Badge variant="warning">
              <Clock className="mr-1 h-3 w-3" aria-hidden="true" />
              Onay Bekliyor
            </Badge>
          </div>
        </CardHeader>

        <CardContent className="space-y-3 text-sm">
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1">
              <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <User className="h-3 w-3" aria-hidden="true" />
                {t("requester")}
              </div>
              <p className="font-medium">
                {requesterName ?? request.requester_id.slice(0, 8) + "..."}
              </p>
            </div>

            <div className="space-y-1">
              <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <Monitor className="h-3 w-3" aria-hidden="true" />
                {t("targetEndpoint")}
              </div>
              <p className="font-medium">
                {endpointHostname ?? request.endpoint_id.slice(0, 8) + "..."}
              </p>
            </div>
          </div>

          <div className="space-y-1">
            <p className="text-xs text-muted-foreground">{t("reasonCode")}</p>
            <code className="font-hash rounded bg-muted px-2 py-1 text-xs">
              {request.reason_code}
            </code>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1">
              <p className="text-xs text-muted-foreground">{t("duration")}</p>
              <p className="font-medium">{request.duration_minutes} dakika</p>
            </div>
            <div className="space-y-1">
              <p className="text-xs text-muted-foreground">{t("requestedAt")}</p>
              <time
                dateTime={request.requested_at}
                className="font-medium"
              >
                {formatRelativeTR(request.requested_at)}
              </time>
            </div>
          </div>
        </CardContent>

        <CardFooter className="flex gap-2 pt-0">
          {/* Deny button — always available to HR */}
          <Button
            variant="outline"
            size="sm"
            onClick={() => setShowDenyDialog(true)}
            disabled={rejectMutation.isPending}
            className="text-destructive border-destructive/30 hover:bg-destructive/5"
            aria-label={`${request.reason_code} talebini reddet`}
          >
            <XCircle className="h-4 w-4" aria-hidden="true" />
            {t("deny")}
          </Button>

          {/* Approve button — disabled for self-requests */}
          <TooltipProvider delayDuration={100}>
            <Tooltip>
              <TooltipTrigger asChild>
                <span tabIndex={isSelfRequest ? 0 : undefined}>
                  <Button
                    size="sm"
                    onClick={handleApprove}
                    disabled={isSelfRequest || approveMutation.isPending}
                    aria-label={
                      isSelfRequest
                        ? t("selfApprovalBlocked")
                        : `${request.reason_code} talebini onayla`
                    }
                    aria-disabled={isSelfRequest}
                  >
                    <CheckCircle className="h-4 w-4" aria-hidden="true" />
                    {approveMutation.isPending ? "Onaylanıyor..." : t("approve")}
                  </Button>
                </span>
              </TooltipTrigger>
              {isSelfRequest && (
                <TooltipContent side="top">
                  {t("selfApprovalTooltip")}
                </TooltipContent>
              )}
            </Tooltip>
          </TooltipProvider>
        </CardFooter>
      </Card>

      {/* Deny reason dialog */}
      <Dialog open={showDenyDialog} onOpenChange={setShowDenyDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("denyConfirm")}</DialogTitle>
            <DialogDescription>
              Talebi reddetmek için bir gerekçe girin.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor="deny-reason">{t("denyReason")}</Label>
            <Input
              id="deny-reason"
              value={denyReason}
              onChange={(e) => setDenyReason(e.target.value)}
              placeholder="Reddetme gerekçesi..."
              autoFocus
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowDenyDialog(false)}>
              İptal
            </Button>
            <Button
              variant="destructive"
              onClick={handleDeny}
              disabled={!denyReason.trim() || rejectMutation.isPending}
            >
              {rejectMutation.isPending ? "Reddediliyor..." : t("deny")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
