"use client";

import { useEffect, useRef, useState } from "react";
import { useLocale } from "next-intl";
import { Info } from "lucide-react";
import { cn } from "@/lib/utils";
import {
  type LegalTermKey,
  LEGAL_TERMS_TR,
  LEGAL_TERMS_EN,
} from "@/lib/legal-terms";

interface LegalTermProps {
  termKey: LegalTermKey;
  children?: React.ReactNode;
  className?: string;
}

/**
 * Wraps a KVKK legal term with an accessible hover/click tooltip
 * showing a plain-Turkish (or plain-English) explanation.
 *
 * Uses a fully custom tooltip to avoid any radix-ui version conflicts
 * and to ensure keyboard accessibility on older Edge/Firefox.
 */
export function LegalTerm({
  termKey,
  children,
  className,
}: LegalTermProps): JSX.Element {
  const locale = useLocale();
  const terms = locale === "tr" ? LEGAL_TERMS_TR : LEGAL_TERMS_EN;
  const termData = terms[termKey];

  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLSpanElement>(null);

  // Close tooltip when clicking outside
  useEffect(() => {
    if (!open) return;

    function handleClickOutside(e: MouseEvent): void {
      if (
        containerRef.current &&
        !containerRef.current.contains(e.target as Node)
      ) {
        setOpen(false);
      }
    }

    document.addEventListener("mousedown", handleClickOutside);
    return () => document.removeEventListener("mousedown", handleClickOutside);
  }, [open]);

  // Close on Escape
  useEffect(() => {
    if (!open) return;

    function handleEscape(e: KeyboardEvent): void {
      if (e.key === "Escape") setOpen(false);
    }

    document.addEventListener("keydown", handleEscape);
    return () => document.removeEventListener("keydown", handleEscape);
  }, [open]);

  return (
    <span ref={containerRef} className={cn("relative inline-block", className)}>
      <span
        className={cn(
          "inline-flex items-center gap-0.5 cursor-help",
          "underline decoration-dotted decoration-portal-400 underline-offset-2",
          "text-portal-700 hover:text-portal-900 transition-colors"
        )}
        onMouseEnter={() => setOpen(true)}
        onMouseLeave={() => setOpen(false)}
        onClick={() => setOpen((v) => !v)}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            setOpen((v) => !v);
          }
        }}
        role="button"
        tabIndex={0}
        aria-expanded={open}
        aria-label={`${termData.term} — açıklama`}
      >
        {children ?? termData.term}
        <Info
          className="inline w-3.5 h-3.5 text-portal-400 flex-shrink-0"
          aria-hidden="true"
        />
      </span>

      {open && (
        <span
          role="tooltip"
          className={cn(
            "absolute z-50 bottom-full left-0 mb-2 w-72",
            "bg-white border border-warm-200 rounded-xl shadow-card-hover",
            "p-4 text-sm text-warm-700 leading-relaxed",
            "animate-fade-in"
          )}
        >
          <strong className="block text-portal-700 font-medium mb-1.5 text-xs uppercase tracking-wide">
            {termData.term}
          </strong>
          <span>{termData.plain}</span>
          {/* Tooltip arrow */}
          <span
            aria-hidden="true"
            className="absolute -bottom-1.5 left-4 w-3 h-3 bg-white border-b border-r border-warm-200 rotate-45"
          />
        </span>
      )}
    </span>
  );
}
