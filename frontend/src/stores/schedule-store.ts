import { create } from "zustand";
import type { ScheduleInfo, ScheduleRunInfo } from "~/lib/schedule-actions";

interface ScheduleState {
  schedules: Record<string, ScheduleInfo>;
  /** Run history keyed by scheduleId, newest first (bounded page). */
  runs: Record<string, ScheduleRunInfo[]>;
  loaded: boolean;

  setSchedules: (schedules: ScheduleInfo[]) => void;
  upsertSchedule: (schedule: ScheduleInfo) => void;
  removeSchedule: (id: string) => void;

  setRuns: (scheduleId: string, runs: ScheduleRunInfo[]) => void;
  upsertRun: (run: ScheduleRunInfo) => void;
}

const MAX_RUNS_IN_STORE = 100;

export const useScheduleStore = create<ScheduleState>((set) => ({
  schedules: {},
  runs: {},
  loaded: false,

  setSchedules: (schedules) =>
    set({
      schedules: Object.fromEntries(schedules.map((s) => [s.id, s])),
      loaded: true,
    }),

  upsertSchedule: (schedule) =>
    set((s) => ({ schedules: { ...s.schedules, [schedule.id]: schedule } })),

  removeSchedule: (id) =>
    set((s) => {
      const { [id]: _, ...schedules } = s.schedules;
      const { [id]: __, ...runs } = s.runs;
      return { schedules, runs };
    }),

  setRuns: (scheduleId, runs) => set((s) => ({ runs: { ...s.runs, [scheduleId]: runs } })),

  upsertRun: (run) =>
    set((s) => {
      const existing = s.runs[run.scheduleId] ?? [];
      const idx = existing.findIndex((r) => r.id === run.id);
      const next =
        idx >= 0
          ? existing.map((r) => (r.id === run.id ? run : r))
          : [run, ...existing].slice(0, MAX_RUNS_IN_STORE);
      return { runs: { ...s.runs, [run.scheduleId]: next } };
    }),
}));

/** Stable empty fallbacks (never return fresh [] from a selector). */
export const EMPTY_SCHEDULES: ScheduleInfo[] = [];
export const EMPTY_RUNS: ScheduleRunInfo[] = [];
