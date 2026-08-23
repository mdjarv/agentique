import { create } from "zustand";
import type { UpdateStatus } from "~/lib/generated-types";
import { fetchUpdateStatus, PRIMARY_MACHINE_KEY } from "~/lib/update-api";

/**
 * What each machine says about its own version (docs/upgrades.md).
 *
 * Keyed by machine: PRIMARY_MACHINE_KEY for the server serving this SPA, the
 * machineId for each paired remote. Nothing here is persisted — a version
 * check is cheap, and a stale cached "update available" is worse than none.
 */
interface UpdateState {
  statuses: Record<string, UpdateStatus>;
  /** Machines whose check is in flight, so a button can say so. */
  checking: Record<string, boolean>;
  /** Machines whose last fetch failed (unreachable, or too old to answer). */
  errors: Record<string, string>;

  /** Fetch one machine's status. `refresh` forces that server to re-check. */
  fetch: (key: string, refresh?: boolean) => Promise<void>;
}

export const useUpdateStore = create<UpdateState>((set) => ({
  statuses: {},
  checking: {},
  errors: {},

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
}));

/** The primary's status, or undefined before the first fetch. */
export function usePrimaryUpdate(): UpdateStatus | undefined {
  return useUpdateStore((s) => s.statuses[PRIMARY_MACHINE_KEY]);
}
