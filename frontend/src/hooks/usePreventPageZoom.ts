import { useEffect } from "react";

/**
 * Stops iOS from pinch-zooming the page.
 *
 * The app is a fixed shell (`#root` is `100dvh`, `overflow: hidden`), so there
 * is nothing for a page zoom to reveal: WebKit scales the whole shell like an
 * image, and the layout viewport it leaves behind no longer matches the
 * visual one — the header and composer end up off-screen or offset until the
 * app is relaunched. Content that is worth zooming zooms itself
 * (`ImageLightbox`).
 *
 * iOS ignores `user-scalable=no` in the viewport meta, and `touch-action`
 * alone does not stop a pinch that starts on a scroll container, so the
 * WebKit `gesture*` events are cancelled directly. They are not pointer
 * events, so in-app pinch handlers still receive both fingers.
 */
export function usePreventPageZoom(): void {
  useEffect(() => {
    const cancel = (e: Event) => e.preventDefault();
    const opts: AddEventListenerOptions = { passive: false };
    const events = ["gesturestart", "gesturechange", "gestureend"] as const;

    for (const name of events) document.addEventListener(name, cancel, opts);
    return () => {
      for (const name of events) document.removeEventListener(name, cancel, opts);
    };
  }, []);
}
