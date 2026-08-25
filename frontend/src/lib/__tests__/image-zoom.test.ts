import { describe, expect, it } from "vitest";
import {
  clampToBounds,
  DOUBLE_TAP_SCALE,
  IDENTITY,
  isAtRest,
  MAX_SCALE,
  panBy,
  toggleZoom,
  type ZoomTransform,
  zoomAbout,
} from "~/lib/image-zoom";

/** Where an image point lands on screen under a transform. */
function project(t: ZoomTransform, point: number): number {
  return point * t.scale + t.x;
}

describe("zoomAbout", () => {
  it("keeps the anchored point under the finger", () => {
    const anchor = 120;
    const zoomed = zoomAbout(IDENTITY, 2, anchor, 0);
    // The image point that was at `anchor` is still at `anchor`.
    const imagePoint = (anchor - IDENTITY.x) / IDENTITY.scale;
    expect(project(zoomed, imagePoint)).toBeCloseTo(anchor, 5);
  });

  it("stays put when zooming about the centre", () => {
    expect(zoomAbout(IDENTITY, 3, 0, 0)).toEqual({ scale: 3, x: 0, y: 0 });
  });

  it("does not drift once clamped at the maximum", () => {
    const atMax = zoomAbout(IDENTITY, MAX_SCALE * 4, 100, 100);
    expect(atMax.scale).toBe(MAX_SCALE);
    // A further pinch past the limit must not translate either — the scale
    // that was refused cannot be allowed to move the image.
    const further = zoomAbout(atMax, 2, 100, 100);
    expect(further).toEqual(atMax);
  });

  it("never scales below rest", () => {
    expect(zoomAbout(IDENTITY, 0.1, 50, 50).scale).toBe(1);
  });
});

describe("clampToBounds", () => {
  const viewport = { width: 400, height: 300 };
  const content = { width: 400, height: 300 };

  it("pins an image that fits to the centre", () => {
    const panned = panBy({ scale: 1, x: 0, y: 0 }, 90, -40);
    expect(clampToBounds(panned, viewport, content)).toEqual({ scale: 1, x: 0, y: 0 });
  });

  it("allows exactly the overflow as slack", () => {
    // At 2x the content is 800x600, so 200/150 of travel each way.
    const panned = panBy({ scale: 2, x: 0, y: 0 }, 1000, 1000);
    expect(clampToBounds(panned, viewport, content)).toEqual({ scale: 2, x: 200, y: 150 });
  });

  it("leaves a pan inside the bounds alone", () => {
    const panned = { scale: 2, x: -50, y: 20 };
    expect(clampToBounds(panned, viewport, content)).toEqual(panned);
  });
});

describe("toggleZoom", () => {
  it("zooms in from rest, anchored at the tap", () => {
    const zoomed = toggleZoom(IDENTITY, 60, 20);
    expect(zoomed.scale).toBe(DOUBLE_TAP_SCALE);
    expect(project(zoomed, 60)).toBeCloseTo(60, 5);
  });

  it("returns all the way to rest from any magnification", () => {
    expect(toggleZoom({ scale: 4.2, x: 120, y: -30 }, 10, 10)).toEqual(IDENTITY);
  });
});

describe("isAtRest", () => {
  it("is true only at the minimum scale", () => {
    expect(isAtRest(IDENTITY)).toBe(true);
    expect(isAtRest({ scale: 1.01, x: 0, y: 0 })).toBe(false);
  });
});
