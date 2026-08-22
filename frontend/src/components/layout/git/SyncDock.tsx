/**
 * The sync dock — the rail's last band, under the session list and above the
 * footer.
 *
 * At rest it is one line: an amber dot, the faces of the repos that have
 * drifted, and how many actions are outstanding. Expanded it lists one row per
 * drifted *checkout* (so a repo out of sync on two machines is two rows, one
 * face) with its single action. It runs only the two mechanical operations —
 * push and fast-forward pull — and hands a diverged checkout to a session with
 * a rebase prompt, exactly as the old project row's pills did.
 *
 * Freshness is part of the design, not a footnote: ahead/behind is only as
 * true as the last `git fetch`, so the dock reports its age and says "sync
 * unknown" rather than presenting a stale count as fact.
 */
import { useNavigate } from "@tanstack/react-router";
import { ChevronRight, RefreshCw } from "lucide-react";
import { useCallback, useMemo, useState } from "react";
import { toast } from "sonner";
import { useNow } from "~/hooks/useNow";
import { useProjectIcon } from "~/hooks/useProjectIcon";
import { useTheme } from "~/hooks/useTheme";
import { useWebSocket } from "~/hooks/useWebSocket";
import { fetchSweep, STALE_AFTER_MS } from "~/lib/git/sync-sweep";
import { pullProject, pushProject } from "~/lib/project-actions";
import { getProjectColor } from "~/lib/project-colors";
import { cn, getErrorMessage } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import { useMachineStore } from "~/stores/machine-store";
import { useUIStore } from "~/stores/ui-store";
import {
  deriveSyncRows,
  mechanicalRows,
  type SyncChip,
  type SyncRowInput,
  type SyncRowVM,
  summarize,
} from "./sync-derive";

/** How many faces the collapsed line shows before it starts counting. */
const MAX_CHIPS = 3;

/** Rebase prompt for a checkout that can't fast-forward. */
function buildRebasePrompt(row: SyncRowVM): string {
  const branch = row.slug;
  return (
    `Project ${branch} is behind its remote by ${row.behind} commits and ahead by ${row.ahead}` +
    (row.uncommitted > 0 ? `, with ${row.uncommitted} uncommitted files` : "") +
    `. Pull is non-fast-forward. Please rebase local commits onto upstream, resolve any ` +
    `conflicts, and verify tests pass before pushing.`
  );
}

function useSyncRows(): SyncRowVM[] {
  const projects = useAppStore((s) => s.projects);
  const gitStatus = useAppStore((s) => s.projectGitStatus);
  const machines = useMachineStore((s) => s.machines);
  const { resolvedTheme } = useTheme();

  return useMemo(() => {
    const projectIds = projects.map((p) => p.id);
    const inputs: SyncRowInput[] = projects.map((project) => {
      const color = getProjectColor(project.color, project.id, projectIds, resolvedTheme);
      return {
        project,
        status: gitStatus[project.id],
        machineLabel: project.machineId ? machines[project.machineId]?.label : undefined,
        colorBg: color.bg,
        colorFg: color.fg,
      };
    });
    return deriveSyncRows(inputs);
  }, [projects, gitStatus, machines, resolvedTheme]);
}

/** Age of the oldest fetch behind the rows on screen — the dock's honesty. */
function useFetchAge(rows: SyncRowVM[], now: number): { oldest: number | null; stale: boolean } {
  const stamps = useAppStore((s) => s.projectGitFetchedAt);
  return useMemo(() => {
    let oldest: number | null = null;
    let missing = false;
    for (const row of rows) {
      const at = stamps[row.projectId];
      if (at === undefined) {
        missing = true;
        continue;
      }
      if (oldest === null || at < oldest) oldest = at;
    }
    // Nothing docked: fall back to the newest stamp we hold, so an all-clear
    // line can still say how recently it was true.
    if (rows.length === 0) {
      for (const at of Object.values(stamps)) {
        if (oldest === null || at > oldest) oldest = at;
      }
    }
    const stale = oldest === null ? true : missing || now - oldest > STALE_AFTER_MS;
    return { oldest, stale };
  }, [rows, stamps, now]);
}

