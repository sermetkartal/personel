import { defineRouting } from "next-intl/routing";

export const routing = defineRouting({
  locales: ["tr", "en"] as const,
  defaultLocale: "tr",
  localePrefix: "always",
});

export type Locale = (typeof routing.locales)[number];
