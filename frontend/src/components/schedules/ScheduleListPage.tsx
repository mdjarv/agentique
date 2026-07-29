import {
  AlertTriangle,
  Check,
  ChevronDown,
  ChevronRight,
  Clock,
  Loader2,
  Pause,
  Pencil,
  Play,
  Plus,
  Trash2,
} from "lucide-react";
import { useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { PageHeader } from "~/components/layout/PageHeader";
import { ScheduleFormDialog } from "~/components/schedules/ScheduleFormDialog";
import {
  agoText,
  formatRunDuration,
  humanCadence,
  pauseReasonText,
  runStatusMeta,
  untilText,
} from "~/components/schedules/schedule-format";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "~/components/ui/alert-dialog";
import { Badge } from "~/components/ui/badge";
import { Button } from "~/components/ui/button";
import { useWebSocket } from "~/hooks/useWebSocket";
import type { ScheduleInfo, ScheduleRunInfo } from "~/lib/schedule-actions";
import {
  approveSchedule,
  deleteSchedule,
  listScheduleRuns,
  pauseSchedule,
  resumeSchedule,
  runScheduleNow,
} from "~/lib/schedule-actions";
import { cn, getErrorMessage } from "~/lib/utils";
import { useChatStore } from "~/stores/chat-store";
import { EMPTY_RUNS, useScheduleStore } from "~/stores/schedule-store";

// Global schedules page (docs/scheduled-loops.md "Surfaces"): every scheduled
// loop across projects, grouped by what deserves attention first. Run history
// per row is fetched lazily on expand; live updates arrive via the store's WS
// subscriptions.
export function ScheduleListPage() {
  const schedules = useScheduleStore((s) => s.schedules);
  const loaded = useScheduleStore((s) => s.loaded);
  const ws = useWebSocket();

  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<ScheduleInfo | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<ScheduleInfo | null>(null);
  const [deleting, setDeleting] = useState(false);

  const { attention, active, parked } = useMemo(() => {
    const all = Object.values(schedules);
    const attention = all.filter((s) => s.attention !== "");
    const active = all.filter((s) => s.attention === "" && s.enabled);
    const parked = all.filter((s) => s.attention === "" && !s.enabled);
    attention.sort((a, b) => b.updatedAt.localeCompare(a.updatedAt));
    // Soonest next fire first; parked ('' = no next fire) sink to the bottom.
    active.sort((a, b) => {
      if (!a.nextRunAt) return b.nextRunAt ? 1 : a.name.localeCompare(b.name);
      if (!b.nextRunAt) return -1;
      return a.nextRunAt.localeCompare(b.nextRunAt);
    });
    parked.sort((a, b) => b.updatedAt.localeCompare(a.updatedAt));
    return { attention, active, parked };
  }, [schedules]);

  const total = attention.length + active.length + parked.length;

  const openCreate = () => {
    setEditing(null);
    setFormOpen(true);
  };
  const openEdit = (s: ScheduleInfo) => {
    setEditing(s);
    setFormOpen(true);
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    setDeleting(true);
    try {
      await deleteSchedule(ws, { id: deleteTarget.id });
      useScheduleStore.getState().removeSchedule(deleteTarget.id);
      toast.success(`Deleted "${deleteTarget.name}"`);
      setDeleteTarget(null);
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to delete schedule"));
    } finally {
      setDeleting(false);
    }
  };

  return (
    <div className="flex flex-col h-full">
      <PageHeader>
        <Clock className="size-4 text-muted-foreground" />
        <span className="font-semibold">Schedules</span>
      </PageHeader>
      <div className="flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-5xl px-6 py-8 max-md:px-4 space-y-10">
          <header className="flex flex-wrap items-start justify-between gap-3">
            <div className="space-y-2">
              <h1 className="text-2xl font-semibold tracking-tight">Schedules</h1>
              <p className="text-sm text-muted-foreground max-w-prose leading-relaxed">
                Scheduled loops re-run a prompt on a session automatically — on a cadence or once at
                a set time. Results land in the target session's timeline.
              </p>
            </div>
            <Button size="sm" onClick={openCreate}>
              <Plus className="size-3.5" />
              New schedule
            </Button>
          </header>

          {!loaded ? (
            <EmptyState>Loading…</EmptyState>
          ) : total === 0 ? (
            <EmptyState>
              <div className="space-y-3">
                <p>
                  Schedules re-run a prompt on a session automatically — a deploy check every 30
                  minutes, a nightly summary, a one-shot reminder. Create one to get a loop going.
                </p>
                <Button size="sm" variant="outline" onClick={openCreate}>
                  <Plus className="size-3.5" />
                  Create a schedule
                </Button>
              </div>
            </EmptyState>
          ) : (
            <>
              {attention.length > 0 && (
                <Section title="Needs attention">
                  <ScheduleList
                    schedules={attention}
                    onEdit={openEdit}
                    onDelete={setDeleteTarget}
                  />
                </Section>
              )}
              {active.length > 0 && (
                <Section title="Active">
                  <ScheduleList schedules={active} onEdit={openEdit} onDelete={setDeleteTarget} />
                </Section>
              )}
              {parked.length > 0 && (
                <Section title="Paused & finished">
                  <ScheduleList schedules={parked} onEdit={openEdit} onDelete={setDeleteTarget} />
                </Section>
              )}
            </>
          )}
        </div>
      </div>

      {formOpen && (
        <ScheduleFormDialog schedule={editing ?? undefined} onClose={() => setFormOpen(false)} />
      )}

      <AlertDialog open={!!deleteTarget} onOpenChange={(o) => !o && setDeleteTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete schedule</AlertDialogTitle>
            <AlertDialogDescription>
              Delete "{deleteTarget?.name}"? This removes the schedule and its run history. The
              target session is not touched.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={handleDelete} disabled={deleting}>
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

// ─── Sections ───────────────────────────────────────────

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="space-y-3">
      <h2 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground">
        {title}
      </h2>
      {children}
    </section>
  );
}

function EmptyState({ children }: { children: React.ReactNode }) {
  return (
    <div className="rounded-lg border border-dashed bg-card/20 px-6 py-8 text-center text-sm text-muted-foreground">
      {children}
    </div>
  );
}

function ScheduleList({
  schedules,
  onEdit,
  onDelete,
}: {
  schedules: ScheduleInfo[];
  onEdit: (s: ScheduleInfo) => void;
  onDelete: (s: ScheduleInfo) => void;
}) {
  return (
    <ul className="space-y-2">
      {schedules.map((s) => (
        <ScheduleRow
          key={s.id}
          schedule={s}
          onEdit={() => onEdit(s)}
          onDelete={() => onDelete(s)}
        />
      ))}
    </ul>
  );
}

// ─── Row ────────────────────────────────────────────────

function accentClass(s: ScheduleInfo): string {
  if (s.attention === "failed") return "bg-destructive";
  if (s.attention === "action_needed") return "bg-amber-500";
  if (s.pauseReason === "pending-approval") return "bg-sky-500";
  if (s.enabled) return "bg-emerald-500";
  return "bg-muted-foreground/40";
}

function ScheduleRow({
  schedule,
  onEdit,
  onDelete,
}: {
  schedule: ScheduleInfo;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const ws = useWebSocket();
  const runs = useScheduleStore((s) => s.runs[schedule.id] ?? EMPTY_RUNS);
  const sessionName = useChatStore((s) => s.sessions[schedule.sessionId]?.meta.name);
  const [expanded, setExpanded] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);
  const fetchedRef = useRef(false);

  const pendingApproval = schedule.pauseReason === "pending-approval";
  const newestRun = runs.length > 0 ? runs[0] : undefined;

  const toggleExpand = async () => {
    const next = !expanded;
    setExpanded(next);
    if (!next || fetchedRef.current) return;
    fetchedRef.current = true;
    try {
      const page = await listScheduleRuns(ws, { scheduleId: schedule.id, limit: 10 });
      useScheduleStore.getState().setRuns(schedule.id, page);
    } catch (err) {
      fetchedRef.current = false;
      toast.error(getErrorMessage(err, "Failed to load run history"));
    }
  };

  const act = async (kind: string, fn: () => Promise<void>) => {
    setBusy(kind);
    try {
      await fn();
    } catch (err) {
      toast.error(getErrorMessage(err, `Failed to ${kind}`));
    } finally {
      setBusy(null);
    }
  };

  const store = useScheduleStore.getState;
  const runNow = () =>
    act("run now", async () => {
      const run = await runScheduleNow(ws, { id: schedule.id });
      store().upsertRun(run);
      toast.success(`Run queued for "${schedule.name}"`);
    });
  const pause = () =>
    act("pause", async () => {
      store().upsertSchedule(await pauseSchedule(ws, { id: schedule.id }));
      toast.success(`Paused "${schedule.name}"`);
    });
  const resume = () =>
    act("resume", async () => {
      store().upsertSchedule(await resumeSchedule(ws, { id: schedule.id }));
      toast.success(`Resumed "${schedule.name}"`);
    });
  const approve = () =>
    act("approve", async () => {
      store().upsertSchedule(await approveSchedule(ws, { id: schedule.id }));
      toast.success(`Approved "${schedule.name}"`);
    });

  const iconBtn = "size-7 text-muted-foreground hover:text-foreground";

  return (
    <li className="rounded-lg border bg-card/40">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2 px-3 py-2.5">
        <span className={cn("w-1 self-stretch rounded-full min-h-9", accentClass(schedule))} />
        <button
          type="button"
          onClick={toggleExpand}
          className="flex-1 min-w-[14rem] text-left cursor-pointer"
        >
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
            {expanded ? (
              <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" />
            ) : (
              <ChevronRight className="size-3.5 shrink-0 text-muted-foreground" />
            )}
            <span className="font-medium text-sm truncate">{schedule.name || "Untitled"}</span>
            {schedule.attention === "failed" && (
              <Badge variant="destructive" className="gap-1">
                <AlertTriangle className="size-3" />
                Loop paused
              </Badge>
            )}
            {schedule.attention === "action_needed" && (
              <Badge variant="capture" className="gap-1">
                Needs you
              </Badge>
            )}
            {schedule.consecutiveFailures > 0 && (
              <Badge variant="outline" className="text-destructive tabular-nums">
                {schedule.consecutiveFailures} fail
                {schedule.consecutiveFailures === 1 ? "" : "s"}
              </Badge>
            )}
            {!schedule.enabled && schedule.pauseReason && (
              <span className="text-xs text-muted-foreground">
                {pauseReasonText(schedule.pauseReason)}
              </span>
            )}
          </div>
          <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 pl-[1.375rem] text-xs text-muted-foreground">
            <span className="truncate max-w-[16rem]">
              {sessionName || schedule.sessionId.slice(0, 8)}
            </span>
            <span>{humanCadence(schedule)}</span>
            {schedule.enabled &&
              (schedule.nextRunAt ? (
                <span>next {untilText(schedule.nextRunAt)}</span>
              ) : (
                <span>parked</span>
              ))}
            {newestRun ? (
              <span className="inline-flex items-center gap-1.5">
                <span className={cn("size-1.5 rounded-full", runStatusMeta(newestRun).dotClass)} />
                {runStatusMeta(newestRun).label}
                {schedule.lastRunAt && ` · ${agoText(schedule.lastRunAt)}`}
              </span>
            ) : (
              schedule.lastRunAt && <span>last run {agoText(schedule.lastRunAt)}</span>
            )}
            {runs.length > 1 && <RunStrip runs={runs} />}
          </div>
        </button>

        <div className="ml-auto flex items-center gap-0.5">
          {pendingApproval ? (
            <>
              <Button
                size="sm"
                variant="outline"
                disabled={busy !== null}
                onClick={approve}
                title="Approve this agent-created schedule"
              >
                {busy === "approve" ? (
                  <Loader2 className="size-3.5 animate-spin" />
                ) : (
                  <Check className="size-3.5" />
                )}
                Approve
              </Button>
              <Button
                size="sm"
                variant="ghost"
                className="text-muted-foreground hover:text-destructive"
                disabled={busy !== null}
                onClick={onDelete}
                title="Deny (deletes the schedule)"
              >
                Deny
              </Button>
            </>
          ) : (
            <>
              <Button
                size="icon"
                variant="ghost"
                className={iconBtn}
                disabled={busy !== null}
                onClick={runNow}
                title="Run now"
              >
                {busy === "run now" ? (
                  <Loader2 className="size-3.5 animate-spin" />
                ) : (
                  <Play className="size-3.5" />
                )}
              </Button>
              {schedule.enabled ? (
                <Button
                  size="icon"
                  variant="ghost"
                  className={iconBtn}
                  disabled={busy !== null}
                  onClick={pause}
                  title="Pause"
                >
                  {busy === "pause" ? (
                    <Loader2 className="size-3.5 animate-spin" />
                  ) : (
                    <Pause className="size-3.5" />
                  )}
                </Button>
              ) : (
                schedule.mode !== "once" && (
                  <Button
                    size="icon"
                    variant="ghost"
                    className={iconBtn}
                    disabled={busy !== null}
                    onClick={resume}
                    title="Resume"
                  >
                    {busy === "resume" ? (
                      <Loader2 className="size-3.5 animate-spin" />
                    ) : (
                      <Play className="size-3.5 text-emerald-500" />
                    )}
                  </Button>
                )
              )}
            </>
          )}
          <Button
            size="icon"
            variant="ghost"
            className={iconBtn}
            disabled={busy !== null}
            onClick={onEdit}
            title="Edit"
          >
            <Pencil className="size-3.5" />
          </Button>
          <Button
            size="icon"
            variant="ghost"
            className="size-7 text-muted-foreground hover:text-destructive"
            disabled={busy !== null}
            onClick={onDelete}
            title="Delete"
          >
            <Trash2 className="size-3.5" />
          </Button>
        </div>
      </div>

      {expanded && (
        <div className="border-t px-3 py-2 pl-[1.875rem]">
          {runs.length === 0 ? (
            <p className="py-1 text-xs text-muted-foreground">
              {fetchedRef.current ? "No runs yet." : "Loading runs…"}
            </p>
          ) : (
            <ul className="space-y-1">
              {runs.slice(0, 10).map((run) => (
                <RunRow key={run.id} run={run} />
              ))}
            </ul>
          )}
        </div>
      )}
    </li>
  );
}

// ─── Run history ────────────────────────────────────────

/** Last-10-runs strip, oldest → newest left to right. */
function RunStrip({ runs }: { runs: ScheduleRunInfo[] }) {
  const strip = useMemo(() => [...runs.slice(0, 10)].reverse(), [runs]);
  return (
    <span className="inline-flex items-center gap-1">
      {strip.map((run) => (
        <span
          key={run.id}
          className={cn("size-1.5 rounded-full", runStatusMeta(run).dotClass)}
          title={`${runStatusMeta(run).label}${run.finishedAt ? ` · ${agoText(run.finishedAt)}` : ""}`}
        />
      ))}
    </span>
  );
}

function RunRow({ run }: { run: ScheduleRunInfo }) {
  const meta = runStatusMeta(run);
  const when = run.firedAt || run.scheduledFor || run.createdAt;
  return (
    <li className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs">
      <span className={cn("size-1.5 shrink-0 rounded-full", meta.dotClass)} />
      <span className={cn("w-16 shrink-0", meta.textClass)}>{meta.label}</span>
      <span className="text-muted-foreground tabular-nums">{when ? agoText(when) : ""}</span>
      {run.durationMs > 0 && (
        <span className="text-muted-foreground tabular-nums">
          {formatRunDuration(run.durationMs)}
        </span>
      )}
      {(run.summary || run.error || run.reason) && (
        <span className="min-w-0 flex-1 truncate text-muted-foreground">
          {run.summary || run.error || run.reason}
        </span>
      )}
    </li>
  );
}
