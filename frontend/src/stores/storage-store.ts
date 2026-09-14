import { create } from "zustand";
import { getDiskStats, getStorageUsage } from "~/lib/api";
import type { DiskStats, StorageUsage } from "~/lib/generated-types";
import { machineKeys, targetFor } from "~/lib/update-api";
import { getErrorMessage } from "~/lib/utils";
import { useMachineStore } from "~/stores/machine-store";

/**
 * Disk readings and storage breakdowns, keyed by machine (docs/storage.md).
 *
 * Keys are `PRIMARY_MACHINE_KEY` or a paired machine's id, the same keys
 * `update-store` uses. Nothing is persisted. A failed fetch never drops a
 * reading: an away machine keeps what it last reported, and the page says how
 * old that is rather than going blank.
 */
interface StorageState {
  disks: Record<string, DiskStats>;
  usages: Record<string, StorageUsage>;
  usageLoading: Record<string, boolean>;
  usageErrors: Record<string, string>;
  /** Fetch one machine's cheap volume free/total stats (safe to poll). */
  fetchDisk: (key: string) => Promise<void>;
  /** Fetch (or recompute with refresh) one machine's full breakdown. */
  fetchUsage: (key: string, refresh?: boolean) => Promise<void>;
}

export const useStorageStore = create<StorageState>((set) => ({
  disks: {},
  usages: {},
  usageLoading: {},
  usageErrors: {},
  fetchDisk: async (key) => {
    try {
      const disk = await getDiskStats(targetFor(key));
      set((s) => ({ disks: { ...s.disks, [key]: disk } }));
    } catch (err) {
      console.debug("Failed to fetch disk stats", key, err);
    }
  },
  fetchUsage: async (key, refresh = false) => {
    set((s) => ({
      usageLoading: { ...s.usageLoading, [key]: true },
      usageErrors: without(s.usageErrors, key),
    }));
    try {
      const usage = await getStorageUsage(targetFor(key), refresh);
      // The cheap disk stats are embedded in the usage payload — keep them in sync.
      set((s) => ({
        usages: { ...s.usages, [key]: usage },
        disks: { ...s.disks, [key]: usage.disk },
      }));
    } catch (err) {
      console.error("Failed to fetch storage usage", key, err);
      set((s) => ({
        usageErrors: {
          ...s.usageErrors,
          [key]: getErrorMessage(err, "Failed to load storage usage"),
        },
      }));
    } finally {
      set((s) => ({ usageLoading: without(s.usageLoading, key) }));
    }
  },
}));

function without<T>(record: Record<string, T>, key: string): Record<string, T> {
  if (!(key in record)) return record;
  const next = { ...record };
  delete next[key];
  return next;
}

/**
 * How often every reachable machine's disk is read. A statfs per machine is
 * cheap, and a disk filling during an install moves on the scale of minutes.
 */
const DISK_POLL_MS = 2 * 60_000;

/**
 * Poll every reachable machine's disk. Returns a teardown.
 *
 * Only this machine and connected remotes are asked: a machine that is away
 * would hold a request open until it times out, and its last reading is kept
 * anyway. A remote that connects is read at once rather than at the next beat,
 * so the footer notch does not wait two minutes to notice a machine that woke
 * up nearly full.
 */
export function startDiskPolling(): () => void {
  const readReachable = () => {
    const { machines, statuses } = useMachineStore.getState();
    for (const key of machineKeys(machines)) {
      const id = targetFor(key);
      if (id && statuses[id] !== "connected") continue;
      void useStorageStore.getState().fetchDisk(key);
    }
  };
  readReachable();
  const timer = setInterval(readReachable, DISK_POLL_MS);

  const unsubscribe = useMachineStore.subscribe((state, prev) => {
    for (const [id, status] of Object.entries(state.statuses)) {
      if (status === "connected" && prev.statuses[id] !== "connected") {
        void useStorageStore.getState().fetchDisk(id);
      }
    }
  });

  return () => {
    clearInterval(timer);
    unsubscribe();
  };
}
