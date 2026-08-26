import { describe, expect, it } from "vitest";
import type { Project } from "~/lib/types";
import { WORLD_ROW_CAP } from "~/lib/voice/protocol";
import { buildWorldSessions } from "~/lib/voice/world";
import type { SessionData, SessionMetadata } from "~/stores/chat-types";

// --- Builders -------------------------------------------------------------

function makeMeta(overrides: Partial<SessionMetadata> = {}): SessionMetadata {
  return {
    id: "sess-1",
    projectId: "proj-1",
    name: "Reconnect drops",
    state: "idle",
    connected: true,
    pinned: false,
    pinOrder: 0,
    model: "sonnet",
    permissionMode: "default",
    autoApproveMode: "fullAuto",
    behaviorPresets: { autoCommit: true, suggestParallel: true, planFirst: false, terse: false },
    totalCost: 0,
    turnCount: 0,
    commitsAhead: 0,
    commitsBehind: 0,
    gitVersion: 0,
    createdAt: "2026-08-01T00:00:00Z",
    updatedAt: "2026-08-01T00:00:00Z",
    ...overrides,
  };
}

function makeSession(overrides: Partial<SessionData> = {}): SessionData {
  return {
    meta: makeMeta(),
    turns: [],
    streamingEvents: [],
    historyComplete: true,
    hasUnseenCompletion: false,
    hasUnreadChannelMessage: false,
    pendingApproval: null,
    pendingQuestion: null,
    planMode: false,
    autoApproveMode: "fullAuto",
    todos: null,
    contextUsage: null,
    compacting: false,
    ...overrides,
  };
}

function makeProject(overrides: Partial<Project> = {}): Project {
  return {
    id: "proj-1",
    name: "Agentique",
    path: "/home/dev/agentique",
    slug: "agentique",
    default_model: "",
    default_permission_mode: "",
    default_system_prompt: "",
    default_behavior_presets: "",
    created_at: "",
    updated_at: "",
    sort_order: 0,
    favorite: 0,
    color: "",
    icon: "",
    folder: "",
    max_sessions: 0,
    remote_url: "",
    ...overrides,
  } as Project;
}

function sessions(...list: SessionData[]): Record<string, SessionData> {
  return Object.fromEntries(list.map((s) => [s.meta.id, s]));
}

// --- Tests ----------------------------------------------------------------

