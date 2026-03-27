import { useEffect } from "react";

/**
 * Prevents the browser from scrolling the visual viewport when the virtual
 * keyboard opens. Without this, browsers that don't fully support
 * `interactive-widget=resizes-content` (or during the keyboard animation)
 * scroll the page to keep the focused input visible, pushing the top
 * navigation bar off-screen.
 */
export function usePreventViewportScroll(): void {
  useEffect(() => {
    const vv = window.visualViewport;
    if (!vv) return;

    let rafId = 0;

    const resetScroll = () => {
      cancelAnimationFrame(rafId);
      rafId = requestAnimationFrame(() => {
        if (vv.offsetTop > 0) {
          window.scrollTo(0, 0);
        }
      });
    };

    vv.addEventListener("scroll", resetScroll);
    vv.addEventListener("resize", resetScroll);

    return () => {
      cancelAnimationFrame(rafId);
      vv.removeEventListener("scroll", resetScroll);
      vv.removeEventListener("resize", resetScroll);
    };
  }, []);
}
