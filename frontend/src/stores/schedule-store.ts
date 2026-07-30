import { create } from "zustand";
import type { ScheduleInfo, ScheduleRunInfo } from "~/lib/schedule-actions";

interface ScheduleState {
  schedules: Record<string, ScheduleInfo>;
  /** Run history keyed by scheduleId, newest first (bounded page). */
  runs: Record<string, ScheduleRunInfo[]>;
  loaded: boolean;
  /** Set when the initial listSchedules fetch failed (e.g. scheduler disabled). */
  loadError: string | null;

  setSchedules: (schedules: ScheduleInfo[]) => void;
  upsertSchedule: (schedule: ScheduleInfo) => void;
  removeSchedule: (id: string) => void;
  /** Drop all schedules targeting a deleted session (plus their runs). */
  removeSchedulesForSession: (sessionId: string) => void;
  setLoadError: (message: string) => void;

  setRuns: (scheduleId: string, runs: ScheduleRunInfo[]) => void;
  upsertRun: (run: ScheduleRunInfo) => void;
}

const MAX_RUNS_IN_STORE = 100;

/** Terminal run statuses — a run never leaves these once reached. */
const TERMINAL_RUN_STATUSES = new Set([
  "ok",
  "action_needed",
  "error",
  "deferred",
  "interrupted",
  "skipped",
]);

const isTerminalRun = (r: ScheduleRunInfo) => TERMINAL_RUN_STATUSES.has(r.status);

export const useScheduleStore = create<ScheduleState>((set) => ({
  schedules: {},
  runs: {},
  loaded: false,
  loadError: null,

  setSchedules: (schedules) =>
    set({
      schedules: Object.fromEntries(schedules.map((s) => [s.id, s])),
      loaded: true,
      loadError: null,
    }),

  upsertSchedule: (schedule) =>
    set((s) => ({ schedules: { ...s.schedules, [schedule.id]: schedule } })),

  removeSchedule: (id) =>
    set((s) => {
      const { [id]: _, ...schedules } = s.schedules;
      const { [id]: __, ...runs } = s.runs;
      return { schedules, runs };
    }),

  removeSchedulesForSession: (sessionId) =>
    set((s) => {
      const ids = Object.values(s.schedules)
        .filter((sc) => sc.sessionId === sessionId)
        .map((sc) => sc.id);
      if (ids.length === 0) return s;
      const schedules = { ...s.schedules };
      const runs = { ...s.runs };
      for (const id of ids) {
        delete schedules[id];
        delete runs[id];
      }
      return { schedules, runs };
    }),

  setLoadError: (message) => set({ loadError: message, loaded: true }),

  // Merge, don't replace: a stale RPC snapshot (listScheduleRuns) races live
  // `schedule.run` pushes — if the incoming page has a non-terminal run the
  // store already knows is terminal, keep the store's version.
  setRuns: (scheduleId, runs) =>
    set((s) => {
      const prevById = new Map((s.runs[scheduleId] ?? []).map((r) => [r.id, r]));
      const merged = runs.map((r) => {
        const prev = prevById.get(r.id);
        return prev && isTerminalRun(prev) && !isTerminalRun(r) ? prev : r;
      });
      return { runs: { ...s.runs, [scheduleId]: merged } };
    }),

  upsertRun: (run) =>
    set((s) => {
      const existing = s.runs[run.scheduleId] ?? [];
      const idx = existing.findIndex((r) => r.id === run.id);
      if (idx >= 0) {
        // Run states are one-way: never regress a terminal run to a
        // non-terminal status (stale RPC snapshots race live pushes).
        const prev = existing[idx];
        if (prev && isTerminalRun(prev) && !isTerminalRun(run)) return s;
        return {
          runs: {
            ...s.runs,
            [run.scheduleId]: existing.map((r) => (r.id === run.id ? run : r)),
          },
        };
      }
      const next = [run, ...existing].slice(0, MAX_RUNS_IN_STORE);
      return { runs: { ...s.runs, [run.scheduleId]: next } };
    }),
}));

/** Stable empty fallbacks (never return fresh [] from a selector). */
export const EMPTY_SCHEDULES: ScheduleInfo[] = [];
export const EMPTY_RUNS: ScheduleRunInfo[] = [];
