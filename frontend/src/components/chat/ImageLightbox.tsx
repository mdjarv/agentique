/**
 * Full-screen image viewer with pinch-zoom, drag-to-pan, double-tap and wheel.
 *
 * The gesture maths lives in `lib/image-zoom.ts`; what is here is the DOM half:
 * pointer bookkeeping, the transform, and the rules for when a tap means
 * "close". Pointer events rather than touch events, so a mouse, a pen and two
 * fingers all drive the same code path.
 *
 * The surface takes `touch-action: none` because the browser's own pinch zooms
 * the *page* — which on an app shell means zooming the chat behind the image
 * and leaving the viewer stranded at 1x.
 */
import { X } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import type { ZoomTransform } from "~/lib/image-zoom";
import {
  clampToBounds,
  distance,
  IDENTITY,
  isAtRest,
  midpoint,
  panBy,
  toggleZoom,
  zoomAbout,
} from "~/lib/image-zoom";

/** Pointer travel (px) past which a release is a pan, not a tap. */
const TAP_SLOP = 8;
/** Milliseconds between taps that still count as a double-tap. */
const DOUBLE_TAP_MS = 300;

interface Point {
  x: number;
  y: number;
}

/**
 * `src` doubles as open/closed. The viewer is keyed by it, so each image gets
 * its own instance and therefore its own zoom — reusing one would open the next
 * screenshot at whatever magnification the last was left at, showing a corner.
 */
export function ImageLightbox({ src, onClose }: { src: string | null; onClose: () => void }) {
  if (!src) return null;
  return <ZoomableViewer key={src} src={src} onClose={onClose} />;
}

