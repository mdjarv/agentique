import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";

export type RateLimitType = "five_hour" | "seven_day";

export interface RateLimitEntry {
  status: string; // "allowed", "allowed_warning", "rejected"
  utilization: number; // 0.0 - 1.0
  resetsAt: number; // unix timestamp (seconds)
  updatedAt: number; // Date.now() when received
}

interface RateLimitState {
  entries: Partial<Record<RateLimitType, RateLimitEntry>>;
  updateEntry: (type: RateLimitType, status: string, utilization: number, resetsAt: number) => void;
}

const STALE_THRESHOLD_MS = 10 * 60_000; // 10 minutes

export const useRateLimitStore = create<RateLimitState>()(
  persist(
    (set) => ({
      entries: {},
      updateEntry: (type, status, utilization, resetsAt) =>
        set((s) => {
          const now = Date.now();
          const entries: Partial<Record<RateLimitType, RateLimitEntry>> = {
            ...s.entries,
            [type]: { status, utilization, resetsAt, updatedAt: now },
          };
          // Evict entries for other types that haven't been refreshed recently.
          for (const key of Object.keys(entries) as RateLimitType[]) {
            const other = entries[key];
            if (key !== type && other && now - other.updatedAt > STALE_THRESHOLD_MS) {
              delete entries[key];
            }
          }
          return { entries };
        }),
    }),
    {
      name: "agentique:rate-limits",
      storage: createJSONStorage(() => localStorage),
      partialize: (state) => ({ entries: state.entries }),
    },
  ),
);
