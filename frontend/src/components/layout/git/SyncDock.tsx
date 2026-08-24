/**
 * The sync dock — the rail's last band, under the session list and above the
 * footer.
 *
 * At rest it is one line: a three-tone meter (ahead / behind / diverged) in
 * place of a status dot, the faces of the repos that have drifted, and the
 * commit counts. Opening it keeps that header exactly as it was and only grows
 * a list underneath — one row per drifted *checkout* (so a repo out of sync on
 * two machines is two rows, one face) with its single action.
 *
 * The open dock states its bulk action by position: everything the button will
 * run sits directly above it, the rule beneath it separates what needs a human
 * (diverged) or a machine that is home (away), and pointing at the button
 * previews that split. The label never says a generic "sync N" — it names each
 * half it contains. Running it applies each checkout's result as it lands, so
 * rows leave the dock one by one and the meter shrinks with them.
 *
 * Freshness is part of the design, not a footnote: ahead/behind is only as
 * true as the last `git fetch`, so the dock reports its age and says "not
 * checked yet" rather than presenting a stale count as fact.
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
import { DEFAULT_MACHINE_ICON, getMachineIcon } from "~/lib/machines/icons";
import { pullProject, pushProject } from "~/lib/project-actions";
import { getProjectColor } from "~/lib/project-colors";
import { cn, getErrorMessage } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import { useMachineStore } from "~/stores/machine-store";
import { useUIStore } from "~/stores/ui-store";
import {
  type BulkAction,
  bulkLabel,
  bulkPlan,
  bulkTargets,
  deriveSyncRows,
  exceptionRows,
  mechanicalRows,
  type SyncChip,
  type SyncRowInput,
  type SyncRowVM,
  type SyncSegments,
  summarize,
  syncSegments,
} from "./sync-derive";

/** How many faces the collapsed line shows before it starts counting. */
const MAX_CHIPS = 3;

/** Rebase prompt for a checkout that can't fast-forward. */
function buildRebasePrompt(row: SyncRowVM): string {
  return (
    `Project ${row.label} is behind its remote by ${row.behind} commits and ahead by ${row.ahead}` +
    (row.uncommitted > 0 ? `, with ${row.uncommitted} uncommitted files` : "") +
    `. Pull is non-fast-forward. Please rebase local commits onto upstream, resolve any ` +
    `conflicts, and verify tests pass before pushing.`
  );
}

