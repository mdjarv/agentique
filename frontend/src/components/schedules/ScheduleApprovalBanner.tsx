import { Check, ChevronDown, ChevronRight, Clock, X } from "lucide-react";
import { useCallback, useState } from "react";
import { toast } from "sonner";
import { humanCadence } from "~/components/schedules/schedule-format";
import { Button } from "~/components/ui/button";
import { useWebSocket } from "~/hooks/useWebSocket";
import type { ScheduleInfo } from "~/lib/schedule-actions";
import { approveSchedule, deleteSchedule } from "~/lib/schedule-actions";
import { getErrorMessage } from "~/lib/utils";
import { useScheduleStore } from "~/stores/schedule-store";

interface ScheduleApprovalBannerProps {
  schedule: ScheduleInfo;
}

/**
 * Approval banner for an agent-proposed schedule (pauseReason
 * "pending-approval"). Approve enables it; Deny deletes it — the agent
 * observes the outcome via schedule state (docs/scheduled-loops.md).
 */
export function ScheduleApprovalBanner({ schedule }: ScheduleApprovalBannerProps) {
  const ws = useWebSocket();
  const [resolving, setResolving] = useState(false);
  const [promptOpen, setPromptOpen] = useState(false);

  const handleApprove = useCallback(async () => {
    setResolving(true);
    try {
      const updated = await approveSchedule(ws, { id: schedule.id });
      useScheduleStore.getState().upsertSchedule(updated);
      toast.success(`Schedule "${schedule.name}" approved`);
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to approve schedule"));
      setResolving(false);
    }
  }, [ws, schedule.id, schedule.name]);

  const handleDeny = useCallback(async () => {
    setResolving(true);
    try {
      await deleteSchedule(ws, { id: schedule.id });
      useScheduleStore.getState().removeSchedule(schedule.id);
      toast(`Schedule "${schedule.name}" denied`);
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to deny schedule"));
      setResolving(false);
    }
  }, [ws, schedule.id, schedule.name]);

  return (
    <div className="mx-4 mb-3 rounded-lg border border-primary/30 bg-primary/5 p-3 space-y-2 shrink-0">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm font-medium">
        <Clock className="h-4 w-4 text-primary shrink-0" />
        <span>Agent proposed a schedule:</span>
        <span className="min-w-0 truncate">{schedule.name}</span>
        <span className="text-muted-foreground font-normal">&mdash; {humanCadence(schedule)}</span>
      </div>

      <button
        type="button"
        className="w-full text-left rounded border border-border/50 bg-background px-3 py-2 hover:bg-muted/30 transition-colors"
        onClick={() => setPromptOpen((v) => !v)}
      >
        <div className="flex items-center gap-1 text-[10px] uppercase tracking-wider text-muted-foreground">
          {promptOpen ? <ChevronDown className="size-3" /> : <ChevronRight className="size-3" />}
          Prompt
        </div>
        <div
          className={
            promptOpen
              ? "text-xs text-muted-foreground mt-1 whitespace-pre-wrap break-words"
              : "text-xs text-muted-foreground mt-0.5 line-clamp-2"
          }
        >
          {schedule.prompt}
        </div>
      </button>

      <div className="flex items-center gap-2 pt-1">
        <Button size="xs" disabled={resolving} onClick={handleApprove}>
          <Check className="h-3 w-3" />
          Approve
        </Button>
        <Button size="xs" variant="outline" disabled={resolving} onClick={handleDeny}>
          <X className="h-3 w-3" />
          Deny
        </Button>
      </div>
    </div>
  );
}
