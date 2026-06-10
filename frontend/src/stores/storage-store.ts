import { create } from "zustand";
import { getDiskStats, getStorageUsage } from "~/lib/api";
import type { DiskStats, StorageUsage } from "~/lib/generated-types";
import { getErrorMessage } from "~/lib/utils";

interface StorageState {
  disk: DiskStats | null;
  usage: StorageUsage | null;
  usageLoading: boolean;
  usageError: string | null;
  /** Fetch the cheap volume free/total stats (safe to poll). */
  fetchDiskStats: () => Promise<void>;
  /** Fetch (or recompute with refresh) the full per-project breakdown. */
  fetchUsage: (refresh?: boolean) => Promise<void>;
}

export const useStorageStore = create<StorageState>((set) => ({
  disk: null,
  usage: null,
  usageLoading: false,
  usageError: null,
  fetchDiskStats: async () => {
    try {
      const disk = await getDiskStats();
      set({ disk });
    } catch (err) {
      console.error("Failed to fetch disk stats", err);
    }
  },
  fetchUsage: async (refresh = false) => {
    set({ usageLoading: true, usageError: null });
    try {
      const usage = await getStorageUsage(refresh);
      // The cheap disk stats are embedded in the usage payload — keep them in sync.
      set({ usage, disk: usage.disk });
    } catch (err) {
      console.error("Failed to fetch storage usage", err);
      set({ usageError: getErrorMessage(err, "Failed to load storage usage") });
    } finally {
      set({ usageLoading: false });
    }
  },
}));