function useSyncRows(): SyncRowVM[] {
  const projects = useAppStore((s) => s.projects);
  const gitStatus = useAppStore((s) => s.projectGitStatus);
  const machines = useMachineStore((s) => s.machines);
  const machineStatuses = useMachineStore((s) => s.statuses);
  const machineFaults = useMachineStore((s) => s.faults);
  const { resolvedTheme } = useTheme();

  return useMemo(() => {
    const projectIds = projects.map((p) => p.id);
    const inputs: SyncRowInput[] = projects.map((project) => {
      const color = getProjectColor(project.color, project.id, projectIds, resolvedTheme);
      return {
        project,
        status: gitStatus[project.id],
        machineLabel: project.machineId ? machines[project.machineId]?.label : undefined,
        machineIcon: project.machineId ? machines[project.machineId]?.icon : undefined,
        machineOffline: project.machineId
          ? machineStatuses[project.machineId] !== "connected"
          : false,
        machineFault: project.machineId ? machineFaults[project.machineId]?.detail : undefined,
        colorBg: color.bg,
        colorFg: color.fg,
      };
    });
    return deriveSyncRows(inputs);
  }, [projects, gitStatus, machines, machineStatuses, machineFaults, resolvedTheme]);
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
  // No stamp means nothing has been fetched *this session* — the counts came
  // from whatever the checkout last knew, which is unverified, not "never".
  if (at === null) return "unverified";
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
      title={chip.label}
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

/**
 * The drift meter — and the dock's status light, since it replaced the amber
 * dot. Proportional by commits, three-tone (ahead / behind / diverged), and it
 * still says something when there is nothing docked: a dim green track for
 * clear, a grey one when no fetch has happened and the claim would be a guess.
 */
function SyncMeter({
  segments,
  clear,
  stale,
}: {
  segments: SyncSegments;
  clear: boolean;
  stale: boolean;
}) {
  const pct = (n: number) => (segments.total > 0 ? `${(n / segments.total) * 100}%` : "0%");
  return (
    <span
      aria-hidden
      className="flex h-[5px] w-[46px] shrink-0 overflow-hidden rounded-full bg-border/55"
    >
      {clear || segments.total === 0 ? (
        <span
          className={cn("h-full w-full", stale ? "bg-muted-foreground-faint/60" : "bg-success/45")}
        />
      ) : (
        <>
          <span className="h-full bg-success" style={{ width: pct(segments.ahead) }} />
          <span className="h-full bg-primary" style={{ width: pct(segments.behind) }} />
          <span className="h-full bg-warning" style={{ width: pct(segments.diverged) }} />
        </>
      )}
    </span>
  );
}

const BULK_CLASS: Record<BulkAction, string> = {
  push: "bg-success hover:bg-success/85",
  pull: "bg-primary hover:bg-primary/85",
};

/**
 * One direction's bulk action. It reports its own batch while running, and the
 * other button stays disabled — two concurrent sweeps over the same checkouts
 * is a race nobody asked for.
 */
function BulkButton({
  action,
  label,
  run,
  onRun,
  onPreview,
}: {
  action: BulkAction;
  label: string;
  run: { action: BulkAction; total: number; done: number } | null;
  onRun: () => void;
  onPreview: (action: BulkAction | null) => void;
}) {
  const mine = run?.action === action;
  return (
    <button
      type="button"
      onClick={onRun}
      onMouseEnter={() => onPreview(action)}
      onMouseLeave={() => onPreview(null)}
      onFocus={() => onPreview(action)}
      onBlur={() => onPreview(null)}
      disabled={!!run}
      title={`${label} — every other row is left alone`}
      className={cn(
        "flex flex-1 cursor-pointer items-center justify-center whitespace-nowrap rounded-lg px-2 py-1",
        "text-[10.5px] font-semibold text-primary-foreground transition-colors",
        "disabled:cursor-default disabled:opacity-60",
        BULK_CLASS[action],
      )}
    >
      {mine && run ? `${run.total - run.done} to go…` : label}
    </button>
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
  // Hover/focus on a bulk button previews *that* button's reach: the rows it
  // will run light up, everything else fades — including the other button's
  // rows, since push and pull are separate batches.
  const [previewing, setPreviewing] = useState<BulkAction | null>(null);
  // A bulk run in flight. Rows leave the dock as their own status lands, so
  // this only has to say which batch is moving and how much of it is left.
  const [run, setRun] = useState<{ action: BulkAction; total: number; done: number } | null>(null);

  const summary = useMemo(() => summarize(rows), [rows]);
  const mechanical = useMemo(() => mechanicalRows(rows), [rows]);
  const exceptions = useMemo(() => exceptionRows(rows), [rows]);
  const plan = useMemo(() => bulkPlan(rows), [rows]);
  const segments = useMemo(() => syncSegments(rows), [rows]);
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

  // Bulk: one direction at a time, mechanical rows only, each settled and
  // *applied* on its own. The per-checkout store write is the point — a synced
  // checkout leaves the list the moment its status lands and the meter shrinks
  // with it, so the run proves what the button covered instead of restating it.
  const runBulk = useCallback(
    async (action: BulkAction) => {
      const targets = bulkTargets(rows, action);
      if (targets.length === 0) return;
      setRun({ action, total: targets.length, done: 0 });
      for (const row of targets) markBusy(row.projectId, true);
      let failed = 0;
      await Promise.all(
        targets.map(async (row) => {
          try {
            const status =
              row.action === "push"
                ? await pushProject(ws, row.projectId)
                : await pullProject(ws, row.projectId);
            const store = useAppStore.getState();
            store.setProjectGitStatus(status);
            store.markProjectFetched(row.projectId, Date.now());
          } catch {
            failed++;
          } finally {
            markBusy(row.projectId, false);
            setRun((prev) => (prev ? { ...prev, done: prev.done + 1 } : prev));
          }
        }),
      );
      setRun(null);
      if (failed > 0) {
        toast.error(
          `${failed} of ${targets.length} could not be ${action === "push" ? "pushed" : "pulled"}`,
        );
      }
    },
    [ws, rows, markBusy],
  );

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
  // Both buttons on one line: each has half the rail, so the labels shorten.
  const both = plan.pushes > 0 && plan.pulls > 0;

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
        {/* The meter is also the status light — it replaced the amber dot, so
            it has to hold the empty states too. */}
        <SyncMeter segments={segments} clear={clear} stale={stale} />

        {/* Staleness only silences the *claim*, never the findings: drift we
            already know about is real work whether or not it was just
            re-checked, so an unverified dock still names its repos and marks
            the age instead. "Not checked yet" is reserved for the one case
            where the alternative would be a claim we can't back — nothing
            docked and nothing fetched, where "everything pushed" might simply
            be ignorance. */}
        {clear ? (
          <span className="truncate text-[11.5px] text-muted-foreground">
            {stale ? "Not checked yet" : "Everything pushed"}
          </span>
        ) : (
          // Open or shut, the header says the same thing in the same place —
          // toggling only grows the list underneath it.
          <>
            {!expanded && (
              <span className="flex shrink-0 items-center pr-1">
                {summary.chips.slice(0, MAX_CHIPS).map((chip) => (
                  <Chip key={chip.repoKey} chip={chip} stacked />
                ))}
              </span>
            )}
            {!expanded && summary.chips.length > MAX_CHIPS && (
              <span className="shrink-0 font-mono text-[10px] text-muted-foreground-faint">
                +{summary.chips.length - MAX_CHIPS}
              </span>
            )}
            <span className="flex shrink-0 items-center gap-1.5 font-mono text-[10px] tabular-nums">
              {segments.ahead > 0 && <span className="text-success">↑{segments.ahead}</span>}
              {segments.behind > 0 && <span className="text-primary">↓{segments.behind}</span>}
              {summary.diverged > 0 && (
                <span className="text-warning" title="diverged — needs a rebase">
                  ⚠{summary.diverged}
                </span>
              )}
            </span>
          </>
        )}

        <span className="ml-auto flex shrink-0 items-center gap-1.5">
          <span className="font-mono text-[9.5px] text-muted-foreground-faint">
            as of {ageLabel(oldest, now)}
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
        // Capped: a long drift list scrolls inside the dock rather than
        // crushing the session list above it.
        <div className="mt-0.5 flex max-h-56 flex-col gap-px overflow-y-auto">
          {/* Everything the bulk buttons cover, then the buttons, then the
              rule. Position states the scope: what a press touches is directly
              above it, what it skips is below the line — so the old "N need a
              rebase, M away" footnote has nothing left to explain. */}
          {mechanical.map((row) => (
            <SyncRow
              key={row.projectId}
              row={row}
              busy={busy.has(row.projectId)}
              inScope={previewing === row.action}
              outOfScope={!!previewing && previewing !== row.action}
              onAct={() => runAction(row)}
            />
          ))}

          {!plan.empty && (
            // Two batches, never one: sending your own work is the thing you
            // want to do first and on its own, so push and pull each get their
            // own button, in that order, in their own colour.
            <div className="flex gap-1 px-1.5 pt-1">
              {plan.pushes > 0 && (
                <BulkButton
                  action="push"
                  label={bulkLabel(plan, "push", both)}
                  run={run}
                  onRun={() => runBulk("push")}
                  onPreview={setPreviewing}
                />
              )}
              {plan.pulls > 0 && (
                <BulkButton
                  action="pull"
                  label={bulkLabel(plan, "pull", both)}
                  run={run}
                  onRun={() => runBulk("pull")}
                  onPreview={setPreviewing}
                />
              )}
            </div>
          )}

          {exceptions.length > 0 && (
            <>
              <div className="mx-1.5 my-1 h-px bg-border/55" />
              <div className="px-1.5 pb-0.5 font-mono text-[9px] uppercase tracking-wider text-muted-foreground-faint">
                needs you
              </div>
              {exceptions.map((row) => (
                <SyncRow
                  key={row.projectId}
                  row={row}
                  busy={busy.has(row.projectId)}
                  dimmed
                  outOfScope={!!previewing}
                  onAct={() => (row.action === "rebase" ? openRebase(row) : runAction(row))}
                />
              ))}
            </>
          )}

          <div className="flex items-center gap-2 px-1.5 pt-1">
            <button
              type="button"
              onClick={refresh}
              className="flex cursor-pointer items-center gap-1 font-mono text-[9.5px] text-muted-foreground-faint transition-colors hover:text-foreground"
            >
              <RefreshCw className={cn("size-2.5", refreshing && "animate-spin")} />
              {refreshing ? "checking…" : "refresh"}
            </button>
            {run && run.done > 0 && (
              <span className="font-mono text-[9.5px] text-success">{run.done} done</span>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

/** The machine's face beside its name — generic glyph when it has no icon. */
function MachineIcon({ iconId }: { iconId: string }) {
  const Icon = getMachineIcon(iconId) ?? DEFAULT_MACHINE_ICON;
  return <Icon className="size-2.5 shrink-0" />;
}

const ACTION_CLASS: Record<SyncRowVM["action"], string> = {
  push: "border-success/30 bg-success/10 text-success hover:bg-success/20",
  pull: "border-primary/30 bg-primary/10 text-primary hover:bg-primary/20",
  rebase: "border-warning/40 bg-warning/10 text-warning hover:bg-warning/20",
};

function actionLabel(row: SyncRowVM): string {
  if (row.action === "push") return `↑${row.ahead}`;
  if (row.action === "pull") return `↓${row.behind}`;
  // The one row whose button neither pushes nor pulls says so in words — a
  // bare ↑2↓3 reads like the other two and it is not one of them.
  return `rebase ↑${row.ahead}↓${row.behind}`;
}

function actionTitle(row: SyncRowVM): string {
  if (row.action === "push") return `Push ${row.ahead} commit${row.ahead === 1 ? "" : "s"}`;
  if (row.action === "pull")
    return `Pull ${row.behind} commit${row.behind === 1 ? "" : "s"} (fast-forward)`;
  return `Diverged — opens a session to rebase${row.uncommitted > 0 ? ` (${row.uncommitted} uncommitted)` : ""}`;
}

function SyncRow({
  row,
  busy,
  dimmed = false,
  inScope = false,
  outOfScope = false,
  onAct,
}: {
  row: SyncRowVM;
  busy: boolean;
  /** Below the rule: real drift the dock will not run on its own. */
  dimmed?: boolean;
  /** The bulk button is being pointed at and this row is in its reach. */
  inScope?: boolean;
  /** …and this one is not. */
  outOfScope?: boolean;
  onAct: () => void;
}) {
  const Icon = useProjectIcon(row.iconId ?? "");
  return (
    <div
      className={cn(
        "flex items-center gap-1.5 rounded-md px-1.5 py-1 transition-colors hover:bg-sidebar-accent/40",
        inScope && "bg-success/[0.08]",
        outOfScope && "opacity-45",
      )}
    >
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
      <span
        className={cn(
          "min-w-0 flex-1 truncate font-mono text-[10.5px]",
          dimmed ? "text-muted-foreground" : "text-foreground",
        )}
      >
        {row.label}
      </span>
      {row.machineLabel && (
        <span
          title={row.machineFault}
          className={cn(
            "flex shrink-0 items-center gap-0.5 font-mono text-[9.5px]",
            row.machineFault ? "text-destructive" : "text-muted-foreground-faint",
          )}
        >
          <MachineIcon iconId={row.machineIcon ?? ""} />
          {row.machineLabel}
        </span>
      )}
      {row.machineOffline ? (
        // The drift is still true and still worth seeing; it just can't be
        // acted on from here until the machine is back.
        <span
          title={`${row.machineLabel} is offline — ${actionTitle(row)} when it is back`}
          className="shrink-0 rounded-full border border-border/50 px-1.5 py-0.5 font-mono text-[10px] font-medium text-muted-foreground-faint"
        >
          {actionLabel(row)}
        </span>
      ) : (
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
      )}
    </div>
  );
}
