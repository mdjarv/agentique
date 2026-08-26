/**
 * How loud the microphone is, right now.
 *
 * Deliberately *not* store state. A level arrives about thirty times a second,
 * and pushing that through zustand would re-render every component subscribed
 * to the call for a value only a five-segment meter reads. So it lives in a
 * module-level cell: capture writes it, a meter reads it on its own
 * requestAnimationFrame, and nothing in between re-renders.
 *
 * The cell also expires. A reader has no way to know that capture stopped —
 * the last frame simply never comes — so a stale value would leave a meter
 * frozen mid-bar on a call that has ended. Reading a value older than
 * `LEVEL_STALE_MS` returns silence instead.
 */

/** Nothing has been heard for this long: report silence, not the last frame. */
const LEVEL_STALE_MS = 300;

let level = 0;
let publishedAt = 0;

/**
 * Peak amplitude of one Int16 PCM frame, as 0..1.
 *
 * Peak rather than RMS: this is a liveness cue, and RMS over a 32 ms batch of
 * speech reads almost flat — the meter has to move when the caller talks.
 * The square root opens up the quiet end, where conversational speech lives.
 */
export function frameLevel(frame: ArrayBuffer): number {
  const samples = new Int16Array(frame);
  let peak = 0;
  for (let i = 0; i < samples.length; i++) {
    const v = samples[i] ?? 0;
    const abs = v < 0 ? -v : v;
    if (abs > peak) peak = abs;
  }
  return Math.sqrt(Math.min(1, peak / 32768));
}

/** Records the level of the frame just captured. */
export function publishMicLevel(next: number): void {
  level = next;
  publishedAt = Date.now();
}

/** The current level, or 0 once the frames stop arriving. */
export function readMicLevel(): number {
  if (Date.now() - publishedAt > LEVEL_STALE_MS) return 0;
  return level;
}

/** Drops back to silence — the microphone is closed, not merely quiet. */
export function resetMicLevel(): void {
  level = 0;
  publishedAt = 0;
}