function ageLabel(at: number | null, now: number): string {
  if (at === null) return "never";
  const mins = Math.floor((now - at) / 60_000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

function Chip({ chip, stacked }: { chip: SyncChip; stacked: boolean }) {
  const Icon = useProjectIcon(chip.iconId ?? "");
  return (
    <span
      title={chip.slug}
      className={cn(
        "flex size-3.5 shrink-0 items-center justify-center rounded",
        // Overlapped like a stack of faces; the ring keeps them separable
        // against the sidebar ground.
        stacked && "-mr-1 ring-2 ring-sidebar",
      )}
      style={{ backgroundColor: `${chip.colorBg}26`, color: chip.colorFg }}
    >
      {Icon ? (
        <Icon className="size-2.5" />
      ) : (
        <span className="text-[7px] font-bold">{chip.initials}</span>
      )}
    </span>
  );
}

export function SyncDock() {
  const ws = useWebSocket();
  const navigate = useNavigate();
  const now = useNow().getTime();
  const rows = useSyncRows();
  const expanded = useUIStore((s) => s.syncDockExpanded);
  const setExpanded = useUIStore((s) => s.setSyncDockExpanded);
  const [busy, setBusy] = useState<Set<string>>(new Set());
  const [refreshing, setRefreshing] = useState(false);

  const summary = useMemo(() => summarize(rows), [rows]);
  const { oldest, stale } = useFetchAge(rows, now);
  const projectsLoaded = useAppStore((s) => s.projectsLoaded);

  const markBusy = useCallback((projectId: string, on: boolean) => {
    setBusy((prev) => {
      const next = new Set(prev);
      if (on) next.add(projectId);
      else next.delete(projectId);
      return next;
    });
  }, []);

  /** Push / fast-forward pull. Never called for a diverged checkout. */
  const runAction = useCallback(
    async (row: SyncRowVM) => {
      markBusy(row.projectId, true);
      try {
        const status =
          row.action === "push"
            ? await pushProject(ws, row.projectId)
            : await pullProject(ws, row.projectId);
        useAppStore.getState().setProjectGitStatus(status);
        useAppStore.getState().markProjectFetched(row.projectId, Date.now());
      } catch (err) {
        toast.error(getErrorMessage(err, `${row.action === "push" ? "Push" : "Pull"} failed`));
      } finally {
        markBusy(row.projectId, false);
      }
    },
    [ws, markBusy],
  );

  /** Diverged: the dock never runs anything that can conflict. */
  const openRebase = useCallback(
    (row: SyncRowVM) => {
      useAppStore.getState().setSidebarOpen(false);
      navigate({
        to: "/project/$projectSlug/session/new",
        params: { projectSlug: row.slug },
        search: { prompt: buildRebasePrompt(row), worktree: false },
      });
    },
    [navigate],
  );

  // Bulk: mechanical rows only, settled per checkout so one unreachable
  // machine can't sink the sweep.
  const syncAll = useCallback(async () => {
    const targets = mechanicalRows(rows);
    if (targets.length === 0) return;
    for (const row of targets) markBusy(row.projectId, true);
    const results = await Promise.allSettled(
      targets.map((row) =>
        row.action === "push" ? pushProject(ws, row.projectId) : pullProject(ws, row.projectId),
      ),
    );
    const store = useAppStore.getState();
    let failed = 0;
    results.forEach((result, i) => {
      const row = targets[i];
      if (!row) return;
      markBusy(row.projectId, false);
      if (result.status === "fulfilled") {
        store.setProjectGitStatus(result.value);
        store.markProjectFetched(row.projectId, Date.now());
      } else {
        failed++;
      }
    });
    if (failed > 0) {
      toast.error(`${failed} of ${targets.length} could not be synced`);
    }
  }, [ws, rows, markBusy]);

  const refresh = useCallback(async () => {
    setRefreshing(true);
    try {
      await fetchSweep(ws, useAppStore.getState().projects);
    } finally {
      setRefreshing(false);
    }
  }, [ws]);

  const toggle = useCallback(() => {
    const next = !expanded;
    setExpanded(next);
    // Expanding is a request for the truth, so pay for a fetch right then.
    if (next) void refresh();
  }, [expanded, setExpanded, refresh]);

  // Before the first status sweep lands there is nothing honest to say.
  if (!projectsLoaded) return null;

  const clear = rows.length === 0;

  return (
    <div className="shrink-0 border-t border-sidebar-border px-2 py-1.5">
      <button
        type="button"
        onClick={clear ? refresh : toggle}
        aria-expanded={clear ? undefined : expanded}
        className={cn(
          "flex w-full cursor-pointer items-center gap-2 rounded-md px-1.5 py-1 text-left",
          "transition-colors hover:bg-sidebar-accent/60 max-md:min-h-9",
        )}
      >
        <span
          className={cn(
            "size-[7px] shrink-0 rounded-full",
            clear ? "bg-success/70" : stale ? "bg-muted-foreground-faint" : "bg-orange",
          )}
        />

        {clear ? (
          <span className="truncate text-[11.5px] text-muted-foreground">
            {stale ? "Sync unknown" : "All repos in sync"}
          </span>
        ) : stale ? (
          <span className="truncate text-[11.5px] text-muted-foreground">Sync unknown</span>
        ) : (
          <>
            <span className="flex shrink-0 items-center pr-1">
              {summary.chips.slice(0, MAX_CHIPS).map((chip) => (
                <Chip key={chip.repoKey} chip={chip} stacked />
              ))}
            </span>
            {summary.chips.length > MAX_CHIPS && (
              <span className="shrink-0 font-mono text-[10px] text-muted-foreground-faint">
                +{summary.chips.length - MAX_CHIPS}
              </span>
            )}
            <span className="truncate text-[11.5px] text-muted-foreground">
              <span className="font-semibold text-foreground-bright">{summary.total}</span> to sync
            </span>
          </>
        )}

        <span className="ml-auto flex shrink-0 items-center gap-1.5">
          <span className="font-mono text-[9.5px] text-muted-foreground-faint">
            {ageLabel(oldest, now)}
          </span>
          {!clear && (
            <ChevronRight
              className={cn(
                "size-3 text-muted-foreground-faint transition-transform",
                expanded && "rotate-90",
              )}
            />
          )}
        </span>
      </button>

      {!clear && expanded && (
        <div className="mt-0.5 flex flex-col gap-px">
          {rows.map((row) => (
            <SyncRow
              key={row.projectId}
              row={row}
              busy={busy.has(row.projectId)}
              onAct={() => (row.action === "rebase" ? openRebase(row) : runAction(row))}
            />
          ))}
          <div className="flex items-center gap-2 px-1.5 pt-1">
            <button
              type="button"
              onClick={refresh}
              className="flex cursor-pointer items-center gap-1 font-mono text-[9.5px] text-muted-foreground-faint transition-colors hover:text-foreground"
            >
              <RefreshCw className={cn("size-2.5", refreshing && "animate-spin")} />
              {refreshing ? "fetching…" : "refresh"}
            </button>
            {summary.mechanical > 0 && (
              <button
                type="button"
                onClick={syncAll}
                className="ml-auto cursor-pointer rounded-full border border-success/30 bg-success/10 px-2 py-0.5 text-[10px] font-semibold text-success transition-colors hover:bg-success/20"
              >
                Sync {summary.mechanical}
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

const ACTION_CLASS: Record<SyncRowVM["action"], string> = {
  push: "border-success/30 bg-success/10 text-success hover:bg-success/20",
  pull: "border-primary/30 bg-primary/10 text-primary hover:bg-primary/20",
  rebase: "border-warning/40 bg-warning/10 text-warning hover:bg-warning/20",
};

function actionLabel(row: SyncRowVM): string {
  if (row.action === "push") return `↑${row.ahead}`;
  if (row.action === "pull") return `↓${row.behind}`;
  return `↑${row.ahead}↓${row.behind}`;
}

function actionTitle(row: SyncRowVM): string {
  if (row.action === "push") return `Push ${row.ahead} commit${row.ahead === 1 ? "" : "s"}`;
  if (row.action === "pull")
    return `Pull ${row.behind} commit${row.behind === 1 ? "" : "s"} (fast-forward)`;
  return `Diverged — opens a session to rebase${row.uncommitted > 0 ? ` (${row.uncommitted} uncommitted)` : ""}`;
}

function SyncRow({ row, busy, onAct }: { row: SyncRowVM; busy: boolean; onAct: () => void }) {
  const Icon = useProjectIcon(row.iconId ?? "");
  return (
    <div className="flex items-center gap-1.5 rounded-md px-1.5 py-1 hover:bg-sidebar-accent/40">
      <span
        className="flex size-3.5 shrink-0 items-center justify-center rounded"
        style={{ backgroundColor: `${row.colorBg}26`, color: row.colorFg }}
      >
        {Icon ? (
          <Icon className="size-2.5" />
        ) : (
          <span className="text-[7px] font-bold">{row.initials}</span>
        )}
      </span>
      <span className="min-w-0 flex-1 truncate font-mono text-[10.5px] text-foreground">
        {row.slug}
      </span>
      {row.machineLabel && (
        <span className="shrink-0 font-mono text-[9.5px] text-muted-foreground-faint">
          @{row.machineLabel}
        </span>
      )}
      <button
        type="button"
        onClick={onAct}
        disabled={busy}
        title={actionTitle(row)}
        className={cn(
          "shrink-0 cursor-pointer rounded-full border px-1.5 py-0.5 font-mono text-[10px] font-medium",
          "transition-colors disabled:opacity-50",
          ACTION_CLASS[row.action],
        )}
      >
        {busy ? "…" : actionLabel(row)}
      </button>
    </div>
  );
}
