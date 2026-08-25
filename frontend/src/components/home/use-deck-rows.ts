/**
 * Maps the chat/app/pulse stores into the landing deck's two bands.
 *
 * Same split of labour as the sidebar: this hook owns the store reads and the
 * partition, `CommandDeck` only renders. Presentation (label, hue) comes from
 * the logical project's representative, so one repo reads the same on the deck
 * as it does in the rail.
 */
import { useMemo } from "react";
import { formatPulse } from "~/components/layout/session/PulseStatus";
import { useTheme } from "~/hooks/useTheme";
import { groupProjects } from "~/lib/machines/grouping";
import { displaySlug } from "~/lib/machines/slug";
import { getProjectColor } from "~/lib/project-colors";
import { deriveRestToken, type RestToken } from "~/lib/session/rest-state";
import type { Project } from "~/lib/types";
import { useAppStore } from "~/stores/app-store";
import { type SessionData, useChatStore } from "~/stores/chat-store";
import { usePulseStore } from "~/stores/pulse-store";

/**
 * Why a card is on the deck. Ordered: the two that hold a process come before
 * the one that only holds the operator's curiosity.
 */
export type DeckKind = "approval" | "question" | "unread";

const KIND_RANK: Record<DeckKind, number> = { approval: 0, question: 1, unread: 2 };

export interface DeckRow {
  sessionId: string;
  name: string;
  /** Routing slug — machine-qualified, as the route param wants it. */
  projectSlug: string;
  /** The slug as read: machine suffix dropped. */
  projectLabel: string;
  /** Theme-appropriate project accent. Identity colour, same as the rail's. */
  projectColorFg: string;
  kind: DeckKind;
  /** The tool call, the question, or "" — whatever the card can be specific about. */
  summary: string;
  /** Set on `approval` rows: what Allow/Deny resolves. */
  approvalId?: string;
  /** One-word outcome, for the mark an unread card wears. */
  restToken: RestToken;
  lastActivity: number;
}

export interface LiveRow {
  sessionId: string;
  name: string;
  projectSlug: string;
  projectLabel: string;
  projectColorFg: string;
  /** Live narration, when the pulse has any. */
  pulse?: string;
  todo?: { done: number; total: number };
  lastActivity: number;
}

export interface DeckRows {
  /** Blocked on a human, then finished-but-unread. One band. */
  needs: DeckRow[];
  live: LiveRow[];
}

function approvalSummary(data: SessionData): string {
  const approval = data.pendingApproval;
  if (!approval) return "";
  const input = approval.input as Record<string, unknown> | null;
  const command = input && typeof input.command === "string" ? input.command : "";
  return command ? `${approval.toolName} · ${command}` : approval.toolName;
}

function deckKind(data: SessionData): DeckKind | null {
  if (data.pendingApproval) return "approval";
  if (data.pendingQuestion) return "question";
  // A running session's completion is the *previous* turn's — it is not
  // waiting to be read, it is being superseded.
  if (data.hasUnseenCompletion && data.meta.state !== "running") return "unread";
  return null;
}

function lastActivity(meta: SessionData["meta"]): number {
  const ts = meta.lastQueryAt || meta.updatedAt || meta.createdAt;
  const ms = ts ? Date.parse(ts) : 0;
  return Number.isNaN(ms) ? 0 : ms;
}

export function useDeckRows(): DeckRows {
  const sessions = useChatStore((s) => s.sessions);
  const projects = useAppStore((s) => s.projects);
  const pulses = usePulseStore((s) => s.pulses);
  const { resolvedTheme } = useTheme();

  return useMemo(() => {
    const projectById = new Map<string, Project>(projects.map((p) => [p.id, p]));
    const projectIds = projects.map((p) => p.id);
    const repById = new Map<string, Project>();
    for (const { project: rep, members } of groupProjects(projects)) {
      for (const member of members) repById.set(member.id, rep);
    }

    const needs: DeckRow[] = [];
    const live: LiveRow[] = [];

    for (const data of Object.values(sessions)) {
      const meta = data.meta;
      if (meta.archivedAt) continue;
      const project = projectById.get(meta.projectId);
      if (!project) continue;
      const rep = repById.get(project.id) ?? project;
      const identity = {
        sessionId: meta.id,
        name: meta.name || "",
        projectSlug: project.slug,
        projectLabel: displaySlug(rep.slug),
        projectColorFg: getProjectColor(rep.color, rep.id, projectIds, resolvedTheme).fg,
        lastActivity: lastActivity(meta),
      };

      const kind = deckKind(data);
      if (kind) {
        needs.push({
          ...identity,
          kind,
          summary:
            kind === "approval"
              ? approvalSummary(data)
              : kind === "question"
                ? (data.pendingQuestion?.questions[0]?.question ?? "")
                : "",
          approvalId: data.pendingApproval?.approvalId,
          restToken: deriveRestToken({
            state: meta.state,
            merged: !!meta.worktreeMerged,
            connected: meta.connected,
          }),
        });
        continue;
      }

      if (meta.state === "running") {
        const pulse = pulses[meta.id];
        const total = data.todos?.length ?? 0;
        live.push({
          ...identity,
          pulse: pulse ? formatPulse(pulse) : undefined,
          todo: total
            ? { done: data.todos?.filter((t) => t.status === "completed").length ?? 0, total }
            : undefined,
        });
      }
    }

    needs.sort(
      (a, b) =>
        KIND_RANK[a.kind] - KIND_RANK[b.kind] ||
        b.lastActivity - a.lastActivity ||
        a.sessionId.localeCompare(b.sessionId),
    );
    live.sort((a, b) => b.lastActivity - a.lastActivity);
    return { needs, live };
  }, [sessions, projects, pulses, resolvedTheme]);
}
