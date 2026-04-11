/**
 * next-intl navigation helpers — type-safe Link, redirect, useRouter etc.
 * that are locale-aware and use the routing configuration.
 */

import { createNavigation } from "next-intl/navigation";
import { routing } from "./routing";

export const { Link, redirect, useRouter, usePathname, getPathname } =
  createNavigation(routing);
