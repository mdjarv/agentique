/**
 * The transform behind a zoomable image: scale plus translation, and the rules
 * that keep the two honest. Pure, so the gestures that drive it (pinch, wheel,
 * double-tap, drag) can be tested without a DOM.
 *
 * Coordinates are viewport-centred: (0,0) is the middle of the box the image
 * sits in, which is where `transform-origin: center` puts it. A point p on the
 * screen maps back to `(p - translate) / scale` on the image, and that identity
 * is the whole trick — zooming "about a point" means solving for the
 * translation that leaves it where the finger is.
 */

export interface ZoomTransform {
  scale: number;
  /** Translation in CSS pixels, applied BEFORE the scale. */
  x: number;
  y: number;
}

export interface Box {
  width: number;
  height: number;
}

export const IDENTITY: ZoomTransform = { scale: 1, x: 0, y: 0 };

export const MIN_SCALE = 1;
export const MAX_SCALE = 8;

/** The scale a double-tap jumps to when the image is sitting at rest. */
export const DOUBLE_TAP_SCALE = 2.5;

function clamp(value: number, min: number, max: number): number {
  const clamped = Math.min(max, Math.max(min, value));
  // Clamping a negative toward a zero bound yields -0, which survives into the
  // transform string as "-0px". Same position, needless diff.
  return clamped === 0 ? 0 : clamped;
}

/**
 * Scale by `factor` while keeping the image point currently under
 * (`originX`, `originY`) — viewport-centred coordinates — under it afterwards.
 *
 * Anchoring is what separates a zoom from a jump: a pinch that ignores its
 * midpoint slides the thing you were looking at off the screen.
 */
export function zoomAbout(
  transform: ZoomTransform,
  factor: number,
  originX: number,
  originY: number,
): ZoomTransform {
  const scale = clamp(transform.scale * factor, MIN_SCALE, MAX_SCALE);
  // The clamp may have swallowed part of the factor; translate by what
  // actually happened, or the image drifts at the limits.
  const applied = scale / transform.scale;
  return {
    scale,
    x: originX - (originX - transform.x) * applied,
    y: originY - (originY - transform.y) * applied,
  };
}

/** Move the image by a screen-space delta. */
export function panBy(transform: ZoomTransform, dx: number, dy: number): ZoomTransform {
  return { ...transform, x: transform.x + dx, y: transform.y + dy };
}

/**
 * Pull the translation back inside the bounds of what is actually visible: an
 * image may never be dragged so far that the empty backdrop shows on the side
 * it came from. An axis with nothing to spare (the image fits) is pinned to
 * centre, which is also what returns a released pinch to rest.
 */
export function clampToBounds(
  transform: ZoomTransform,
  viewport: Box,
  content: Box,
): ZoomTransform {
  const slackX = Math.max(0, (content.width * transform.scale - viewport.width) / 2);
  const slackY = Math.max(0, (content.height * transform.scale - viewport.height) / 2);
  return {
    scale: transform.scale,
    x: clamp(transform.x, -slackX, slackX),
    y: clamp(transform.y, -slackY, slackY),
  };
}

/** Distance between two pointers — the pinch's raw signal. */
export function distance(a: { x: number; y: number }, b: { x: number; y: number }): number {
  return Math.hypot(a.x - b.x, a.y - b.y);
}

/** Midpoint of two pointers — where a pinch is anchored. */
export function midpoint(
  a: { x: number; y: number },
  b: { x: number; y: number },
): { x: number; y: number } {
  return { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 };
}

/**
 * A double-tap toggles rather than steps: zoomed in at all, it goes back to
 * rest, because "get me out of here" is the other half of the gesture and a
 * second way in (pinch) already exists.
 */
export function toggleZoom(
  transform: ZoomTransform,
  originX: number,
  originY: number,
): ZoomTransform {
  if (transform.scale > MIN_SCALE) return IDENTITY;
  return zoomAbout(transform, DOUBLE_TAP_SCALE, originX, originY);
}

/** At rest, so a tap on the backdrop means "close" rather than "stop panning". */
export function isAtRest(transform: ZoomTransform): boolean {
  return transform.scale <= MIN_SCALE;
}
