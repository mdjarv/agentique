import { useEffect } from "react";
import { useWebSocket } from "~/hooks/useWebSocket";
import { markSessionAttention } from "~/lib/session/actions";

/**
 * Heartbeat interval. Comfortably shorter than any sane idle-evict TTL (the
 * shortest documented setting is 30m), so a viewed session never drifts close
 * to eviction, while still costing only a couple of tiny messages an hour.
 */
const ATTENTION_INTERVAL_MS = 60_000;

/**
 * Keeps the open session from being idle-evicted while someone is looking at it.
 *
 * The backend measures idleness from the last *turn*, so reading a long answer,
 * reviewing a diff, or writing a careful prompt all look identical to a session
 * abandoned days ago — and the sweep reclaims it mid-use. This is the missing
 * signal: the server has no per-session view state of its own (WS subscriptions
 * are per project).
 *
 * Gated on document visibility so a forgotten background tab stops voting
 * almost immediately; the point is to protect sessions a human is actually
 * looking at, not every tab ever opened. Fires once on mount/focus so a session
 * is protected the moment it is opened, not a minute later.
 */
export function useSessionAttention(sessionId: string | undefined, enabled = true) {
  const ws = useWebSocket();

  useEffect(() => {
    if (!sessionId || !enabled) return;

    const ping = () => {
      if (document.visibilityState !== "visible") return;
      // Best-effort: losing a heartbeat costs at most one sweep interval of
      // protection, and must never surface as an error to the user.
      markSessionAttention(ws, sessionId).catch(() => {});
    };

    ping();
    const timer = window.setInterval(ping, ATTENTION_INTERVAL_MS);
    document.addEventListener("visibilitychange", ping);
    window.addEventListener("focus", ping);

    return () => {
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", ping);
      window.removeEventListener("focus", ping);
    };
  }, [ws, sessionId, enabled]);
}
