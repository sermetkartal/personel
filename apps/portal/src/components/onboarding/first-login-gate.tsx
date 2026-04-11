"use client";

import { useState } from "react";
import { FirstLoginModal } from "./first-login-modal";

interface FirstLoginGateProps {
  accessToken: string;
  /**
   * Whether to show the first-login modal.
   * Passed from the server layout based on session.firstLoginAcknowledged.
   */
  showModal: boolean;
  children: React.ReactNode;
}

/**
 * Client-side gate that shows the mandatory first-login modal
 * if the employee has not yet acknowledged the disclosure.
 *
 * Per calisan-bilgilendirme-akisi.md Aşama 5: the "Anladım" click is
 * audited via the API. Cannot be dismissed any other way.
 *
 * Accessibility: while the modal is open, the underlying content is
 * rendered but aria-hidden to prevent screen reader access before acknowledgement.
 */
export function FirstLoginGate({
  accessToken,
  showModal,
  children,
}: FirstLoginGateProps): JSX.Element {
  const [acknowledged, setAcknowledged] = useState(!showModal);

  if (acknowledged) {
    return <>{children}</>;
  }

  return (
    <>
      {/* Page content is visually present but inaccessible until acknowledged */}
      <div aria-hidden="true" className="pointer-events-none select-none">
        {children}
      </div>
      <FirstLoginModal
        accessToken={accessToken}
        onAcknowledge={() => setAcknowledged(true)}
      />
    </>
  );
}
