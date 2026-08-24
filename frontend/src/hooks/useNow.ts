import { useEffect, useState } from "react";

/**
 * Wall-clock tick: re-renders the consumer every `intervalMs` so relative
 * times ("in 3m", "2h ago") stay live instead of freezing at mount time.
 *
 * `enabled` exists for consumers that only need a fast tick during a
 * short-lived condition (agents in flight): passing `false` stops the timer
 * rather than re-rendering the subtree once a second forever.
 */
export function useNow(intervalMs = 30_000, enabled = true): Date {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    if (!enabled) return;
    setNow(new Date());
    const id = setInterval(() => setNow(new Date()), intervalMs);
    return () => clearInterval(id);
  }, [intervalMs, enabled]);
  return now;
}
