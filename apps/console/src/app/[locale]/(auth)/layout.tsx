import type { ReactNode } from "react";

/**
 * Auth route group layout — no sidebar or header.
 * Login and callback pages render their own full-page layouts.
 */
export default function AuthLayout({
  children,
}: {
  children: ReactNode;
}): JSX.Element {
  return <>{children}</>;
}
