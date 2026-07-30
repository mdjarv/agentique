import { useEffect, useState } from "react";

/**
 * Wall-clock tick: re-renders the consumer every `intervalMs` so relative
 * times ("in 3m", "2h ago") stay live instead of freezing at mount time.
 */
export function useNow(intervalMs = 30_000): Date {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const id = setInterval(() => setNow(new Date()), intervalMs);
    return () => clearInterval(id);
  }, [intervalMs]);
  return now;
}
