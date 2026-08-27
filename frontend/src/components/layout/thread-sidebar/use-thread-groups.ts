/**
 * Maps the chat/app/pulse/machine stores into the thread-sidebar view-model.
 *
 * All derivation is pure (`derive.ts`); this hook owns only the store reads
 * and the Pinned / Open / Archived partition. Archived means `archivedAt` is
 * set, which is now user intent and nothing else — the runtime stopped writing
 * that field, so a session the CLI merely exited stays visible and falls to the
 * "Finished earlier" shelf on its own schedule.
 *
 * A session lives in exactly one section, and Archived is decided first: "keep
 * this at the top" and "stow this away" are contradictory claims, and the newer
 * gesture wins. The server releases the pin when it archives; this ordering is
 * what keeps the view right in the meantime — the state push announcing an
 * archive carries archivedAt but not pinned, and a peer on an older release
 * never clears the pin at all.
 */
import { useMemo } from "react";
import { formatPulse } from "~/components/layout/session/PulseStatus";
import { useTheme } from "~/hooks/useTheme";
import { groupProjects } from "~/lib/machines/grouping";
import { displaySlug } from "~/lib/machines/slug";
import { sessionModelLabel } from "~/lib/model-catalog";
import { getProjectColor } from "~/lib/project-colors";
import { projectInitials, projectLabel } from "~/lib/project-label";
import { deriveRestToken, isParked } from "~/lib/session/rest-state";
import { workspaceKind } from "~/lib/session/workspace";
import type { Project } from "~/lib/types";
import { relativeTime } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import { type SessionData, useChatStore } from "~/stores/chat-store";
import { useMachineStore } from "~/stores/machine-store";
import { usePulseStore } from "~/stores/pulse-store";
import {
  compareOpenRows,
  deriveBadge,
  deriveLivePhrase,
  deriveWorkKind,
  isAwake,
  isHued,
  isStale,
  isTerminalState,
  sectionFor,
} from "./derive";
import type { ThreadGroups, ThreadRowVM } from "./types";

/** Compact summary for the machine line while an approval blocks the agent. */
function approvalSummary(data: SessionData): string | undefined {
  const approval = data.pendingApproval;
  if (!approval) return undefined;
  const input = approval.input as Record<string, unknown> | null;
  const command = input && typeof input.command === "string" ? input.command : "";
  return command || approval.toolName;
}

/** The question itself while one is pending — the row's most useful string. */
function questionSummary(data: SessionData): string | undefined {
  return data.pendingQuestion?.questions[0]?.question || undefined;
}

function sessionTime(meta: SessionData["meta"]): string {
  const ts = meta.archivedAt || meta.lastQueryAt || meta.updatedAt;
  return ts ? relativeTime(ts) : "";
}

/** Archiving is refused mid-turn, so the row must not offer it. */
function canArchive(meta: SessionData["meta"]): boolean {
  return meta.state !== "running" && meta.state !== "merging";
}

function lastActivity(meta: SessionData["meta"]): number {
  const ts = meta.lastQueryAt || meta.updatedAt || meta.createdAt;
  const ms = ts ? Date.parse(ts) : 0;
  return Number.isNaN(ms) ? 0 : ms;
}

