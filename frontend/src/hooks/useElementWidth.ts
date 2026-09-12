import { useCallback, useEffect, useRef, useState } from "react";

/**
 * The live content width of an element in CSS pixels, and the ref to attach to
 * it. `0` means "not measured yet", which is not the same as "no room" — a
 * caller sizing something against it has to say what it does before the first
 * observation lands.
 *
 * A callback ref rather than an object ref: a node that mounts behind a
 * condition would otherwise be measured once, at nothing, and never again.
 */
export function useElementWidth<T extends HTMLElement>(): [(node: T | null) => void, number] {
  const [width, setWidth] = useState(0);
  const observerRef = useRef<ResizeObserver | null>(null);

  const ref = useCallback((node: T | null) => {
    observerRef.current?.disconnect();
    observerRef.current = null;
    if (!node) return;
    setWidth(node.clientWidth);
    // jsdom and older browsers have none; the caller's unmeasured branch stands.
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(() => setWidth(node.clientWidth));
    observer.observe(node);
    observerRef.current = observer;
  }, []);

  useEffect(() => () => observerRef.current?.disconnect(), []);

  return [ref, width];
}
