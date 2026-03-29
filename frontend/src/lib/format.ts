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
