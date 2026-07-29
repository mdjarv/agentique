import { useEffect } from "react";
import type { useWebSocket } from "~/hooks/useWebSocket";
import { useScheduleStore } from "~/stores/schedule-store";

/** Subscribes to scheduled-loop pushes (broadcast globally, all projects). */
export function useScheduleSubscriptions(ws: ReturnType<typeof useWebSocket>) {
  useEffect(() => {
    const unsubUpdated = ws.subscribe("schedule.updated", (payload) => {
      useScheduleStore.getState().upsertSchedule(payload);
    });
    const unsubDeleted = ws.subscribe("schedule.deleted", (payload) => {
      useScheduleStore.getState().removeSchedule(payload.id);
    });
    const unsubRun = ws.subscribe("schedule.run", (payload) => {
      useScheduleStore.getState().upsertRun(payload);
    });

    return () => {
      unsubUpdated();
      unsubDeleted();
      unsubRun();
    };
  }, [ws]);
}
