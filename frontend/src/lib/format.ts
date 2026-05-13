/**
 * Format milliseconds into a human-readable duration string.
 * - <1s: "0.Xs"
 * - 1-59s: "Xs" or "X.Xs"
 * - 60s+: "Xm Ys"
 */
export function formatDuration(ms: number): string {
  const totalSeconds = ms / 1000;

  if (totalSeconds < 1) {
    return `${totalSeconds.toFixed(1)}s`;
  }

  if (totalSeconds < 60) {
    const rounded = Math.round(totalSeconds * 10) / 10;
    return rounded % 1 === 0 ? `${rounded}s` : `${rounded.toFixed(1)}s`;
  }

  const minutes = Math.floor(totalSeconds / 60);
  const seconds = Math.round(totalSeconds % 60);
  return seconds === 0 ? `${minutes}m` : `${minutes}m ${seconds}s`;
}

/**
 * Format a Unix-ms timestamp as a short label for chat turn footers.
 * - Same day: "14:32"
 * - Same year, different day: "Mar 4, 14:32"
 * - Older: "Mar 4, 2024, 14:32"
 * Returns "" for null/undefined.
 */
export function formatTurnTime(ts: number | null | undefined, now: number = Date.now()): string {
  if (ts == null) return "";
  const d = new Date(ts);
  const nowDate = new Date(now);
  const time = d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  const sameDay =
    d.getFullYear() === nowDate.getFullYear() &&
    d.getMonth() === nowDate.getMonth() &&
    d.getDate() === nowDate.getDate();
  if (sameDay) return time;
  const sameYear = d.getFullYear() === nowDate.getFullYear();
  const datePart = sameYear
    ? d.toLocaleDateString([], { month: "short", day: "numeric" })
    : d.toLocaleDateString([], { year: "numeric", month: "short", day: "numeric" });
  return `${datePart}, ${time}`;
}
