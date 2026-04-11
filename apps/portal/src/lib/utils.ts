import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";
import { format, formatDistanceToNow, differenceInDays, parseISO } from "date-fns";
import { tr, enUS } from "date-fns/locale";

export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}

// ── Date formatting ───────────────────────────────────────────────────────────

type SupportedLocale = "tr" | "en";

function getDateLocale(locale: SupportedLocale) {
  return locale === "tr" ? tr : enUS;
}

export function formatDate(
  dateString: string,
  locale: SupportedLocale = "tr"
): string {
  const date = typeof dateString === "string" ? parseISO(dateString) : dateString;
  return format(date, "d MMMM yyyy", { locale: getDateLocale(locale) });
}

export function formatDateTime(
  dateString: string,
  locale: SupportedLocale = "tr"
): string {
  const date = typeof dateString === "string" ? parseISO(dateString) : dateString;
  return format(date, "d MMMM yyyy, HH:mm", { locale: getDateLocale(locale) });
}

export function formatRelative(
  dateString: string,
  locale: SupportedLocale = "tr"
): string {
  const date = typeof dateString === "string" ? parseISO(dateString) : dateString;
  return formatDistanceToNow(date, {
    addSuffix: true,
    locale: getDateLocale(locale),
  });
}

// ── SLA helpers ───────────────────────────────────────────────────────────────

export function getDaysRemaining(slaDeadline: string): number {
  return differenceInDays(parseISO(slaDeadline), new Date());
}

export function getSLAProgress(createdAt: string, slaDeadline: string): number {
  const total = differenceInDays(parseISO(slaDeadline), parseISO(createdAt));
  const elapsed = differenceInDays(new Date(), parseISO(createdAt));
  return Math.min(100, Math.max(0, (elapsed / total) * 100));
}

// ── Duration formatting ───────────────────────────────────────────────────────

export function formatDurationSeconds(
  seconds: number,
  locale: SupportedLocale = "tr"
): string {
  if (seconds < 60) {
    return locale === "tr" ? `${seconds} saniye` : `${seconds} seconds`;
  }
  const minutes = Math.floor(seconds / 60);
  const remainingSeconds = seconds % 60;
  if (locale === "tr") {
    return remainingSeconds > 0
      ? `${minutes} dk ${remainingSeconds} sn`
      : `${minutes} dakika`;
  }
  return remainingSeconds > 0
    ? `${minutes} min ${remainingSeconds} sec`
    : `${minutes} minutes`;
}

// ── String utilities ──────────────────────────────────────────────────────────

export function truncate(str: string, maxLength: number): string {
  if (str.length <= maxLength) return str;
  return `${str.slice(0, maxLength - 3)}...`;
}
