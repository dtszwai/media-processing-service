/**
 * Formatting utilities
 */

/**
 * Format file size in human-readable format
 */
export function formatFileSize(bytes: number): string {
  if (bytes < 1024) return bytes + " B";
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + " KB";
  if (bytes < 1024 * 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(1) + " MB";
  return (bytes / (1024 * 1024 * 1024)).toFixed(2) + " GB";
}

/**
 * Format ISO timestamp as relative time (e.g., "5m ago", "2h ago")
 */
export function formatRelativeTime(isoString: string): string {
  const date = new Date(isoString);
  const now = new Date();
  const diff = Math.floor((now.getTime() - date.getTime()) / 1000);

  if (diff < 60) return "Just now";
  if (diff < 3600) return Math.floor(diff / 60) + "m ago";
  if (diff < 86400) return Math.floor(diff / 3600) + "h ago";
  if (diff < 604800) return Math.floor(diff / 86400) + "d ago";
  return date.toLocaleDateString();
}

/**
 * Format ISO timestamp as localized date/time string
 */
export function formatDateTime(isoString: string | undefined): string {
  if (!isoString) return "N/A";
  const date = new Date(isoString);
  return date.toLocaleString();
}

/**
 * Format number with commas (e.g., 1234567 -> "1,234,567")
 */
export function formatNumber(num: number): string {
  return num.toLocaleString();
}
