import {
  AlertTriangle,
  Archive,
  Check,
  ChevronDown,
  ChevronRight,
  GitMerge,
  HardDrive,
  Loader2,
  RefreshCw,
  RotateCcw,
  Trash2,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { PageHeader } from "~/components/layout/PageHeader";
import { SelectionBar } from "~/components/storage/SelectionBar";
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
import { deleteOrphanedWorktree, reclaimSessions } from "~/lib/api";
import type { CategoryUsage, ProjectStorage, SessionStorage } from "~/lib/generated-types";
import { deleteSession, deleteSessionsBulk } from "~/lib/session/actions";
import { canDelete, canReclaim, freedBytes, reconcile, summarize } from "~/lib/storage/selection";
import { cn, formatBytes, getErrorMessage, relativeTime } from "~/lib/utils";
import { useStorageStore } from "~/stores/storage-store";

/**
 * The page offers two verbs against a session and they answer to different bars.
 *
 * Reclaim removes the checked-out tree, the browser profile and the scratchpad,
 * keeping the session row and its git branch — the next message re-provisions
 * from the branch. Reversible, so it applies to any finished session with no
 * uncommitted work, archived ones included.
 *
 * Delete removes the row, the branch and the tree. Irreversible, so it needs the
 * server to have established that the branch's commits already exist on the
 * project's main line. That used to be approximated by `merged`, which is set
 * only when agentique itself performed the merge — false for every branch merged
 * from a terminal, which made the bulk affordance unreachable on repos worked
 * that way. `safety` is the computed answer; `merged` survives only as a badge.
 */
type DeleteTarget =
  | { kind: "orphan"; path: string; label: string; bytes: number }
  | { kind: "orphan-all"; count: number; bytes: number }
  | { kind: "session"; id: string; label: string; bytes: number }
  | { kind: "reclaim"; sessions: SessionStorage[] }
  // Bulk delete. The set is named in the dialog rather than counted, so a count
  // is never the only thing standing between a click and an irreversible action.
  | { kind: "delete-bulk"; sessions: SessionStorage[] };

const sumBytes = (sessions: SessionStorage[]) => sessions.reduce((a, s) => a + freedBytes(s), 0);

const categoryColors: Record<string, string> = {
  worktrees: "bg-sky-500",
  backups: "bg-amber-500",
  database: "bg-violet-500",
  "session-files": "bg-emerald-500",
  certs: "bg-rose-500",
  other: "bg-muted-foreground/40",
  "chrome-profiles": "bg-orange-500",
  scratchpads: "bg-teal-500",
};

export function StoragePage() {
  const ws = useWebSocket();
  const usage = useStorageStore((s) => s.usage);
  const usageLoading = useStorageStore((s) => s.usageLoading);
  const usageError = useStorageStore((s) => s.usageError);
  const fetchUsage = useStorageStore((s) => s.fetchUsage);

  const [expanded, setExpanded] = useState<Set<string>>(() => new Set());
  const [selected, setSelected] = useState<Set<string>>(() => new Set());
  const [deleteTarget, setDeleteTarget] = useState<DeleteTarget | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    fetchUsage(false);
  }, [fetchUsage]);

  const allSessions = useMemo(
    () => (usage ? usage.projects.flatMap((p) => p.sessions) : []),
    [usage],
  );

  // A selection routinely outlives the rows it was made from: the walk is cached
  // for a minute and refreshed after every action. Drop ids that are gone rather
  // than letting a later click act on rows the user can no longer see.
  useEffect(() => {
    setSelected((prev) => {
      const next = reconcile(prev, allSessions);
      return next.size === prev.size ? prev : next;
    });
  }, [allSessions]);

  const selectedSessions = useMemo(
    () => allSessions.filter((s) => selected.has(s.sessionId)),
    [allSessions, selected],
  );
  const summary = useMemo(() => summarize(selectedSessions), [selectedSessions]);

  const toggle = (id: string) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  const toggleSelected = (id: string) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  const selectAllReclaimable = () => {
    const ids = allSessions.filter(canReclaim).map((s) => s.sessionId);
    setSelected(new Set(ids));
    // Open every card holding one: the bar is about to act on rows the user
    // should be able to see.
    setExpanded(new Set(usage?.projects.map((p) => p.projectId) ?? []));
  };

  const runReclaim = async (sessions: SessionStorage[]) => {
    const res = await reclaimSessions(sessions.map((s) => s.sessionId));
    // Report what happened, not what was asked for — the server re-plans, so a
    // session that woke up in the meantime comes back as a skip.
    if (res.removed.length === 0 && res.skipped.length > 0) {
      toast.warning(`Nothing reclaimed — ${res.skipped[0]?.reason ?? "all sessions were skipped"}`);
      return;
    }
    const skipNote = res.skipped.length > 0 ? `, ${res.skipped.length} skipped` : "";
    toast.success(`Reclaimed ${formatBytes(res.freedBytes)}${skipNote}`);
    setSelected(new Set());
  };

  const confirmDelete = async () => {
    if (!deleteTarget) return;
    setBusy(true);
    try {
      if (deleteTarget.kind === "orphan") {
        await deleteOrphanedWorktree(deleteTarget.path);
        toast.success(`Removed ${deleteTarget.label}`);
      } else if (deleteTarget.kind === "orphan-all") {
        const orphans = usage?.orphans ?? [];
        const results = await Promise.allSettled(
          orphans.map((o) => deleteOrphanedWorktree(o.worktreePath)),
        );
        orphans.forEach((o, i) => {
          const r = results[i];
          if (r?.status === "rejected") {
            console.error("Failed to remove orphan", o.worktreePath, r.reason);
          }
        });
        const removed = results.filter((r) => r.status === "fulfilled").length;
        toast.success(`Removed ${removed} of ${orphans.length} orphaned worktrees`);
      } else if (deleteTarget.kind === "reclaim") {
        await runReclaim(deleteTarget.sessions);
      } else if (deleteTarget.kind === "delete-bulk") {
        const ids = deleteTarget.sessions.map((s) => s.sessionId);
        const { results } = await deleteSessionsBulk(ws, ids);
        const removed = results.filter((r) => r.success).length;
        results
          .filter((r) => !r.success)
          .forEach((r) => {
            console.error("Failed to delete session", r.sessionId, r.error);
          });
        toast.success(`Deleted ${removed} of ${ids.length} sessions`);
        setSelected(new Set());
      } else {
        await deleteSession(ws, deleteTarget.id);
        toast.success(`Deleted session ${deleteTarget.label}`);
      }
      await fetchUsage(true);
    } catch (err) {
      toast.error(getErrorMessage(err, "Action failed"));
    } finally {
      setBusy(false);
      setDeleteTarget(null);
    }
  };

  const disk = usage?.disk;
  const usedPct = disk ? Math.min(Math.round(disk.usagePercent), 100) : 0;
  const reclaimableCount = usage?.reclaimableCount ?? 0;

  return (
    <div className="flex flex-col h-full">
      <PageHeader>
        <HardDrive className="size-4 text-muted-foreground" />
        <span className="font-semibold">Storage</span>
        {usage && (
          <span className="text-xs text-muted-foreground ml-1">
            updated {relativeTime(usage.computedAt)} ago
          </span>
        )}
        <div className="ml-auto flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => fetchUsage(true)}
            disabled={usageLoading}
          >
            {usageLoading ? (
              <Loader2 className="size-3.5 animate-spin" />
            ) : (
              <RefreshCw className="size-3.5" />
            )}
            Refresh
          </Button>
        </div>
      </PageHeader>

      <div className="flex-1 overflow-y-auto p-4 space-y-4 max-w-4xl w-full mx-auto">
        {!usage && usageLoading && (
          <div className="flex items-center justify-center gap-2 py-16 text-muted-foreground text-sm">
            <Loader2 className="size-4 animate-spin" /> Calculating disk usage…
          </div>
        )}

        {!usage && !usageLoading && usageError && (
          <div className="flex flex-col items-center justify-center gap-3 py-16 text-center">
            <AlertTriangle className="size-5 text-destructive" />
            <div className="text-sm text-muted-foreground">{usageError}</div>
            <Button variant="outline" size="sm" onClick={() => fetchUsage(false)}>
              Try again
            </Button>
          </div>
        )}

        {disk && (
          <div className="rounded-lg border bg-card/40 px-4 py-3">
            <div className="flex items-center justify-between text-xs uppercase tracking-wider text-muted-foreground">
              <span>Volume — {disk.path}</span>
              <span className="tabular-nums normal-case">
                {formatBytes(disk.freeBytes)} free of {formatBytes(disk.totalBytes)}
              </span>
            </div>
            <div className="mt-2 h-2.5 w-full rounded-full bg-muted overflow-hidden">
              <div
                className={cn(
                  "h-full rounded-full transition-all",
                  usedPct >= 95 ? "bg-destructive" : usedPct >= 90 ? "bg-warning" : "bg-primary",
                )}
                style={{ width: `${usedPct}%` }}
              />
            </div>
            <div className="mt-1.5 flex items-center justify-between text-xs text-muted-foreground tabular-nums">
              <span>{usedPct}% used</span>
              {usage && (
                <span>
                  Agentique data: {formatBytes(usage.dataDirBytes)}
                  {usage.tempBytes ? ` · elsewhere: ${formatBytes(usage.tempBytes)}` : ""}
                </span>
              )}
            </div>
          </div>
        )}

        {usage && reclaimableCount > 0 && (
          <div className="flex flex-wrap items-center gap-2 rounded-lg border bg-card/40 px-4 py-3">
            <RotateCcw className="size-4 text-muted-foreground" />
            <span className="text-sm">
              <span className="font-medium tabular-nums">
                {formatBytes(usage.reclaimableBytes ?? 0)}
              </span>{" "}
              can be freed from {reclaimableCount} finished session
              {reclaimableCount === 1 ? "" : "s"}
            </span>
            <span className="text-xs text-muted-foreground">branches and history are kept</span>
            <Button variant="outline" size="sm" className="ml-auto" onClick={selectAllReclaimable}>
              Select all
            </Button>
          </div>
        )}

        {usage && (
          <CategoryBreakdown
            categories={usage.categories}
            total={usage.dataDirBytes}
            tempCategories={usage.tempCategories ?? []}
            tempTotal={usage.tempBytes ?? 0}
          />
        )}

        {usage && usage.orphans.length > 0 && (
          <div className="rounded-lg border border-warning/40 bg-warning/5 px-4 py-3">
            <div className="flex items-center gap-2 mb-2">
              <AlertTriangle className="size-4 text-warning" />
              <span className="font-medium text-sm">
                Orphaned worktrees ({usage.orphans.length})
              </span>
              <span className="text-xs text-muted-foreground">
                no matching session — safe to delete
              </span>
              <Button
                variant="outline"
                size="sm"
                className="ml-auto text-destructive hover:text-destructive"
                onClick={() =>
                  setDeleteTarget({
                    kind: "orphan-all",
                    count: usage.orphans.length,
                    bytes: usage.orphans.reduce((a, o) => a + o.bytes, 0),
                  })
                }
              >
                <Trash2 className="size-3.5" /> Delete all
              </Button>
            </div>
            <div className="space-y-0.5">
              {usage.orphans.map((o) => (
                <SessionRow
                  key={o.worktreePath}
                  session={o}
                  onDelete={() =>
                    setDeleteTarget({
                      kind: "orphan",
                      path: o.worktreePath,
                      label: o.name,
                      bytes: o.bytes,
                    })
                  }
                />
              ))}
            </div>
          </div>
        )}

        {usage && usage.projects.length > 0 && (
          <div className="space-y-2">
            <div className="text-xs uppercase tracking-wider text-muted-foreground px-1">
              By project
            </div>
            {usage.projects.map((p) => (
              <ProjectCard
                key={p.projectId}
                project={p}
                expanded={expanded.has(p.projectId)}
                selected={selected}
                onToggle={() => toggle(p.projectId)}
                onToggleSelected={toggleSelected}
                onDeleteSession={(s) =>
                  setDeleteTarget({
                    kind: "session",
                    id: s.sessionId,
                    label: s.name || s.sessionId,
                    bytes: freedBytes(s),
                  })
                }
                onReclaimSession={(s) => setDeleteTarget({ kind: "reclaim", sessions: [s] })}
                onSelectReclaimable={() => {
                  const ids = p.sessions.filter(canReclaim).map((s) => s.sessionId);
                  if (ids.length === 0) return;
                  setExpanded((prev) => new Set(prev).add(p.projectId));
                  setSelected((prev) => new Set([...prev, ...ids]));
                }}
              />
            ))}
          </div>
        )}

        {usage && usage.projects.length === 0 && usage.orphans.length === 0 && (
          <div className="text-center text-sm text-muted-foreground py-8">
            No session worktrees on disk.
          </div>
        )}

        <SelectionBar
          summary={summary}
          busy={busy}
          onClear={() => setSelected(new Set())}
          onReclaim={() => setDeleteTarget({ kind: "reclaim", sessions: summary.reclaimable })}
          onDelete={() => setDeleteTarget({ kind: "delete-bulk", sessions: summary.deletable })}
        />
      </div>

      <AlertDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => !open && !busy && setDeleteTarget(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{dialogTitle(deleteTarget)}</AlertDialogTitle>
            <AlertDialogDescription>{dialogBody(deleteTarget)}</AlertDialogDescription>
            {(deleteTarget?.kind === "delete-bulk" ||
              (deleteTarget?.kind === "reclaim" && deleteTarget.sessions.length > 1)) && (
              <ul className="mt-1 max-h-56 overflow-y-auto rounded-md border bg-muted/30 divide-y divide-border/60 text-sm">
                {deleteTarget.sessions.map((s) => (
                  <li key={s.sessionId} className="flex items-center gap-2 px-2.5 py-1.5">
                    <span className="truncate min-w-0 flex-1">{s.name || s.sessionId}</span>
                    {s.archived && (
                      <Archive
                        className="size-3 shrink-0 text-muted-foreground"
                        aria-label="archived"
                      />
                    )}
                    <span className="shrink-0 tabular-nums text-xs text-muted-foreground">
                      {formatBytes(freedBytes(s))}
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={busy}>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={(e) => {
                e.preventDefault();
                confirmDelete();
              }}
              disabled={busy}
              // AlertDialogAction is destructive by construction, which is right
              // for three of the four targets. Reclaim is reversible and must
              // not wear the colour that means "this cannot be undone".
              className={cn(
                deleteTarget?.kind === "reclaim"
                  ? "bg-primary text-primary-foreground hover:bg-primary/90"
                  : "bg-destructive text-destructive-foreground hover:bg-destructive/90",
              )}
            >
              {busy ? (
                <Loader2 className="size-3.5 animate-spin" />
              ) : deleteTarget?.kind === "reclaim" ? (
                "Reclaim"
              ) : (
                "Delete"
              )}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function dialogTitle(t: DeleteTarget | null): string {
  switch (t?.kind) {
    case "orphan-all":
      return `Delete ${t.count} orphaned worktrees?`;
    case "reclaim":
      return t.sessions.length === 1
        ? "Reclaim this session's disk?"
        : `Reclaim ${t.sessions.length} sessions' disk?`;
    case "delete-bulk":
      return `Delete ${t.sessions.length} session${t.sessions.length === 1 ? "" : "s"}?`;
    case "session":
      return "Delete session?";
    default:
      return "Delete orphaned worktree?";
  }
}

function dialogBody(t: DeleteTarget | null): string {
  switch (t?.kind) {
    case "session":
      return `This stops the session "${t.label}" and removes its worktree and branch. Frees ~${formatBytes(t.bytes)}. This cannot be undone.`;
    case "orphan-all":
      return `Permanently removes all orphaned worktree directories, freeing ~${formatBytes(t.bytes)}. This cannot be undone.`;
    case "reclaim":
      // Say what it costs, not just what it frees: the session comes back to a
      // repo that does not build until dependencies reinstall, and that cost
      // lands later, on whoever resumes it.
      return `Removes the checked-out files, browser profile and scratchpad, freeing ~${formatBytes(sumBytes(t.sessions))}. The ${t.sessions.length === 1 ? "session and its branch stay" : "sessions and their branches stay"} — the next message checks the files out again, and dependencies reinstall.`;
    case "delete-bulk":
      return `These branches add no commits the main branch does not already have, and their worktrees are clean. Deleting removes each worktree, branch and session row, freeing ~${formatBytes(sumBytes(t.sessions))}. Files git ignores — a local .env, downloaded fixtures — go with them. This cannot be undone.`;
    case "orphan":
      return `Permanently removes ${t.label}, freeing ~${formatBytes(t.bytes)}. This cannot be undone.`;
    default:
      return "";
  }
}

function CategoryBreakdown({
  categories,
  total,
  tempCategories,
  tempTotal,
}: {
  categories: CategoryUsage[];
  total: number;
  tempCategories: CategoryUsage[];
  tempTotal: number;
}) {
  const shown = categories.filter((c) => c.bytes > 0);
  const shownTemp = tempCategories.filter((c) => c.bytes > 0);
  if (shown.length === 0 && shownTemp.length === 0) return null;
  return (
    <div className="rounded-lg border bg-card/40 px-4 py-3">
      <div className="text-xs uppercase tracking-wider text-muted-foreground mb-2">
        Breakdown — data directory
      </div>
      <CategoryBars categories={shown} total={total} />
      {shownTemp.length > 0 && (
        <>
          {/* Its own group and its own total: "Agentique data" is a claim about
              one directory, and quietly widening it would make that number wrong
              in a different way. */}
          <div className="mt-3 pt-3 border-t text-xs uppercase tracking-wider text-muted-foreground mb-2">
            Elsewhere — temporary files
          </div>
          <CategoryBars categories={shownTemp} total={tempTotal} />
        </>
      )}
    </div>
  );
}

function CategoryBars({ categories, total }: { categories: CategoryUsage[]; total: number }) {
  return (
    <div className="space-y-1.5">
      {categories.map((c) => {
        const pct = total > 0 ? (c.bytes / total) * 100 : 0;
        return (
          <div key={c.key} className="flex items-center gap-2 text-xs">
            <span className="w-24 shrink-0 text-muted-foreground">{c.label}</span>
            <div className="flex-1 h-1.5 rounded-full bg-muted overflow-hidden">
              <div
                className={cn("h-full rounded-full", categoryColors[c.key] ?? "bg-primary")}
                style={{ width: `${Math.max(pct, 1)}%` }}
              />
            </div>
            <span className="w-16 text-right tabular-nums shrink-0">{formatBytes(c.bytes)}</span>
          </div>
        );
      })}
    </div>
  );
}

function ProjectCard({
  project,
  expanded,
  selected,
  onToggle,
  onToggleSelected,
  onDeleteSession,
  onReclaimSession,
  onSelectReclaimable,
}: {
  project: ProjectStorage;
  expanded: boolean;
  selected: Set<string>;
  onToggle: () => void;
  onToggleSelected: (id: string) => void;
  onDeleteSession: (s: SessionStorage) => void;
  onReclaimSession: (s: SessionStorage) => void;
  onSelectReclaimable: () => void;
}) {
  const reclaimable = project.sessions.filter(canReclaim);
  return (
    <div className="rounded-lg border bg-card/40">
      <div className="group flex items-center gap-2 w-full px-3 py-2.5 hover:bg-muted/30 transition-colors rounded-lg">
        <button
          type="button"
          onClick={onToggle}
          className="flex items-center gap-2 flex-1 min-w-0 text-left"
        >
          {expanded ? (
            <ChevronDown className="size-4 text-muted-foreground shrink-0" />
          ) : (
            <ChevronRight className="size-4 text-muted-foreground shrink-0" />
          )}
          <span
            className="size-2.5 rounded-full shrink-0"
            style={{ backgroundColor: project.color || "var(--color-muted-foreground)" }}
          />
          <span className="font-medium text-sm truncate">{project.name || project.slug}</span>
          <span className="text-xs text-muted-foreground shrink-0">
            {project.sessions.length} session{project.sessions.length === 1 ? "" : "s"}
            {reclaimable.length > 0 && (
              <span className="text-muted-foreground/70"> · {reclaimable.length} reclaimable</span>
            )}
          </span>
        </button>
        {reclaimable.length > 0 && (
          <button
            type="button"
            onClick={onSelectReclaimable}
            className="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-xs text-muted-foreground hover:text-foreground hover:bg-muted transition-all opacity-0 group-hover:opacity-100 shrink-0"
            title={`Select ${reclaimable.length} reclaimable session${reclaimable.length === 1 ? "" : "s"}`}
          >
            <Check className="size-3" />
            select ({reclaimable.length})
          </button>
        )}
        <span className="text-sm tabular-nums font-medium shrink-0">
          {formatBytes(project.totalBytes)}
        </span>
      </div>
      {expanded && (
        <div className="px-3 pb-2 space-y-0.5">
          {project.sessions.map((s) => (
            <SessionRow
              key={s.sessionId}
              session={s}
              selected={selected.has(s.sessionId)}
              onToggleSelected={() => onToggleSelected(s.sessionId)}
              onReclaim={canReclaim(s) ? () => onReclaimSession(s) : undefined}
              onDelete={() => onDeleteSession(s)}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function SessionRow({
  session,
  selected,
  onToggleSelected,
  onReclaim,
  onDelete,
}: {
  session: SessionStorage;
  selected?: boolean;
  onToggleSelected?: () => void;
  onReclaim?: () => void;
  onDelete: () => void;
}) {
  const temp = session.tempBytes ?? 0;
  return (
    <div
      className={cn(
        "group flex items-center gap-2 px-2 py-1.5 rounded-md hover:bg-muted/40 text-sm",
        session.archived && "text-muted-foreground",
        selected && "bg-muted/60",
      )}
    >
      {/* A real checkbox rather than a styled button: this is the one control on
          the page a keyboard user has to reach for every row, and the native
          element already knows how to be one. */}
      {onToggleSelected && (
        <input
          type="checkbox"
          checked={selected === true}
          onChange={onToggleSelected}
          aria-label={`Select ${session.name || session.sessionId}`}
          className="shrink-0 size-4 accent-primary cursor-pointer"
        />
      )}
      <span className="truncate min-w-0 flex-1">
        {session.name || (session.orphaned ? session.worktreePath : session.sessionId)}
      </span>
      {/* `merged` is now a fact about how the merge happened, not the gate — the
          gate is `safety`, which git answered. Kept because it still explains a
          row at a glance. */}
      {session.merged && (
        <Badge
          variant="outline"
          className="text-[10px] shrink-0 gap-1 border-sky-500/30 text-sky-600 dark:text-sky-400"
        >
          <GitMerge className="size-2.5" />
          merged
        </Badge>
      )}
      {!session.orphaned &&
        (session.archived ? (
          <Badge
            variant="outline"
            className="text-[10px] shrink-0 gap-1 border-emerald-500/30 text-emerald-600 dark:text-emerald-400"
          >
            <Archive className="size-2.5" />
            archived
          </Badge>
        ) : (
          session.state && (
            <Badge
              variant="outline"
              className="text-[10px] shrink-0 border-primary/40 text-primary"
            >
              {session.state}
            </Badge>
          )
        ))}
      {/* Why Delete is unavailable, on the row that causes it. The bar only ever
          reports how many blocked it; this is where the reason lives. */}
      {!session.orphaned && !canDelete(session) && session.safetyReason && (
        <span className="text-xs text-muted-foreground/80 shrink-0 hidden sm:inline">
          {session.safetyReason}
        </span>
      )}
      {!session.orphaned && session.updatedAt && (
        <span className="text-xs text-muted-foreground tabular-nums shrink-0">
          {relativeTime(session.updatedAt)} ago
        </span>
      )}
      <span
        className="w-16 text-right tabular-nums text-muted-foreground shrink-0"
        title={
          temp > 0
            ? `${formatBytes(session.bytes)} worktree + ${formatBytes(temp)} temp`
            : undefined
        }
      >
        {formatBytes(freedBytes(session))}
      </span>
      {onReclaim && (
        <button
          type="button"
          onClick={onReclaim}
          className="shrink-0 rounded p-1 text-muted-foreground opacity-0 group-hover:opacity-100 hover:bg-muted hover:text-foreground transition-all"
          title="Free the disk, keep the session"
        >
          <RotateCcw className="size-3.5" />
        </button>
      )}
      <button
        type="button"
        onClick={onDelete}
        className="shrink-0 rounded p-1 text-muted-foreground opacity-0 group-hover:opacity-100 hover:bg-destructive/10 hover:text-destructive transition-all"
        title="Delete"
      >
        <Trash2 className="size-3.5" />
      </button>
    </div>
  );
}