export function useThreadGroups(searchQuery: string): ThreadGroups {
  const sessions = useChatStore((s) => s.sessions);
  const projects = useAppStore((s) => s.projects);
  const machines = useMachineStore((s) => s.machines);
  const machineStatuses = useMachineStore((s) => s.statuses);
  const machineFaults = useMachineStore((s) => s.faults);
  const pulses = usePulseStore((s) => s.pulses);
  const { resolvedTheme } = useTheme();

  return useMemo(() => {
    const projectById = new Map<string, Project>(projects.map((p) => [p.id, p]));
    const projectIds = projects.map((p) => p.id);
    // One repo reads the same everywhere: a session on a remote machine takes
    // its label, colour and icon from the logical project's representative,
    // and says WHICH machine through its glyph — not through a second colour.
    const repById = new Map<string, Project>();
    for (const { project: rep, members } of groupProjects(projects)) {
      for (const member of members) repById.set(member.id, rep);
    }

    // Worker counts for lead sessions, from the parent hierarchy.
    const workerCounts = new Map<string, number>();
    for (const data of Object.values(sessions)) {
      const parent = data.meta.parentSessionId;
      if (parent) workerCounts.set(parent, (workerCounts.get(parent) ?? 0) + 1);
    }

    const query = searchQuery.trim().toLowerCase();
    const now = Date.now();
    const pinned: ThreadRowVM[] = [];
    const open: ThreadRowVM[] = [];
    const stale: ThreadRowVM[] = [];
    const archived: ThreadRowVM[] = [];
    // Pin order and archival time only matter within their own sections,
    // tracked aside so the VM stays free of them.
    const pinOrderById = new Map<string, number>();
    const archivedAtById = new Map<string, number>();

    for (const data of Object.values(sessions)) {
      const meta = data.meta;
      const project = projectById.get(meta.projectId);
      if (!project) continue;
      // Presentation comes from the representative; ownership (which machine
      // runs it) stays with the session's own project.
      const rep = repById.get(project.id) ?? project;

      if (
        query &&
        !(meta.name || "").toLowerCase().includes(query) &&
        !project.slug.toLowerCase().includes(query) &&
        !project.name.toLowerCase().includes(query) &&
        !rep.slug.toLowerCase().includes(query) &&
        !rep.name.toLowerCase().includes(query)
      ) {
        continue;
      }

      const badge = deriveBadge({
        state: meta.state,
        hasPendingApproval: !!data.pendingApproval,
        hasPendingQuestion: !!data.pendingQuestion,
        isPlanning: data.planMode,
        hasUnseenCompletion: data.hasUnseenCompletion,
        connected: meta.connected,
      });
      const pulse = pulses[meta.id];
      const remoteMachine = project.machineId ? machines[project.machineId] : undefined;
      const remoteMachineLabel = remoteMachine?.label;
      const color = getProjectColor(rep.color, rep.id, projectIds, resolvedTheme);
      const repLabel = projectLabel(rep.name, displaySlug(rep.slug));
      const isTerminal = isTerminalState(meta.state);
      const todoTotal = data.todos?.length ?? 0;
      const todoDone = data.todos?.filter((t) => t.status === "completed").length ?? 0;
      const isArchived = !!meta.archivedAt;
      const restToken = deriveRestToken({
        state: meta.state,
        merged: !!meta.worktreeMerged,
        connected: meta.connected,
        machineOffline: project.machineId
          ? machineStatuses[project.machineId] !== "connected"
          : false,
      });

      const vm: ThreadRowVM = {
        sessionId: meta.id,
        name: meta.name || "",
        untitled: !meta.name,
        // Routing needs the session's OWN (machine-qualified) slug; the label
        // beside it is the repo's, so both copies read as one project.
        projectSlug: project.slug,
        projectLabel: repLabel,
        projectInitials: projectInitials(repLabel),
        workspace: workspaceKind(meta.worktreeBranch),
        projectColorBg: color.bg,
        projectColorFg: color.fg,
        projectIconId: rep.icon || undefined,
        badge,
        awake: isAwake(badge) || meta.connected,
        hued: isHued({
          state: meta.state,
          archived: isArchived,
          merged: !!meta.worktreeMerged,
        }),
        livePhrase:
          deriveLivePhrase({
            badge,
            liveStatus: pulse ? formatPulse(pulse) : undefined,
            approvalSummary: approvalSummary(data),
            questionSummary: questionSummary(data),
          }) ?? undefined,
        workKind: deriveWorkKind(pulse?.lastToolCategory),
        restToken,
        parked: isParked(restToken),
        timeLabel: sessionTime(meta),
        struck: isTerminal && !!meta.worktreeMerged,
        unread: data.hasUnseenCompletion,
        todo: todoTotal > 0 ? { done: todoDone, total: todoTotal } : undefined,
        workers: workerCounts.get(meta.id),
        // Archiving releases the pin server-side, but the state push that
        // announces it carries archivedAt and not pinned — and a peer on an
        // older release never clears it at all. Archived wins either way, so a
        // filed-away row offers Pin rather than a stale Unpin.
        pinned: meta.pinned && !isArchived,
        archived: isArchived,
        canArchive: canArchive(meta),
        remoteMachineLabel,
        remoteMachineIcon: remoteMachine?.icon || undefined,
        remoteMachineOffline: project.machineId
          ? machineStatuses[project.machineId] !== "connected"
          : undefined,
        remoteMachineFault: project.machineId
          ? machineFaults[project.machineId]?.detail
          : undefined,
        lastActivity: lastActivity(meta),
        branch: meta.worktreeBranch || undefined,
        model: sessionModelLabel(meta.model, meta.resolvedModel) || undefined,
        turns: meta.turnCount || undefined,
      };

      switch (
        sectionFor({
          archived: isArchived,
          pinned: meta.pinned,
          // A search flattens the shelf so matches never hide behind it.
          stale:
            !query &&
            isStale({ state: meta.state, unread: vm.unread, lastActivity: vm.lastActivity, now }),
        })
      ) {
        case "archived":
          archivedAtById.set(meta.id, Date.parse(meta.archivedAt ?? "") || 0);
          archived.push(vm);
          break;
        case "pinned":
          pinOrderById.set(meta.id, meta.pinOrder);
          pinned.push(vm);
          break;
        case "stale":
          stale.push(vm);
          break;
        default:
          open.push(vm);
      }
    }

    pinned.sort(
      (a, b) =>
        (pinOrderById.get(a.sessionId) ?? 0) - (pinOrderById.get(b.sessionId) ?? 0) ||
        a.sessionId.localeCompare(b.sessionId),
    );
    open.sort(compareOpenRows);
    stale.sort((a, b) => b.lastActivity - a.lastActivity);
    // Most recently *archived* first — an old session swept today surfaces
    // on top, so a sweep (or a mistake) is easy to find and reverse.
    archived.sort(
      (a, b) =>
        (archivedAtById.get(b.sessionId) ?? 0) - (archivedAtById.get(a.sessionId) ?? 0) ||
        b.lastActivity - a.lastActivity,
    );

    return { pinned, open, stale, archived };
  }, [
    sessions,
    projects,
    machines,
    machineStatuses,
    machineFaults,
    pulses,
    resolvedTheme,
    searchQuery,
  ]);
}
