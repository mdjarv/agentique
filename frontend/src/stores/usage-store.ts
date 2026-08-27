import { create } from "zustand";
import type { UsageDocument } from "~/lib/generated-types";
import { fetchUsage } from "~/lib/usage-api";

/**
 * What this machine has left of each subscription window (docs/usage.md).
 *
 * Nothing here is persisted. The server holds the cache and does the probing,
 * so a reload costs one cheap read rather than a round trip to a vendor, and
 * five open tabs cost one probe rather than five.
 *
 * A failed refresh NEVER blanks the document: the last good numbers stay, and
 * the record's own `usageStatusText` explains why they are old. That rule lives
 * here as well as on the server, because a fetch that throws must not undo a
 * document that arrived.
 */

/** The resting beat. Windows move on the scale of minutes at worst. */
const POLL_MS = 15 * 60_000;

/**
 * The beat while nothing has answered yet.
 *
 * Armed off "do we have a document", not off a request completing: a fetch that
 * never starts produces no completion to hook, and a completion-driven retry
 * leaves the indicator dark forever.
 */
const RETRY_MS = 30_000;

interface UsageState {
  doc: UsageDocument | null;
  /** A fetch is in flight, so a button can say so. */
  loading: boolean;
  /** Why the last fetch failed. The document, if any, survives it. */
  error: string | null;
  fetch: (refresh?: boolean) => Promise<void>;
}

export const useUsageStore = create<UsageState>((set, get) => ({
  doc: null,
  loading: false,
  error: null,

  fetch: async (refresh = false) => {
    set({ loading: true });
    try {
      const doc = await fetchUsage(refresh);
      set({ doc, error: null });
    } catch (err) {
      // Keep whatever we last had. An unreachable server is not a reason to
      // throw away numbers that were true a minute ago.
      set({ error: err instanceof Error ? err.message : String(err) });
    } finally {
      set({ loading: false });
    }
    void get; // keep the signature stable for future callers
  },
}));

/**
 * Start the poll. Returns a teardown.
 *
 * Kept out of a component so the cadence survives navigation: the indicator is
 * in the sidebar footer, which outlives every route.
 */
export function startUsagePolling(): () => void {
  const tick = () => void useUsageStore.getState().fetch();
  tick();

  let timer: ReturnType<typeof setTimeout>;
  const schedule = () => {
    // Re-read the beat each time rather than fixing it at start: the first
    // successful document is what moves us off the fast retry.
    const delay = useUsageStore.getState().doc ? POLL_MS : RETRY_MS;
    timer = setTimeout(() => {
      tick();
      schedule();
    }, delay);
  };
  schedule();

  return () => clearTimeout(timer);
}
