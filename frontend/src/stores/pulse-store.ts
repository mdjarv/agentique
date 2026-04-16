import { create } from "zustand";
import type { PushSessionPulse } from "~/lib/generated-types";

export interface PulseData {
  lastToolCategory: string;
  lastFilePath: string;
  toolCallCount: number;
  commitCount: number;
  errorCount: number;
  turnStartedAt: number;
}

interface PulseStore {
  /** sessionId → latest pulse snapshot. Cleared when session goes idle. */
  pulses: Record<string, PulseData>;
  setPulse: (sessionId: string, pulse: PushSessionPulse) => void;
  clearPulse: (sessionId: string) => void;
}

export const usePulseStore = create<PulseStore>((set) => ({
  pulses: {},
  setPulse: (sessionId, pulse) =>
    set((s) => ({
      pulses: {
        ...s.pulses,
        [sessionId]: {
          lastToolCategory: pulse.lastToolCategory ?? "",
          lastFilePath: pulse.lastFilePath ?? "",
          toolCallCount: pulse.toolCallCount,
          commitCount: pulse.commitCount,
          errorCount: pulse.errorCount,
          turnStartedAt: pulse.turnStartedAt,
        },
      },
    })),
  clearPulse: (sessionId) =>
    set((s) => {
      if (!s.pulses[sessionId]) return s;
      const { [sessionId]: _, ...rest } = s.pulses;
      return { pulses: rest };
    }),
}));
