import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { useActivityStreamItems } from "~/hooks/useActivityStreamItems";
import type { ChannelInfo } from "~/lib/channel-actions";
import type { Project } from "~/lib/types";
import { useAppStore } from "~/stores/app-store";
import { useChannelStore } from "~/stores/channel-store";
import { type SessionData, useChatStore } from "~/stores/chat-store";
import type { SessionMetadata } from "~/stores/chat-types";

function makeMeta(overrides: Partial<SessionMetadata> = {}): SessionMetadata {
  return {
    id: "sess-1",
    projectId: "proj-1",
    name: "Session One",
    state: "idle",
    connected: true,
    model: "sonnet",
    permissionMode: "default",
    autoApproveMode: "manual",
    behaviorPresets: { autoCommit: true, suggestParallel: true, planFirst: false, terse: false },
    totalCost: 0,
    turnCount: 0,
    commitsAhead: 0,
    commitsBehind: 0,
    gitVersion: 0,
    createdAt: "2026-04-01T00:00:00Z",
    updatedAt: "2026-04-29T12:00:00Z",
    ...overrides,
  };
}

function makeSessionData(overrides: Partial<SessionData> = {}): SessionData {
  const meta = overrides.meta ?? makeMeta();
  return {
    meta,
    turns: [],
    streamingEvents: [],
    historyComplete: false,
    hasUnseenCompletion: false,
    hasUnreadChannelMessage: false,
    pendingApproval: null,
    pendingQuestion: null,
    planMode: false,
    autoApproveMode: "manual",
    todos: null,
    contextUsage: null,
    compacting: false,
    ...overrides,
  };
}

function makeProject(overrides: Partial<Project> = {}): Project {
  return {
    id: "proj-1",
    name: "Test Project",
    slug: "test-project",
    path: "/tmp/p",
    color: "#aabbcc",
    icon: "",
    maxSessions: 0,
    defaultBehaviorPresets: "",
    favorite: false,
    sortOrder: 0,
    createdAt: "2026-01-01",
    ...overrides,
  } as Project;
}

function setStores(args: {
  sessions?: Record<string, SessionData>;
  projects?: Project[];
  channels?: ChannelInfo[];
}) {
  useChatStore.setState({ sessions: args.sessions ?? {} });
  useAppStore.setState({ projects: args.projects ?? [makeProject()] });
  useChannelStore.getState().setChannels(args.channels ?? []);
}

describe("useActivityStreamItems", () => {
  beforeEach(() => {
    useChatStore.setState({
      sessions: {},
      activeSessionId: null,
      loadedProjects: new Set(),
      historyLoading: new Set(),
    });
    useAppStore.setState({ projects: [] });
    useChannelStore.setState({ channels: {}, timelines: {} });
  });

  it("partitions a plain idle session into the active section", () => {
    setStores({
      sessions: { "sess-1": makeSessionData() },
    });
    const { result } = renderHook(() => useActivityStreamItems("", null));
    expect(result.current.attention).toEqual([]);
    expect(result.current.active).toHaveLength(1);
    expect(result.current.active[0]).toMatchObject({ kind: "session", sessionId: "sess-1" });
    expect(result.current.recent).toEqual([]);
  });

  it("places a session with a pending approval into attention with kind=approval", () => {
    setStores({
      sessions: {
        "sess-1": makeSessionData({
          pendingApproval: {
            approvalId: "a1",
            toolName: "Bash",
            input: {},
          },
        }),
      },
    });
    const { result } = renderHook(() => useActivityStreamItems("", null));
    expect(result.current.attention).toHaveLength(1);
    expect(result.current.attention[0]).toMatchObject({ id: "sess-1", kind: "approval" });
    expect(result.current.active).toEqual([]);
  });

  it("uses kind=plan when planMode is on alongside a pending approval", () => {
    setStores({
      sessions: {
        "sess-1": makeSessionData({
          planMode: true,
          pendingApproval: { approvalId: "a1", toolName: "Bash", input: {} },
        }),
      },
    });
    const { result } = renderHook(() => useActivityStreamItems("", null));
    expect(result.current.attention[0]?.kind).toBe("plan");
  });

  it("places sessions with completedAt into recent", () => {
    setStores({
      sessions: {
        "sess-1": makeSessionData({
          meta: makeMeta({ completedAt: "2026-04-29T12:00:00Z", id: "sess-1" }),
        }),
      },
    });
    const { result } = renderHook(() => useActivityStreamItems("", null));
    expect(result.current.recent).toHaveLength(1);
    expect(result.current.active).toEqual([]);
  });

  it("counts hasUnseenCompletion in activeUnread when not yet completed", () => {
    setStores({
      sessions: {
        "sess-1": makeSessionData({ hasUnseenCompletion: true }),
      },
    });
    const { result } = renderHook(() => useActivityStreamItems("", null));
    expect(result.current.activeUnread).toBe(1);
    expect(result.current.recentUnread).toBe(0);
  });

  it("filters by project when filterProjectId is set", () => {
    setStores({
      projects: [
        makeProject({ id: "proj-1", slug: "p1" }),
        makeProject({ id: "proj-2", slug: "p2" }),
      ],
      sessions: {
        "sess-1": makeSessionData({ meta: makeMeta({ id: "sess-1", projectId: "proj-1" }) }),
        "sess-2": makeSessionData({ meta: makeMeta({ id: "sess-2", projectId: "proj-2" }) }),
      },
    });
    const { result } = renderHook(() => useActivityStreamItems("", "proj-1"));
    expect(result.current.active).toHaveLength(1);
    expect((result.current.active[0] as { sessionId: string }).sessionId).toBe("sess-1");
  });

  it("matches search by session name", () => {
    setStores({
      sessions: {
        a: makeSessionData({ meta: makeMeta({ id: "a", name: "Alpha" }) }),
        b: makeSessionData({ meta: makeMeta({ id: "b", name: "Beta" }) }),
      },
    });
    const { result } = renderHook(() => useActivityStreamItems("alpha", null));
    expect(result.current.active).toHaveLength(1);
    expect((result.current.active[0] as { sessionId: string }).sessionId).toBe("a");
  });

  it("excludes channel-member sessions from the session loop", () => {
    // A session that is a member of a channel should not appear directly —
    // the channel item carries it.
    const ch: ChannelInfo = {
      id: "ch-1",
      projectId: "proj-1",
      name: "team",
      createdAt: "2026-04-01",
      members: [{ sessionId: "sess-1", role: "lead", state: "idle", connected: true, name: "S1" }],
    } as ChannelInfo;

    setStores({
      sessions: { "sess-1": makeSessionData() },
      channels: [ch],
    });
    const { result } = renderHook(() => useActivityStreamItems("", null));
    // The session should NOT be present as a session item.
    expect(result.current.active.find((i) => i.kind === "session")).toBeUndefined();
    // The channel itself should be present in active.
    expect(result.current.active.find((i) => i.kind === "channel")).toBeDefined();
  });

  it("sorts active items by lastActivity descending", () => {
    setStores({
      sessions: {
        old: makeSessionData({
          meta: makeMeta({ id: "old", name: "Old", lastQueryAt: "2026-01-01T00:00:00Z" }),
        }),
        "new": makeSessionData({
          meta: makeMeta({ id: "new", name: "New", lastQueryAt: "2026-04-29T00:00:00Z" }),
        }),
      },
    });
    const { result } = renderHook(() => useActivityStreamItems("", null));
    expect(result.current.active.map((i) => (i as { sessionId: string }).sessionId)).toEqual([
      "new",
      "old",
    ]);
  });
});
