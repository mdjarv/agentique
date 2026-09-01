/**
 * A lead session's crew: the workers it spawned, and whether each one is still
 * out.
 *
 * This is the delegation counterpart to `agent-runs`, and the difference
 * between them is the whole reason it is a separate module. A subagent
 * *vanishes* when it returns, so its strip reports presence and empties itself.
 * A worker reports and keeps going — it has its own session, its own worktree
 * and a life longer than the lead's turn — so the crew never empties, and a
 * chip has to carry state rather than presence. The interesting number is not
 * how many are running, it is how many have not come back.
 *
 * Membership comes from `parentSessionId` and nothing else. `channelRoles` says
 * a session is in a channel, which is also true of a discussion persona and of
 * anyone who joined by hand; parentage is the fact that actually means "this
 * lead spawned this worker".
 */
import type { SessionData } from "~/stores/chat-store";

/**
 * Where a worker stands, closed so a new state must choose its mark rather than
 * inherit a blank — the same rule `RestToken` follows.
 *
 * `resting` and `working` are both *out*: a parked worker's work is unfinished
 * and the next message resumes it, which is exactly the state a lead needs to
 * see. Only `back` means the lead can stop waiting.
 */
export type CrewToken = "waiting" | "failed" | "working" | "resting" | "back";

/** Rank for display, lowest first — `lib/session/priority.ts`'s order. */
const TOKEN_RANK: Record<CrewToken, number> = {
  waiting: 0,
  failed: 1,
  working: 2,
  resting: 3,
  back: 4,
};

export interface CrewMember {
  sessionId: string;
  /** Empty for a worker that has not been named yet; the strip supplies a fallback. */
  name: string;
  /** The worker's own project — a spawn can land in a different checkout. */
  projectId: string;
  token: CrewToken;
  /**
   * When this worker last did anything, or undefined when it has never
   * reported a timestamp. An absolute instant rather than an elapsed figure:
   * the clock lives with whatever is ticking, so a derivation that runs on
   * every store change does not also have to run on every second.
   */
  lastActivityAt?: number;
}

export interface Crew {
  members: CrewMember[];
  /** Workers whose token is not `back`. The strip's headline number. */
  out: number;
}

const EMPTY_CREW: Crew = { members: [], out: 0 };

function lastActivityMs(meta: SessionData["meta"]): number | undefined {
  const ts = meta.lastQueryAt || meta.updatedAt || meta.createdAt;
  if (!ts) return undefined;
  const ms = Date.parse(ts);
  return Number.isNaN(ms) ? undefined : ms;
}

/**
 * One worker's standing.
 *
 * A blocked worker outranks a busy one, because only the first needs a person.
 * `merging` counts as working: git is running and the worker is not back yet.
 * Losing the CLI is never "back" — an evicted or disconnected worker has
 * unfinished work and one message resumes it, so it rests rather than returns.
 */
function tokenFor(data: SessionData): CrewToken {
  if (data.pendingApproval || data.pendingQuestion) return "waiting";
  const state = data.meta.state;
  if (state === "failed") return "failed";
  if (state === "running" || state === "merging") return "working";
  if (state === "done" || data.meta.worktreeMerged) return "back";
  return "resting";
}

/**
 * The workers a lead spawned, ranked by how much they need the operator.
 *
 * Ranked rather than kept in spawn order on the app's standing rule that
 * attention decides ordering everywhere. It costs some positional stability in
 * exchange for the guarantee that a worker holding up the run is the leftmost
 * chip.
 */
export function deriveCrew(sessions: Record<string, SessionData>, leadSessionId: string): Crew {
  const members: CrewMember[] = [];
  for (const data of Object.values(sessions)) {
    if (data.meta.parentSessionId !== leadSessionId) continue;
    // A worker filed away is no longer part of the run being watched. The row
    // stays in the sidebar's Archived section; the strip is about live work.
    if (data.meta.archivedAt) continue;
    members.push({
      sessionId: data.meta.id,
      name: data.meta.name || "",
      projectId: data.meta.projectId,
      token: tokenFor(data),
      lastActivityAt: lastActivityMs(data.meta),
    });
  }
  if (members.length === 0) return EMPTY_CREW;

  members.sort(
    (a, b) =>
      TOKEN_RANK[a.token] - TOKEN_RANK[b.token] ||
      a.name.localeCompare(b.name) ||
      a.sessionId.localeCompare(b.sessionId),
  );
  return { members, out: members.filter((m) => m.token !== "back").length };
}

/** What the strip's label says. Counts what is missing, not what is present. */
export function crewLabel(crew: Crew): string {
  if (crew.out === 0) return "all back";
  return `${crew.out} out`;
}
