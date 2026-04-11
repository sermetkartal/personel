import type { ReactNode } from "react";

/**
 * Empty state route group layout — no sidebar or header.
 * Used for unauthorized and other full-page states.
 */
export default function EmptyStateLayout({
  children,
}: {
  children: ReactNode;
}): JSX.Element {
  return <>{children}</>;
}
