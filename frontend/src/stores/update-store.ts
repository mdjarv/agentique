import { create } from "zustand";
import type { UpdateStatus } from "~/lib/generated-types";
import { fetchUpdateStatus, PRIMARY_MACHINE_KEY } from "~/lib/update-api";

/**
 * What each machine says about its own version (docs/upgrades.md).
 *
 * Keyed by machine: PRIMARY_MACHINE_KEY for the server serving this SPA, the
 * machineId for each paired remote. Nothing here is persisted — including the
 * dismissal (decision U2): a reload brings the chip back, deliberately. An
 * update you waved away in the morning should still be there in the afternoon.
 */
interface UpdateState {
  statuses: Record<string, UpdateStatus>;
  /** Machines whose check is in flight, so a button can say so. */
  checking: Record<string, boolean>;
  /** Machines whose last fetch failed (unreachable, or too old to answer). */
  errors: Record<string, string>;
  /** The footer chip is hidden for this page session only. */
  dismissed: boolean;

  /** Fetch one machine's status. `refresh` forces that server to re-check. */
  fetch: (key: string, refresh?: boolean) => Promise<void>;
  /** Fetch several machines concurrently — see `machineKeys()` for the set. */
  fetchAll: (keys: string[], refresh?: boolean) => Promise<void>;
  dismiss: () => void;
}

export const useUpdateStore = create<UpdateState>((set, get) => ({
  statuses: {},
  checking: {},
  errors: {},
  dismissed: false,

  fetch: async (key, refresh = false) => {
    set((s) => ({ checking: { ...s.checking, [key]: true } }));
    try {
      const status = await fetchUpdateStatus(key, refresh);
      set((s) => {
        const errors = { ...s.errors };
        delete errors[key];
        return { statuses: { ...s.statuses, [key]: status }, errors };
      });
    } catch (err) {
      // A machine that cannot answer is not a failure to shout about: keep
      // whatever it last said and record why the refresh didn't land.
      set((s) => ({
        errors: { ...s.errors, [key]: err instanceof Error ? err.message : String(err) },
      }));
    } finally {
      set((s) => {
        const checking = { ...s.checking };
        delete checking[key];
        return { checking };
      });
    }
  },

  fetchAll: async (keys, refresh = false) => {
    // One request per machine, in parallel: every server answers for itself,
    // and a machine that is asleep must not hold up the ones that are awake.
    await Promise.all(keys.map((key) => get().fetch(key, refresh)));
  },

  dismiss: () => set({ dismissed: true }),
}));

/** Machine keys with a published upgrade waiting. Ordered primary-first. */
export function behindKeys(statuses: Record<string, UpdateStatus>): string[] {
  return Object.keys(statuses)
    .filter((key) => statuses[key]?.behind)
    .sort((a, b) => (a === PRIMARY_MACHINE_KEY ? -1 : b === PRIMARY_MACHINE_KEY ? 1 : 0));
}
