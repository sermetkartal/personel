"use client";

/**
 * Locale switcher — swaps TR ↔ EN by rewriting the locale segment of the
 * current URL. Uses next-intl's cookie contract (`NEXT_LOCALE`) so that
 * subsequent server-rendered requests honour the selection.
 *
 * Keyboard-accessible via the shadcn DropdownMenu primitive (Radix), which
 * implements roving tabindex + arrow-key navigation natively — meeting
 * WCAG 2.1.1 (Keyboard) without custom code.
 */

import { usePathname, useRouter } from "next/navigation";
import { useParams } from "next/navigation";
import { useTransition } from "react";
import { Globe, Check } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

const LOCALES = [
  { code: "tr", label: "Türkçe" },
  { code: "en", label: "English" },
] as const;

type LocaleCode = (typeof LOCALES)[number]["code"];

export function LocaleSwitcher(): JSX.Element {
  const router = useRouter();
  const pathname = usePathname();
  const params = useParams();
  const [isPending, startTransition] = useTransition();
  const current = (params.locale as LocaleCode | undefined) ?? "tr";

  function switchTo(target: LocaleCode) {
    if (target === current) return;
    // Persist preference in cookie (next-intl expects NEXT_LOCALE).
    // Cookie lives for 1 year so operator-selected language survives reloads.
    document.cookie = `NEXT_LOCALE=${target};path=/;max-age=31536000;SameSite=Lax`;
    // Rewrite locale segment in the current pathname.
    const segments = pathname.split("/");
    if (segments[1] === current || segments[1] === "tr" || segments[1] === "en") {
      segments[1] = target;
    } else {
      segments.splice(1, 0, target);
    }
    const newPath = segments.join("/") || `/${target}`;
    startTransition(() => {
      router.replace(newPath);
      router.refresh();
    });
  }

  const currentLabel = LOCALES.find((l) => l.code === current)?.label ?? "TR";

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="sm"
          aria-label={`Dil: ${currentLabel}`}
          disabled={isPending}
          className="gap-1.5"
        >
          <Globe className="h-4 w-4" aria-hidden="true" />
          <span className="hidden sm:inline text-xs uppercase">{current}</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-36">
        {LOCALES.map((locale) => (
          <DropdownMenuItem
            key={locale.code}
            onClick={() => switchTo(locale.code)}
            className="flex items-center justify-between"
          >
            <span>{locale.label}</span>
            {current === locale.code && (
              <Check className="h-4 w-4" aria-hidden="true" />
            )}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
