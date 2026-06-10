import {
  AlertTriangle,
  ChevronDown,
  ChevronRight,
  HardDrive,
  Loader2,
  RefreshCw,
  Trash2,
} from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { PageHeader } from "~/components/layout/PageHeader";
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
import { deleteOrphanedWorktree } from "~/lib/api";
import type { CategoryUsage, ProjectStorage, SessionStorage } from "~/lib/generated-types";
import { deleteSession } from "~/lib/session/actions";
import { cn, formatBytes, getErrorMessage, relativeTime } from "~/lib/utils";
import { useStorageStore } from "~/stores/storage-store";

type DeleteTarget =
  | { kind: "orphan"; path: string; label: string; bytes: number }
  | { kind: "orphan-all"; count: number; bytes: number }
  | { kind: "session"; id: string; label: string; bytes: number };

const categoryColors: Record<string, string> = {
  worktrees: "bg-sky-500",
  backups: "bg-amber-500",
  database: "bg-violet-500",
  "session-files": "bg-emerald-500",
  certs: "bg-rose-500",
  other: "bg-muted-foreground/40",
};

export function StoragePage() {
  const ws = useWebSocket();
  const usage = useStorageStore((s) => s.usage);
  const usageLoading = useStorageStore((s) => s.usageLoading);
  const usageError = useStorageStore((s) => s.usageError);
  const fetchUsage = useStorageStore((s) => s.fetchUsage);

  const [expanded, setExpanded] = useState<Set<string>>(() => new Set());
  const [deleteTarget, setDeleteTarget] = useState<DeleteTarget | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    fetchUsage(false);
  }, [fetchUsage]);

  const toggle = (id: string) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

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
      } else {
        await deleteSession(ws, deleteTarget.id);
        toast.success(`Deleted session ${deleteTarget.label}`);
      }
      await fetchUsage(true);
    } catch (err) {
      toast.error(getErrorMessage(err, "Delete failed"));
    } finally {
      setBusy(false);
      setDeleteTarget(null);
    }
  };

  const disk = usage?.disk;
  const usedPct = disk ? Math.min(Math.round(disk.usagePercent), 100) : 0;

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
              {usage && <span>Agentique data: {formatBytes(usage.dataDirBytes)}</span>}
            </div>
          </div>
        )}

        {usage && <CategoryBreakdown categories={usage.categories} total={usage.dataDirBytes} />}

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
                onToggle={() => toggle(p.projectId)}
                onDeleteSession={(s) =>
                  setDeleteTarget({
                    kind: "session",
                    id: s.sessionId,
                    label: s.name || s.sessionId,
                    bytes: s.bytes,
                  })
                }
              />
            ))}
          </div>
        )}

        {usage && usage.projects.length === 0 && usage.orphans.length === 0 && (
          <div className="text-center text-sm text-muted-foreground py-8">
            No session worktrees on disk.
          </div>
        )}
      </div>

      <AlertDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => !open && !busy && setDeleteTarget(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {deleteTarget?.kind === "orphan-all"
                ? `Delete ${deleteTarget.count} orphaned worktrees?`
                : deleteTarget?.kind === "session"
                  ? "Delete session?"
                  : "Delete orphaned worktree?"}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {deleteTarget?.kind === "session"
                ? `This stops the session "${deleteTarget.label}" and removes its worktree and branch. Frees ~${formatBytes(deleteTarget.bytes)}. This cannot be undone.`
                : deleteTarget?.kind === "orphan-all"
                  ? `Permanently removes all orphaned worktree directories, freeing ~${formatBytes(deleteTarget.bytes)}. This cannot be undone.`
                  : deleteTarget
                    ? `Permanently removes ${deleteTarget.label}, freeing ~${formatBytes(deleteTarget.bytes)}. This cannot be undone.`
                    : ""}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={busy}>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={(e) => {
                e.preventDefault();
                confirmDelete();
              }}
              disabled={busy}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {busy ? <Loader2 className="size-3.5 animate-spin" /> : "Delete"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function CategoryBreakdown({ categories, total }: { categories: CategoryUsage[]; total: number }) {
  const shown = categories.filter((c) => c.bytes > 0);
  if (shown.length === 0) return null;
  return (
    <div className="rounded-lg border bg-card/40 px-4 py-3">
      <div className="text-xs uppercase tracking-wider text-muted-foreground mb-2">Breakdown</div>
      <div className="space-y-1.5">
        {shown.map((c) => {
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
    </div>
  );
}

function ProjectCard({
  project,
  expanded,
  onToggle,
  onDeleteSession,
}: {
  project: ProjectStorage;
  expanded: boolean;
  onToggle: () => void;
  onDeleteSession: (s: SessionStorage) => void;
}) {
  return (
    <div className="rounded-lg border bg-card/40">
      <button
        type="button"
        onClick={onToggle}
        className="flex items-center gap-2 w-full px-3 py-2.5 text-left hover:bg-muted/30 transition-colors rounded-lg"
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
        <span className="text-xs text-muted-foreground">
          {project.sessions.length} session{project.sessions.length === 1 ? "" : "s"}
        </span>
        <span className="ml-auto text-sm tabular-nums font-medium shrink-0">
          {formatBytes(project.totalBytes)}
        </span>
      </button>
      {expanded && (
        <div className="px-3 pb-2 space-y-0.5">
          {project.sessions.map((s) => (
            <SessionRow key={s.sessionId} session={s} onDelete={() => onDeleteSession(s)} />
          ))}
        </div>
      )}
    </div>
  );
}

function SessionRow({ session, onDelete }: { session: SessionStorage; onDelete: () => void }) {
  return (
    <div className="group flex items-center gap-2 px-2 py-1.5 rounded-md hover:bg-muted/40 text-sm">
      <span className="truncate min-w-0 flex-1">
        {session.name || (session.orphaned ? session.worktreePath : session.sessionId)}
      </span>
      {!session.orphaned && session.state && (
        <Badge variant="outline" className="text-[10px] shrink-0">
          {session.state}
        </Badge>
      )}
      {!session.orphaned && session.updatedAt && (
        <span className="text-xs text-muted-foreground tabular-nums shrink-0">
          {relativeTime(session.updatedAt)} ago
        </span>
      )}
      <span className="w-16 text-right tabular-nums text-muted-foreground shrink-0">
        {formatBytes(session.bytes)}
      </span>
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
