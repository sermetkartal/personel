"use client";

/**
 * DSR Fulfillment Action Buttons
 * Provides Respond, Reject, and Extend SLA actions.
 * Role-gated: only DPO can use these.
 */

import { useState } from "react";
import { useTranslations } from "next-intl";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { rejectDSR, extendDSR, dsrKeys } from "@/lib/api/dsr";
import { toUserFacingError } from "@/lib/errors";
import { toast } from "sonner";
import { DSRResponseForm } from "./response-form";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { CheckCircle, XCircle, CalendarPlus } from "lucide-react";
import type { DSRState } from "@/lib/api/types";

interface FulfillmentActionsProps {
  dsrId: string;
  state: DSRState;
}

export function DSRFulfillmentActions({ dsrId, state }: FulfillmentActionsProps): JSX.Element {
  const t = useTranslations("dsr.actions");
  const qc = useQueryClient();

  const [respondOpen, setRespondOpen] = useState(false);
  const [rejectOpen, setRejectOpen] = useState(false);
  const [extendOpen, setExtendOpen] = useState(false);
  const [rejectReason, setRejectReason] = useState("");
  const [extendJustification, setExtendJustification] = useState("");

  const rejectMutation = useMutation({
    mutationFn: () => rejectDSR(dsrId, { reason: rejectReason }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: dsrKeys.detail(dsrId) });
      void qc.invalidateQueries({ queryKey: dsrKeys.all });
      toast.success(t("rejectSuccess"));
      setRejectOpen(false);
      setRejectReason("");
    },
    onError: (err) => {
      const ufe = toUserFacingError(err);
      toast.error(ufe.title, { description: ufe.description });
    },
  });

  const extendMutation = useMutation({
    mutationFn: () => extendDSR(dsrId, { justification: extendJustification }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: dsrKeys.detail(dsrId) });
      toast.success(t("extendSuccess"));
      setExtendOpen(false);
      setExtendJustification("");
    },
    onError: (err) => {
      const ufe = toUserFacingError(err);
      toast.error(ufe.title, { description: ufe.description });
    },
  });

  // These actions are only relevant for open/at_risk/overdue states
  const isActionable = state === "open" || state === "at_risk" || state === "overdue";

  if (!isActionable) {
    return (
      <p className="text-sm text-muted-foreground">
        {t("noActions", { state })}
      </p>
    );
  }

  return (
    <>
      <div className="flex flex-wrap gap-2">
        <Button
          size="sm"
          onClick={() => setRespondOpen(true)}
        >
          <CheckCircle className="mr-2 h-4 w-4" aria-hidden="true" />
          {t("respond")}
        </Button>
        <Button
          size="sm"
          variant="outline"
          onClick={() => setExtendOpen(true)}
        >
          <CalendarPlus className="mr-2 h-4 w-4" aria-hidden="true" />
          {t("extend")}
        </Button>
        <Button
          size="sm"
          variant="destructive"
          onClick={() => setRejectOpen(true)}
        >
          <XCircle className="mr-2 h-4 w-4" aria-hidden="true" />
          {t("reject")}
        </Button>
      </div>

      {/* Respond dialog */}
      <Dialog open={respondOpen} onOpenChange={setRespondOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>{t("respondTitle")}</DialogTitle>
            <DialogDescription>{t("respondDesc")}</DialogDescription>
          </DialogHeader>
          <DSRResponseForm dsrId={dsrId} onSuccess={() => setRespondOpen(false)} />
        </DialogContent>
      </Dialog>

      {/* Reject dialog */}
      <Dialog open={rejectOpen} onOpenChange={setRejectOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("rejectTitle")}</DialogTitle>
            <DialogDescription>{t("rejectDesc")}</DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor="reject-reason">{t("rejectReasonLabel")}</Label>
            <Input
              id="reject-reason"
              value={rejectReason}
              onChange={(e) => setRejectReason(e.target.value)}
              placeholder={t("rejectReasonPlaceholder")}
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRejectOpen(false)}>
              {t("cancel")}
            </Button>
            <Button
              variant="destructive"
              disabled={!rejectReason.trim() || rejectMutation.isPending}
              onClick={() => void rejectMutation.mutateAsync()}
            >
              {rejectMutation.isPending ? t("rejecting") : t("confirmReject")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Extend dialog */}
      <Dialog open={extendOpen} onOpenChange={setExtendOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("extendTitle")}</DialogTitle>
            <DialogDescription>{t("extendDesc")}</DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor="extend-just">{t("extendJustLabel")}</Label>
            <Input
              id="extend-just"
              value={extendJustification}
              onChange={(e) => setExtendJustification(e.target.value)}
              placeholder={t("extendJustPlaceholder")}
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setExtendOpen(false)}>
              {t("cancel")}
            </Button>
            <Button
              disabled={!extendJustification.trim() || extendMutation.isPending}
              onClick={() => void extendMutation.mutateAsync()}
            >
              {extendMutation.isPending ? t("extending") : t("confirmExtend")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
