import { defineRouting } from "next-intl/routing";

export const routing = defineRouting({
  // Supported locales. To add a new locale:
  // 1. Add the locale code here (e.g. "de")
  // 2. Create messages/<locale>.json mirroring tr.json key structure
  // 3. Optionally add date-fns locale import in components that use formatDate
  locales: ["tr", "en"],

  // Turkish is the mandatory default per product brief
  defaultLocale: "tr",

  // Locale prefix strategy — always include locale in path
  localePrefix: "always",
});

export type Locale = (typeof routing.locales)[number];
