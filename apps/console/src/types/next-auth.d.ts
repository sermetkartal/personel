/**
 * Type extensions for the session object.
 * We use a custom session implementation (not next-auth),
 * so this file just exports the session types for consumers.
 */

import type { Role } from "@/lib/api/types";

export interface SessionUser {
  id: string;
  tenant_id: string;
  username: string;
  email: string;
  role: Role;
}

export interface Session {
  user: SessionUser;
  access_token: string;
  refresh_token: string;
  expires_at: number; // Unix timestamp
}
