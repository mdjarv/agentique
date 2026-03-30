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

export const useRateLimitStore = create<RateLimitState>()(
  persist(
    (set) => ({
      entries: {},
      updateEntry: (type, status, utilization, resetsAt) =>
        set((s) => ({
          entries: {
            ...s.entries,
            [type]: { status, utilization, resetsAt, updatedAt: Date.now() },
          },
        })),
    }),
    {
      name: "agentique:rate-limits",
      storage: createJSONStorage(() => localStorage),
      partialize: (state) => ({ entries: state.entries }),
    },
  ),
);
