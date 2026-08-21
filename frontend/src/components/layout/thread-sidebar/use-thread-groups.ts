/**
 * Maps the chat/app/pulse/machine stores into the thread-sidebar view-model.
 *
 * All derivation is pure (`derive.ts`); this hook owns only the store reads
 * and the Pinned / Open / Archived partition. Archived means `completedAt`
 * is set (the repo's existing "user moved it aside" primitive); pinned rows
 * are excluded from Open/Archived so a session lives in exactly one section.
 */
import { useMemo } from "react";
import { formatPulse } from "~/components/layout/session/PulseStatus";
import { useTheme } from "~/hooks/useTheme";
import { getProjectColor } from "~/lib/project-colors";
import type { Project } from "~/lib/types";
import { relativeTime } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import { type SessionData, useChatStore } from "~/stores/chat-store";
import { useMachineStore } from "~/stores/machine-store";
import { usePulseStore } from "~/stores/pulse-store";
import { compareOpenRows, deriveBadge, deriveLivePhrase, deriveRestToken, isStale } from "./derive";
import type { ThreadGroups, ThreadRowVM } from "./types";

function projectInitials(slug: string): string {
  return slug
    .split("-")
    .map((w) => w[0])
    .join("")
    .toUpperCase()
    .slice(0, 2);
}

/** Compact summary for the machine line while an approval blocks the agent. */
function approvalSummary(data: SessionData): string | undefined {
  const approval = data.pendingApproval;
  if (!approval) return undefined;
  const input = approval.input as Record<string, unknown> | null;
  const command = input && typeof input.command === "string" ? input.command : "";
  return command || approval.toolName;
}

function sessionTime(meta: SessionData["meta"]): string {
  const ts = meta.completedAt || meta.lastQueryAt || meta.updatedAt;
  return ts ? relativeTime(ts) : "";
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
  const pulses = usePulseStore((s) => s.pulses);
  const { resolvedTheme } = useTheme();

  return useMemo(() => {
    const projectById = new Map<string, Project>(projects.map((p) => [p.id, p]));
    const projectIds = projects.map((p) => p.id);

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

      if (
        query &&
        !(meta.name || "").toLowerCase().includes(query) &&
        !project.slug.toLowerCase().includes(query) &&
        !project.name.toLowerCase().includes(query)
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
      const remoteMachineLabel = project.machineId ? machines[project.machineId]?.label : undefined;
      const color = getProjectColor(project.color, project.id, projectIds, resolvedTheme);
      const isTerminal =
        meta.state === "done" || meta.state === "stopped" || meta.state === "failed";
      const todoTotal = data.todos?.length ?? 0;
      const todoDone = data.todos?.filter((t) => t.status === "completed").length ?? 0;

      const vm: ThreadRowVM = {
        sessionId: meta.id,
        name: meta.name || "",
        untitled: !meta.name,
        projectSlug: project.slug,
        projectInitials: projectInitials(project.slug),
        projectColorBg: color.bg,
        projectColorFg: color.fg,
        projectIconId: project.icon || undefined,
        badge,
        livePhrase:
          deriveLivePhrase({
            badge,
            liveStatus: pulse ? formatPulse(pulse) : undefined,
            approvalSummary: approvalSummary(data),
          }) ?? undefined,
        restToken: deriveRestToken({
          state: meta.state,
          merged: !!meta.worktreeMerged,
          connected: meta.connected,
        }),
        timeLabel: sessionTime(meta),
        struck: isTerminal && !!meta.worktreeMerged,
        unread: data.hasUnseenCompletion,
        todo: todoTotal > 0 ? { done: todoDone, total: todoTotal } : undefined,
        workers: workerCounts.get(meta.id),
        pinned: meta.pinned,
        remoteMachineLabel,
        lastActivity: lastActivity(meta),
      };

      if (meta.pinned) {
        pinOrderById.set(meta.id, meta.pinOrder);
        pinned.push(vm);
      } else if (meta.completedAt) {
        archivedAtById.set(meta.id, Date.parse(meta.completedAt) || 0);
        archived.push(vm);
      } else if (
        // A search flattens the shelf so matches never hide behind it.
        !query &&
        isStale({ state: meta.state, unread: vm.unread, lastActivity: vm.lastActivity, now })
      ) {
        stale.push(vm);
      } else {
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
  }, [sessions, projects, machines, pulses, resolvedTheme, searchQuery]);
}