describe("buildWorldSessions", () => {
  it("names the session, its project and its branch", () => {
    const rows = buildWorldSessions({
      sessions: sessions(
        makeSession({ meta: makeMeta({ worktreeBranch: "fix/reconnect", state: "running" }) }),
      ),
      projects: [makeProject()],
      machineNames: {},
    });

    expect(rows).toEqual([
      {
        sessionId: "sess-1",
        name: "Reconnect drops",
        projectSlug: "agentique",
        projectName: "Agentique",
        state: "running",
        branch: "fix/reconnect",
        lastActivityAt: "2026-08-01T00:00:00Z",
      },
    ]);
  });

  describe("attention", () => {
    it("puts approval first", () => {
      const [row] = buildWorldSessions({
        sessions: sessions(
          makeSession({
            pendingApproval: { approvalId: "a1", toolName: "Bash", input: null },
            pendingQuestion: { questionId: "q1", questions: [{ question: "which?" }] },
            hasUnseenCompletion: true,
          }),
        ),
        projects: [makeProject()],
        machineNames: {},
      });
      expect(row?.attention).toBe("approval");
    });

    it("falls to question when nothing is waiting on approval", () => {
      const [row] = buildWorldSessions({
        sessions: sessions(
          makeSession({
            pendingQuestion: { questionId: "q1", questions: [{ question: "which?" }] },
            hasUnseenCompletion: true,
          }),
        ),
        projects: [makeProject()],
        machineNames: {},
      });
      expect(row?.attention).toBe("question");
    });

    it("reports an unread completion", () => {
      const [row] = buildWorldSessions({
        sessions: sessions(makeSession({ hasUnseenCompletion: true })),
        projects: [makeProject()],
        machineNames: {},
      });
      expect(row?.attention).toBe("unread");
    });

    // A running session's completion belongs to the turn before it — it is
    // being superseded, not waiting to be read.
    it("stays silent about an unread completion on a running session", () => {
      const [row] = buildWorldSessions({
        sessions: sessions(
          makeSession({
            hasUnseenCompletion: true,
            meta: makeMeta({ state: "running" }),
          }),
        ),
        projects: [makeProject()],
        machineNames: {},
      });
      expect(row?.attention).toBeUndefined();
    });

    it("says nothing when nothing needs anyone", () => {
      const [row] = buildWorldSessions({
        sessions: sessions(makeSession()),
        projects: [makeProject()],
        machineNames: {},
      });
      expect(row?.attention).toBeUndefined();
    });
  });

  // The operator filed these away; an agent offering to pick one up is
  // offering to undo that.
  it("leaves archived sessions out", () => {
    const rows = buildWorldSessions({
      sessions: sessions(
        makeSession({ meta: makeMeta({ id: "open" }) }),
        makeSession({ meta: makeMeta({ id: "filed", archivedAt: "2026-08-02T00:00:00Z" }) }),
      ),
      projects: [makeProject()],
      machineNames: {},
    });
    expect(rows.map((r) => r.sessionId)).toEqual(["open"]);
  });

  // Nameable but not navigable is worse than not mentioned.
  it("leaves out a session whose project this client does not hold", () => {
    const rows = buildWorldSessions({
      sessions: sessions(makeSession({ meta: makeMeta({ projectId: "gone" }) })),
      projects: [makeProject()],
      machineNames: {},
    });
    expect(rows).toEqual([]);
  });

  it("carries the machine a remote session runs on", () => {
    const [row] = buildWorldSessions({
      sessions: sessions(makeSession({ meta: makeMeta({ projectId: "proj-remote" }) })),
      projects: [makeProject({ id: "proj-remote", slug: "agentique~ab12cd34", machineId: "m-1" })],
      machineNames: { "m-1": "zbook" },
    });
    expect(row?.machineId).toBe("m-1");
    expect(row?.machineName).toBe("zbook");
    // Routing stays physical: the qualified slug is the one that addresses it.
    expect(row?.projectSlug).toBe("agentique~ab12cd34");
  });

  // One repo, two checkouts, one name: presentation belongs to the logical
  // representative (docs/multi-machine.md).
  it("names a remote checkout after its logical representative", () => {
    const [row] = buildWorldSessions({
      sessions: sessions(makeSession({ meta: makeMeta({ projectId: "proj-remote" }) })),
      projects: [
        makeProject({ remote_url: "github.com/org/agentique", name: "Agentique" }),
        makeProject({
          id: "proj-remote",
          slug: "agentique~ab12cd34",
          name: "agentique-checkout",
          machineId: "m-1",
          remote_url: "github.com/org/agentique",
        }),
      ],
      machineNames: { "m-1": "zbook" },
    });
    expect(row?.projectName).toBe("Agentique");
  });

  it("orders by recency, newest first", () => {
    const rows = buildWorldSessions({
      sessions: sessions(
        makeSession({ meta: makeMeta({ id: "old", updatedAt: "2026-08-01T00:00:00Z" }) }),
        makeSession({
          meta: makeMeta({ id: "newest", lastQueryAt: "2026-08-09T00:00:00Z" }),
        }),
        makeSession({ meta: makeMeta({ id: "mid", updatedAt: "2026-08-05T00:00:00Z" }) }),
      ),
      projects: [makeProject()],
      machineNames: {},
    });
    expect(rows.map((r) => r.sessionId)).toEqual(["newest", "mid", "old"]);
  });

  // Everything in the snapshot goes to the speech vendor on every change.
  it("caps the snapshot at the newest rows", () => {
    const many = Array.from({ length: WORLD_ROW_CAP + 25 }, (_, i) =>
      makeSession({
        meta: makeMeta({
          id: `sess-${String(i).padStart(4, "0")}`,
          updatedAt: `2026-08-01T00:00:${String(i % 60).padStart(2, "0")}Z`,
        }),
      }),
    );
    const rows = buildWorldSessions({
      sessions: sessions(...many),
      projects: [makeProject()],
      machineNames: {},
    });
    expect(rows).toHaveLength(WORLD_ROW_CAP);
  });
});
