/**
 * The world snapshot: every session this client can see, as the call's agent
 * needs to see it.
 *
 * The browser is the only party that holds this picture. Each server knows its
 * own sessions; the client is talking to the primary plus one socket per paired
 * machine, so "what am I working on" is a question only it can answer. That is
 * why the snapshot is pushed to the call rather than queried by it.
 *
 * Pure on purpose — store reads live in the caller, so the mapping (attention,
 * ordering, the cap) is testable without a store or a socket.
 */
import { groupProjects } from "~/lib/machines/grouping";
import { displaySlug } from "~/lib/machines/slug";
import { needsYou } from "~/lib/session/needs-you";
import type { Project } from "~/lib/types";
import { type VoiceWorldSession, WORLD_ROW_CAP } from "~/lib/voice/protocol";
import type { SessionData, SessionMetadata } from "~/stores/chat-types";

export interface WorldInput {
  /** The chat store's sessions, from every machine. */
  sessions: Record<string, SessionData>;
  /** Projects as held client-side; remote ones carry `machineId`. */
  projects: Project[];
  /** machineId → label, for the machines that have one. */
  machineNames: Record<string, string>;
}

/** Epoch ms of the last thing that happened, for ordering. */
function lastActivityMs(meta: SessionMetadata): number {
  const ts = meta.lastQueryAt || meta.updatedAt || meta.createdAt;
  const ms = ts ? Date.parse(ts) : 0;
  return Number.isNaN(ms) ? 0 : ms;
}

/**
 * Builds the snapshot, newest first, capped.
 *
 * Archived sessions are left out: the operator filed them away, and a voice
 * agent offering to pick one up is offering to undo that. A session whose
 * project this client does not hold is left out too — it could be named but not
 * navigated to, which is worse than not mentioning it.
 */
export function buildWorldSessions(input: WorldInput): VoiceWorldSession[] {
  const projectById = new Map<string, Project>(input.projects.map((p) => [p.id, p]));

  // Presentation is the logical representative's (docs/multi-machine.md): the
  // same repo on two machines is one repo, and it must not have two names
  // depending on which machine happened to run the session.
  const repById = new Map<string, Project>();
  for (const { project: rep, members } of groupProjects(input.projects)) {
    for (const member of members) repById.set(member.id, rep);
  }

  const rows: Array<{ row: VoiceWorldSession; at: number }> = [];
  for (const data of Object.values(input.sessions)) {
    const meta = data.meta;
    if (meta.archivedAt) continue;
    // Routing is physical, presentation is logical: the slug that addresses the
    // session is this checkout's, the name it is called by is the group's.
    const project = projectById.get(meta.projectId);
    if (!project) continue;
    const rep = repById.get(project.id) ?? project;

    const machineId = project.machineId;
    const machineName = machineId ? input.machineNames[machineId] : undefined;
    const attention = needsYou(data);

    rows.push({
      at: lastActivityMs(meta),
      row: {
        sessionId: meta.id,
        name: meta.name || "Untitled",
        projectSlug: project.slug,
        projectName: rep.name || displaySlug(rep.slug),
        ...(machineId ? { machineId } : {}),
        ...(machineName ? { machineName } : {}),
        state: meta.state,
        ...(attention ? { attention } : {}),
        ...(meta.worktreeBranch ? { branch: meta.worktreeBranch } : {}),
        ...(meta.lastQueryAt || meta.updatedAt
          ? { lastActivityAt: meta.lastQueryAt || meta.updatedAt }
          : {}),
      },
    });
  }

  rows.sort((a, b) => b.at - a.at || a.row.sessionId.localeCompare(b.row.sessionId));
  return rows.slice(0, WORLD_ROW_CAP).map((r) => r.row);
}
