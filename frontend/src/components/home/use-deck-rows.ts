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
import { projectLabel } from "~/lib/project-label";
import { type NeedsYouKind, needsYou } from "~/lib/session/needs-you";
import { deriveRestToken, type RestToken } from "~/lib/session/rest-state";
import type { Project } from "~/lib/types";
import { sessionShortId } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import { selectAssistantOpenProposals, useAssistantStore } from "~/stores/assistant-store";
import { type SessionData, useChatStore } from "~/stores/chat-store";
import { usePulseStore } from "~/stores/pulse-store";

/**
 * Why a card is on the deck.
 *
 * Ordered: the two that hold a process, then the assistant's proposal, then the
 * one that only holds the operator's curiosity. `needsYou` keeps its three
 * SESSION kinds — the same rule the voice call's world snapshot reads — and
 * `proposal` is added here and only here, because a proposal is not a state a
 * session is in: it is a row in the assistant's own table, it can be about a
 * channel rather than a session, and nothing outside this deck asks a session
 * whether one exists.
 *
 * It ranks above `unread` because a proposal is work the operator has to
 * authorise before anything happens, and below `approval` and `question`
 * because those hold a live process: a CLI is sitting idle waiting on an
 * answer, where a proposal is a row that will still be there in an hour.
 */
export type DeckKind = NeedsYouKind | "proposal";

export const KIND_RANK: Record<DeckKind, number> = {
  approval: 0,
  question: 1,
  proposal: 2,
  unread: 3,
};

/**
 * The band's order: why it is on the deck, then how recent it is, then the id
 * so two rows never swap places between renders.
 *
 * Exported because it is the ranking itself rather than a detail of the hook —
 * the one place a kind's precedence is decided.
 */
export function compareDeckRows(a: DeckRow, b: DeckRow): number {
  return (
    KIND_RANK[a.kind] - KIND_RANK[b.kind] ||
    b.lastActivity - a.lastActivity ||
    deckRowKey(a).localeCompare(deckRowKey(b))
  );
}

/**
 * What identifies a row, for a React key and for a stable tiebreak.
 *
 * A session can be on the deck once for its own state and once per proposal
 * about it, so the session id alone is not unique here.
 */
export function deckRowKey(row: DeckRow): string {
  return row.proposalId ? `proposal:${row.proposalId}` : `session:${row.sessionId}`;
}

export interface DeckRow {
  sessionId: string;
  name: string;
  /** Routing slug — machine-qualified, as the route param wants it. */
  projectSlug: string;
  /** The project as a reader names it — its name, not its slug. */
  projectLabel: string;
  /** Theme-appropriate project accent. Identity colour, same as the rail's. */
  projectColorFg: string;
  kind: DeckKind;
  /** The tool call, the question, or "" — whatever the card can be specific about. */
  summary: string;
  /** Set on `approval` rows: what Allow/Deny resolves. */
  approvalId?: string;
  /** Set on `proposal` rows: what Accept/Decline decides. */
  proposalId?: string;
  /** Set on `proposal` rows: the verb, for the words the card shows. */
  verb?: string;
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

function lastActivity(meta: SessionData["meta"]): number {
  const ts = meta.lastQueryAt || meta.updatedAt || meta.createdAt;
  const ms = ts ? Date.parse(ts) : 0;
  return Number.isNaN(ms) ? 0 : ms;
}

export function useDeckRows(): DeckRows {
  const sessions = useChatStore((s) => s.sessions);
  const projects = useAppStore((s) => s.projects);
  const pulses = usePulseStore((s) => s.pulses);
  // The open proposals come from the assistant's own store, seeded and kept
  // current from the app shell — so the deck lists them on arrival rather than
  // only after somebody has opened the thread. A stored array, not a filter in
  // the selector.
  const openProposals = useAssistantStore(selectAssistantOpenProposals);
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
        projectLabel: projectLabel(rep.name, displaySlug(rep.slug)),
        projectColorFg: getProjectColor(rep.color, rep.id, projectIds, resolvedTheme).fg,
        lastActivity: lastActivity(meta),
      };

      const kind = needsYou(data);
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
            evicted: !!meta.evictedAt,
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

    // One row per open proposal, whatever else its session is doing: the
    // session's own state and "this is waiting for your yes" are two different
    // claims, and collapsing them would hide the decision behind an approval.
    for (const proposal of openProposals) {
      if (!proposal.id) continue;
      const project = proposal.projectId ? projectById.get(proposal.projectId) : undefined;
      const rep = project ? (repById.get(project.id) ?? project) : undefined;
      const held = proposal.sessionId ? sessions[proposal.sessionId] : undefined;
      const channel = typeof proposal.args?.channel === "string" ? proposal.args.channel : "";
      const created = proposal.createdAt ? Date.parse(proposal.createdAt) : 0;
      needs.push({
        sessionId: proposal.sessionId ?? "",
        // The live name first, then whatever the row recorded, then the channel
        // a dissolve is about — a proposal can target something that is not a
        // session at all.
        name:
          held?.meta.name ||
          proposal.sessionName ||
          channel ||
          (proposal.sessionId ? sessionShortId(proposal.sessionId) : ""),
        projectSlug: project?.slug ?? "",
        projectLabel: rep
          ? projectLabel(rep.name, displaySlug(rep.slug))
          : (proposal.projectName ?? ""),
        // No project row means no hue to be right about: the label inherits
        // rather than being painted some other repo's colour.
        projectColorFg: rep ? getProjectColor(rep.color, rep.id, projectIds, resolvedTheme).fg : "",
        kind: "proposal",
        // The assistant's own reason. It is model-written text, so the card
        // quotes and attributes it rather than printing it as a fact.
        summary: proposal.rationale ?? "",
        proposalId: proposal.id,
        verb: proposal.verb,
        restToken: "",
        lastActivity: Number.isNaN(created) ? 0 : created,
      });
    }

    needs.sort(compareDeckRows);
    live.sort((a, b) => b.lastActivity - a.lastActivity);
    return { needs, live };
  }, [sessions, projects, pulses, openProposals, resolvedTheme]);
}
