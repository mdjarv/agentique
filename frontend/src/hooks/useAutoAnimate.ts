import autoAnimate, {
  type AnimationController,
  type AutoAnimateOptions,
} from "@formkit/auto-animate";
export { useAutoAnimate } from "@formkit/auto-animate/react";
import { useEffect, useRef } from "react";

export const ANIMATE_CHAT: Partial<AutoAnimateOptions> = { duration: 200, easing: "ease-out" };
export const ANIMATE_DEFAULT: Partial<AutoAnimateOptions> = { duration: 150, easing: "ease-out" };

/**
 * Attaches auto-animate to a container that already has a ref.
 * Returns a controller ref for enable/disable control.
 */
export function useMergedAutoAnimate(
  existingRef: React.RefObject<HTMLElement | null>,
  options: Partial<AutoAnimateOptions> = ANIMATE_DEFAULT,
): React.RefObject<AnimationController | null> {
  const controllerRef = useRef<AnimationController | null>(null);

  useEffect(() => {
    const el = existingRef.current;
    if (!el) return;
    controllerRef.current = autoAnimate(el, options);
    return () => {
      controllerRef.current?.disable();
      controllerRef.current = null;
    };
  }, [existingRef, options]);

  return controllerRef;
}
