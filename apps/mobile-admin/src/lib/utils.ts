/**
 * Shared utility functions for the Personel Mobile Admin app.
 */

/**
 * Format a UTC ISO date string to a human-readable Turkish locale date.
 */
export function formatDate(iso: string): string {
  return new Date(iso).toLocaleDateString("tr-TR", {
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
  });
}

/**
 * Format a UTC ISO date string to a Turkish locale date + time.
 */
export function formatDateTime(iso: string): string {
  return new Date(iso).toLocaleString("tr-TR", {
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

/**
 * Calculate days remaining until a deadline.
 * Positive = days left. Negative = overdue by that many days.
 */
export function daysUntil(isoDeadline: string): number {
  const now = new Date();
  const deadline = new Date(isoDeadline);
  const diffMs = deadline.getTime() - now.getTime();
  return Math.ceil(diffMs / (1000 * 60 * 60 * 24));
}

/**
 * Format duration in seconds to a human-readable "Xh Ym" or "Ym" string.
 */
export function formatDurationSeconds(seconds: number): string {
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  if (hours > 0) return `${hours}s ${minutes}dk`;
  return `${minutes}dk`;
}

/**
 * Clamp a string to a max length, appending "…" if truncated.
 */
export function truncate(str: string, maxLength: number): string {
  if (str.length <= maxLength) return str;
  return str.slice(0, maxLength - 1) + "…";
}
