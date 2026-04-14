import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";
import { formatDistanceToNow, format } from "date-fns";
import { tr } from "date-fns/locale";

export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}

/**
 * Format a date string for display in Turkish locale.
 * Returns "—" for null/undefined/invalid input rather than throwing
 * RangeError — the UI gracefully degrades when the backend omits a
 * timestamp (e.g. endpoint has never reported in yet).
 */
export function formatDateTR(
  date: string | Date | null | undefined,
  pattern = "d MMMM yyyy, HH:mm",
): string {
  if (date == null) return "—";
  const d = typeof date === "string" ? new Date(date) : date;
  if (isNaN(d.getTime())) return "—";
  return format(d, pattern, { locale: tr });
}

/**
 * Format a date as relative time ("2 saat önce") in Turkish.
 * Returns "—" for null/undefined/invalid input.
 */
export function formatRelativeTR(date: string | Date | null | undefined): string {
  if (date == null) return "—";
  const d = typeof date === "string" ? new Date(date) : date;
  if (isNaN(d.getTime())) return "—";
  return formatDistanceToNow(d, { addSuffix: true, locale: tr });
}

/**
 * Format seconds as "Xsa Ydak Zsn" (Turkish abbreviations).
 */
export function formatDurationTR(seconds: number): string {
  if (seconds < 60) return `${seconds}sn`;

  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = seconds % 60;

  const parts: string[] = [];
  if (h > 0) parts.push(`${h}sa`);
  if (m > 0) parts.push(`${m}dk`);
  if (s > 0 && h === 0) parts.push(`${s}sn`);

  return parts.join(" ");
}

/**
 * Truncate a UUID for compact display.
 */
export function shortId(uuid: string): string {
  return uuid.slice(0, 8);
}

/**
 * Format bytes into human-readable string.
 */
export function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const k = 1024;
  const sizes = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  const size = sizes[i];
  return size ? `${parseFloat((bytes / Math.pow(k, i)).toFixed(2))} ${size}` : "0 B";
}

/**
 * Derive SLA status color from days elapsed.
 * Matches the DSR SLA timeline design:
 * - 0-19 days: safe (green)
 * - 20-27 days: warning (amber)
 * - 28-29 days: critical (orange)
 * - 30+ days: breach (red)
 */
export function slaStatusFromDays(daysElapsed: number): "safe" | "warning" | "critical" | "breach" {
  if (daysElapsed >= 30) return "breach";
  if (daysElapsed >= 28) return "critical";
  if (daysElapsed >= 20) return "warning";
  return "safe";
}

export const SLA_STATUS_COLORS = {
  safe: "text-green-600 bg-green-50 border-green-200",
  warning: "text-amber-600 bg-amber-50 border-amber-200",
  critical: "text-orange-600 bg-orange-50 border-orange-200",
  breach: "text-red-600 bg-red-50 border-red-200",
} as const;

/**
 * Truncate long strings with an ellipsis.
 */
export function truncate(str: string, maxLength = 40): string {
  if (str.length <= maxLength) return str;
  return str.slice(0, maxLength - 1) + "…";
}

/**
 * Convert a snake_case key to a display-friendly Turkish label.
 * Used for dynamic audit payload display.
 */
export function snakeToTitle(str: string): string {
  return str
    .split("_")
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
    .join(" ");
}
