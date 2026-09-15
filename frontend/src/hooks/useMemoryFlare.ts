import { useEffect, useRef, useState } from "react";
import { useBrainStore } from "~/stores/brain-store";

/**
 * True for one pulse after memory changes: a fact added, edited or removed, or
 * a consolidation applied, here or in another tab.
 *
 * The brain store's `flareSeq` bumps on every `brain.updated` push, so this
 * watches the number rather than the events. The initial value is skipped,
 * because a control mounting is not news.
 */
export function useMemoryFlare(): boolean {
  const flareSeq = useBrainStore((s) => s.flareSeq);
  const [flaring, setFlaring] = useState(false);
  const seenRef = useRef(flareSeq);
  useEffect(() => {
    if (flareSeq === seenRef.current) return;
    seenRef.current = flareSeq;
    setFlaring(true);
    // Outlives the keyframe's two pulses, so the class is not pulled mid-glow.
    const t = setTimeout(() => setFlaring(false), 2400);
    return () => clearTimeout(t);
  }, [flareSeq]);
  return flaring;
}
