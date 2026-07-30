import type { ScheduleInfo, ScheduleRunInfo } from "~/lib/generated-types";
import { define } from "~/lib/ws-rpc";

// Scheduled loops (docs/scheduled-loops.md). Wire types are generated; this
// module only binds the RPC callers.

export type { ScheduleInfo, ScheduleRunInfo };

export interface ScheduleCreateParams {
  projectId: string;
  sessionId: string;
  name: string;
  prompt: string;
  /** Exactly one of cron (recurring), at (one-shot RFC3339), or dynamic
   * (self-paced via ScheduleNext) is required. */
  cron?: string;
  at?: string;
  dynamic?: boolean;
  expiresAt?: string;
}

export interface ScheduleUpdateParams {
  id: string;
  name: string;
  prompt: string;
  cron?: string;
  expiresAt?: string;
}

export const createSchedule = define<ScheduleInfo, ScheduleCreateParams>("schedule.create");
export const listSchedules = define<ScheduleInfo[]>("schedule.list");
export const updateSchedule = define<ScheduleInfo, ScheduleUpdateParams>("schedule.update");
export const deleteSchedule = define<void, { id: string }>("schedule.delete");
export const pauseSchedule = define<ScheduleInfo, { id: string }>("schedule.pause");
export const resumeSchedule = define<ScheduleInfo, { id: string }>("schedule.resume");
export const approveSchedule = define<ScheduleInfo, { id: string }>("schedule.approve");
export const runScheduleNow = define<ScheduleRunInfo, { id: string }>("schedule.run-now");
export const listScheduleRuns = define<
  ScheduleRunInfo[],
  { scheduleId: string; limit?: number; offset?: number }
>("schedule.runs");
export const markScheduleViewed = define<void, { id: string }>("schedule.mark-viewed");
