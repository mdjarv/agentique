import { create } from "zustand";
import { type Finding, fetchFindings } from "~/lib/steward";

/**
 * This machine's open steward findings (docs/peers.md). Polled, because the
 * steward itself passes once a minute and pushes nothing to a browser.
 *
 * A failed fetch keeps the last answer: a server that did not respond is not a
 * machine whose problems went away.
 */

const POLL_MS = 60_000;

const NONE: Finding[] = [];

interface StewardState {
  findings: Finding[];
  fetch: () => Promise<void>;
}

export const useStewardStore = create<StewardState>((set) => ({
  findings: NONE,
  fetch: async () => {
    try {
      const findings = await fetchFindings();
      set({ findings: findings.length === 0 ? NONE : findings });
    } catch {
      // Keep what we had.
    }
  },
}));

/** Start the poll; returns a teardown. The footer outlives every route. */
export function startStewardPolling(): () => void {
  const tick = () => void useStewardStore.getState().fetch();
  tick();
  const timer = setInterval(tick, POLL_MS);
  return () => clearInterval(timer);
}