function ZoomableViewer({ src, onClose }: { src: string; onClose: () => void }) {
  const surfaceRef = useRef<HTMLDivElement>(null);
  const imageRef = useRef<HTMLImageElement>(null);
  const [transform, setTransform] = useState<ZoomTransform>(IDENTITY);
  const [dragging, setDragging] = useState(false);

  // Live pointers, keyed by pointerId: one is a drag, two are a pinch.
  const pointers = useRef(new Map<number, Point>());
  const pinchStart = useRef<{ distance: number; scale: number } | null>(null);
  const lastTap = useRef(0);
  const travelled = useRef(0);
  const onImage = useRef(false);
  const wasPinch = useRef(false);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onClose]);

  /** Viewport-centred coordinates for a client point. */
  const toLocal = useCallback((clientX: number, clientY: number): Point => {
    const rect = surfaceRef.current?.getBoundingClientRect();
    if (!rect) return { x: 0, y: 0 };
    return { x: clientX - rect.left - rect.width / 2, y: clientY - rect.top - rect.height / 2 };
  }, []);

  /** Keep the image inside its own edges after any change that could free it. */
  const settle = useCallback((next: ZoomTransform): ZoomTransform => {
    const surface = surfaceRef.current;
    const image = imageRef.current;
    if (!surface || !image) return next;
    return clampToBounds(
      next,
      { width: surface.clientWidth, height: surface.clientHeight },
      // The rendered size at scale 1 — offsetWidth is pre-transform, which is
      // exactly the content box the clamp is defined against.
      { width: image.offsetWidth, height: image.offsetHeight },
    );
  }, []);

  const handlePointerDown = useCallback((e: React.PointerEvent) => {
    // Capture keeps a finger that slides off the image still driving the
    // gesture. It throws on a pointer the element never owned, which is not a
    // reason to drop the gesture.
    try {
      (e.target as Element).setPointerCapture?.(e.pointerId);
    } catch {
      // ignore
    }
    pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY });
    if (pointers.current.size === 1) {
      travelled.current = 0;
      wasPinch.current = false;
      onImage.current = e.target === imageRef.current;
      setDragging(true);
      return;
    }
    wasPinch.current = true;
    // Second finger down: remember the span this pinch started from, so the
    // scale tracks the fingers absolutely rather than drifting per event.
    const [a, b] = [...pointers.current.values()];
    if (a && b) {
      pinchStart.current = { distance: distance(a, b), scale: 0 };
      setDragging(false);
    }
  }, []);

  const handlePointerMove = useCallback(
    (e: React.PointerEvent) => {
      const previous = pointers.current.get(e.pointerId);
      if (!previous) return;
      const current = { x: e.clientX, y: e.clientY };
      pointers.current.set(e.pointerId, current);

      const points = [...pointers.current.values()];
      if (points.length >= 2) {
        const [a, b] = points;
        if (!a || !b || !pinchStart.current) return;
        const span = distance(a, b);
        if (pinchStart.current.scale === 0) {
          // First move of the pinch: anchor the reference scale now that we
          // know the transform the fingers landed on.
          pinchStart.current = { distance: span, scale: transform.scale };
          return;
        }
        const target = (span / pinchStart.current.distance) * pinchStart.current.scale;
        const centre = midpoint(a, b);
        const origin = toLocal(centre.x, centre.y);
        setTransform((t) => settle(zoomAbout(t, target / t.scale, origin.x, origin.y)));
        return;
      }

      const dx = current.x - previous.x;
      const dy = current.y - previous.y;
      travelled.current += Math.abs(dx) + Math.abs(dy);
      // Panning a resting image would just slide it around inside its own
      // letterbox; the clamp pins it, so this is a no-op until zoomed in.
      setTransform((t) => (isAtRest(t) ? t : settle(panBy(t, dx, dy))));
    },
    [settle, toLocal, transform.scale],
  );

  const endPointer = useCallback(
    (e: React.PointerEvent) => {
      pointers.current.delete(e.pointerId);
      if (pointers.current.size < 2) pinchStart.current = null;
      if (pointers.current.size > 0) return;
      setDragging(false);

      // A release that moved is a pan; only a still one is a tap. Lifting off
      // a pinch is never a tap either — a pinch anchored in the letterboxing
      // beside a portrait image would otherwise close the viewer on release.
      if (wasPinch.current) {
        wasPinch.current = false;
        return;
      }
      if (travelled.current > TAP_SLOP) return;

      // Off the image is the backdrop, and tapping past a picture has always
      // meant "put it away". On the image it never closes — that tap is half
      // of a double-tap, and closing on the first one would make zooming by
      // tapping impossible.
      if (!onImage.current) {
        onClose();
        return;
      }

      const now = e.timeStamp;
      const origin = toLocal(e.clientX, e.clientY);
      if (now - lastTap.current < DOUBLE_TAP_MS) {
        lastTap.current = 0;
        setTransform((t) => settle(toggleZoom(t, origin.x, origin.y)));
        return;
      }
      lastTap.current = now;
    },
    [onClose, settle, toLocal],
  );

  const handleWheel = useCallback(
    (e: React.WheelEvent) => {
      const origin = toLocal(e.clientX, e.clientY);
      // Trackpad pinch arrives as ctrl+wheel; a plain wheel zooms too, since
      // there is nothing else to scroll in here.
      const factor = Math.exp(-e.deltaY / 300);
      setTransform((t) => settle(zoomAbout(t, factor, origin.x, origin.y)));
    },
    [settle, toLocal],
  );

  const zoomed = !isAtRest(transform);

  return createPortal(
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Image preview"
      className="fixed inset-0 z-50 bg-black/85"
    >
      <div
        ref={surfaceRef}
        className="absolute inset-0 flex touch-none items-center justify-center overflow-hidden"
        style={{ cursor: zoomed ? (dragging ? "grabbing" : "grab") : "zoom-in" }}
        onPointerDown={handlePointerDown}
        onPointerMove={handlePointerMove}
        onPointerUp={endPointer}
        onPointerCancel={endPointer}
        onWheel={handleWheel}
      >
        <img
          ref={imageRef}
          src={src}
          alt="Full-size preview"
          draggable={false}
          className="max-h-[95vh] max-w-[95vw] select-none rounded-lg object-contain"
          style={{
            transform: `translate(${transform.x}px, ${transform.y}px) scale(${transform.scale})`,
            // Snapping (double-tap, release) should read as motion; a pinch
            // must not lag behind the fingers driving it.
            transition: dragging || pointers.current.size > 1 ? "none" : "transform 150ms ease-out",
          }}
        />
      </div>
      {/* Touch has no Escape key, and at any zoom above rest a tap pans
          instead of closing — so the way out is always on screen. */}
      <button
        type="button"
        onClick={onClose}
        aria-label="Close preview"
        className="absolute top-[max(0.75rem,env(safe-area-inset-top))] right-3 inline-flex size-9 items-center justify-center rounded-full bg-black/50 text-white/80 backdrop-blur transition-colors hover:bg-black/70 hover:text-white"
      >
        <X className="size-5" />
      </button>
    </div>,
    document.body,
  );
}
