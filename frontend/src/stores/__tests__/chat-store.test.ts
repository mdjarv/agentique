import { beforeEach, describe, expect, it } from "vitest";
import { type ChatEvent, type SessionMetadata, useChatStore } from "~/stores/chat-store";

function makeMeta(overrides: Partial<SessionMetadata> = {}): SessionMetadata {
  return {
    id: "sess-1",
    projectId: "proj-1",
    name: "Test Session",
    state: "idle",
    connected: true,
    model: "sonnet",
    permissionMode: "default",
    autoApprove: false,
    behaviorPresets: { autoCommit: true, suggestParallel: true, planFirst: false, terse: false },
    totalCost: 0,
    turnCount: 0,
    commitsAhead: 0,
    commitsBehind: 0,
    gitVersion: 0,
    createdAt: "2024-01-01T00:00:00Z",
    updatedAt: "2024-01-01T00:00:00Z",
    ...overrides,
  };
}

function makeEvent(overrides: Partial<ChatEvent> & Pick<ChatEvent, "type">): ChatEvent {
  return { id: `evt-${Math.random().toString(36).slice(2)}`, ...overrides };
}

describe("chat-store", () => {
  beforeEach(() => {
    useChatStore.setState({
      sessions: {},
      activeSessionId: null,
      loadedProjects: new Set(),
      historyLoading: new Set(),
      rateLimit: null,
    });
  });

  // --- Session management ---

  describe("addSession / removeSession", () => {
    it("adds a session with empty data", () => {
      const meta = makeMeta();
      useChatStore.getState().addSession(meta);
      const s = useChatStore.getState().sessions["sess-1"];
      expect(s).toBeDefined();
      expect(s?.meta.id).toBe("sess-1");
      expect(s?.turns).toEqual([]);
      expect(s?.pendingApproval).toBeNull();
    });

    it("removes a session and clears active if needed", () => {
      const meta = makeMeta();
      useChatStore.getState().addSession(meta);
      useChatStore.getState().setActiveSessionId("sess-1");
      expect(useChatStore.getState().activeSessionId).toBe("sess-1");

      useChatStore.getState().removeSession("sess-1");
      expect(useChatStore.getState().sessions["sess-1"]).toBeUndefined();
      expect(useChatStore.getState().activeSessionId).toBeNull();
    });
  });

  // --- setSessionState ---

  describe("setSessionState", () => {
    it("updates state", () => {
      useChatStore.getState().addSession(makeMeta());
      useChatStore.getState().setSessionState("sess-1", "running");
      expect(useChatStore.getState().sessions["sess-1"]?.meta.state).toBe("running");
    });

    it("rejects stale version updates", () => {
      useChatStore.getState().addSession(makeMeta({ gitVersion: 5 }));
      // Version 3 < 5 → stale, should be rejected
      useChatStore.getState().setSessionState("sess-1", "running", { gitVersion: 3 });
      expect(useChatStore.getState().sessions["sess-1"]?.meta.state).toBe("idle");
    });

    it("accepts newer version updates", () => {
      useChatStore.getState().addSession(makeMeta({ gitVersion: 5 }));
      useChatStore.getState().setSessionState("sess-1", "done", { gitVersion: 6 });
      expect(useChatStore.getState().sessions["sess-1"]?.meta.state).toBe("done");
    });

    it("preserves git fields during transient states (running)", () => {
      useChatStore.getState().addSession(
        makeMeta({
          commitsAhead: 3,
          hasUncommitted: true,
          mergeStatus: "clean",
        }),
      );
      useChatStore.getState().setSessionState("sess-1", "running");
      const meta = useChatStore.getState().sessions["sess-1"]?.meta;
      expect(meta?.commitsAhead).toBe(3);
      expect(meta?.hasUncommitted).toBe(true);
      expect(meta?.mergeStatus).toBe("clean");
    });

    it("resets git fields for non-transient states", () => {
      useChatStore.getState().addSession(
        makeMeta({
          commitsAhead: 3,
          hasUncommitted: true,
        }),
      );
      useChatStore.getState().setSessionState("sess-1", "idle", {
        commitsAhead: 0,
        hasUncommitted: false,
      });
      const meta = useChatStore.getState().sessions["sess-1"]?.meta;
      expect(meta?.commitsAhead).toBe(0);
      expect(meta?.hasUncommitted).toBe(false);
    });
  });

  // --- submitQuery ---

  describe("submitQuery", () => {
    it("creates a new turn with prompt and empty events", () => {
      useChatStore.getState().addSession(makeMeta());
      useChatStore.getState().submitQuery("sess-1", "Hello world");
      const turns = useChatStore.getState().sessions["sess-1"]?.turns;
      expect(turns).toHaveLength(1);
      expect(turns?.[0]?.prompt).toBe("Hello world");
      expect(turns?.[0]?.events).toEqual([]);
      expect(turns?.[0]?.complete).toBe(false);
    });
  });

  // --- handleServerEvent ---

  describe("handleServerEvent", () => {
    beforeEach(() => {
      useChatStore.getState().addSession(makeMeta());
      useChatStore.getState().submitQuery("sess-1", "test prompt");
    });

    it("appends text event to current turn", () => {
      const event = makeEvent({ type: "text", content: "Hello" });
      useChatStore.getState().handleServerEvent("sess-1", event);
      const turns = useChatStore.getState().sessions["sess-1"]?.turns;
      expect(turns?.[0]?.events).toHaveLength(1);
      expect(turns?.[0]?.events[0]?.content).toBe("Hello");
    });

    it("marks turn complete on result event", () => {
      useChatStore.getState().handleServerEvent(
        "sess-1",
        makeEvent({
          type: "result",
          cost: 0.01,
          duration: 100,
          stopReason: "end_turn",
          contextWindow: 200000,
          inputTokens: 1000,
          outputTokens: 500,
        }),
      );
      const turns = useChatStore.getState().sessions["sess-1"]?.turns;
      expect(turns?.[0]?.complete).toBe(true);
    });

    it("transitions to idle on result event", () => {
      useChatStore
        .getState()
        .handleServerEvent("sess-1", makeEvent({ type: "result", stopReason: "end_turn" }));
      const meta = useChatStore.getState().sessions["sess-1"]?.meta;
      expect(meta?.state).toBe("idle");
    });

    it("updates context usage on result event", () => {
      useChatStore.getState().handleServerEvent(
        "sess-1",
        makeEvent({
          type: "result",
          contextWindow: 200000,
          inputTokens: 5000,
          outputTokens: 1000,
        }),
      );
      const cu = useChatStore.getState().sessions["sess-1"]?.contextUsage;
      expect(cu?.contextWindow).toBe(200000);
      expect(cu?.inputTokens).toBe(5000);
      expect(cu?.outputTokens).toBe(1000);
    });

    it("extracts todos from TodoWrite tool_use", () => {
      useChatStore.getState().handleServerEvent(
        "sess-1",
        makeEvent({
          type: "tool_use",
          toolName: "TodoWrite",
          toolInput: {
            todos: [
              { content: "Fix bug", status: "in_progress", activeForm: "Fixing bug" },
              { content: "Write tests", status: "pending", activeForm: "Writing tests" },
            ],
          },
        }),
      );
      const todos = useChatStore.getState().sessions["sess-1"]?.todos;
      expect(todos).toHaveLength(2);
      expect(todos?.[0]?.content).toBe("Fix bug");
      expect(todos?.[0]?.status).toBe("in_progress");
    });

    it("ignores rate_limit events (no turn append)", () => {
      useChatStore
        .getState()
        .handleServerEvent(
          "sess-1",
          makeEvent({ type: "rate_limit", status: "warning", utilization: 0.8 }),
        );
      const turns = useChatStore.getState().sessions["sess-1"]?.turns;
      expect(turns?.[0]?.events).toHaveLength(0);
    });

    it("sets global rateLimit on warning", () => {
      useChatStore
        .getState()
        .handleServerEvent(
          "sess-1",
          makeEvent({ type: "rate_limit", status: "warning", utilization: 0.8, resetsAt: 123 }),
        );
      const rl = useChatStore.getState().rateLimit;
      expect(rl?.status).toBe("warning");
      expect(rl?.utilization).toBe(0.8);
      expect(rl?.resetsAt).toBe(123);
    });

    it("ignores stream events", () => {
      useChatStore.getState().handleServerEvent("sess-1", makeEvent({ type: "stream" }));
      const turns = useChatStore.getState().sessions["sess-1"]?.turns;
      expect(turns?.[0]?.events).toHaveLength(0);
    });

    it("sets compacting flag on compact_status", () => {
      useChatStore
        .getState()
        .handleServerEvent("sess-1", makeEvent({ type: "compact_status", status: "compacting" }));
      expect(useChatStore.getState().sessions["sess-1"]?.compacting).toBe(true);
    });

    it("preserves context usage on compact_boundary (avoids flash)", () => {
      // First set some context usage via result
      useChatStore.getState().handleServerEvent(
        "sess-1",
        makeEvent({
          type: "result",
          contextWindow: 200000,
          inputTokens: 5000,
          outputTokens: 1000,
        }),
      );
      expect(useChatStore.getState().sessions["sess-1"]?.contextUsage).not.toBeNull();

      // Start a new turn for the compact_boundary event
      useChatStore.getState().submitQuery("sess-1", "second prompt");

      useChatStore
        .getState()
        .handleServerEvent(
          "sess-1",
          makeEvent({ type: "compact_boundary", trigger: "auto", preTokens: 50000 }),
        );
      // contextUsage preserved — streaming data from the next turn replaces it
      expect(useChatStore.getState().sessions["sess-1"]?.contextUsage).not.toBeNull();
      expect(useChatStore.getState().sessions["sess-1"]?.compacting).toBe(false);
    });

    it("sets unseen completion when not active session", () => {
      useChatStore.getState().setActiveSessionId(null);
      useChatStore
        .getState()
        .handleServerEvent("sess-1", makeEvent({ type: "result", stopReason: "end_turn" }));
      expect(useChatStore.getState().sessions["sess-1"]?.hasUnseenCompletion).toBe(true);
    });

    it("does not set unseen completion when active session", () => {
      useChatStore.getState().setActiveSessionId("sess-1");
      useChatStore
        .getState()
        .handleServerEvent("sess-1", makeEvent({ type: "result", stopReason: "end_turn" }));
      expect(useChatStore.getState().sessions["sess-1"]?.hasUnseenCompletion).toBe(false);
    });
  });

  // --- setSessions (project-scoped) ---

  describe("setSessions", () => {
    it("replaces sessions for a project", () => {
      const meta1 = makeMeta({ id: "s1", projectId: "p1" });
      const meta2 = makeMeta({ id: "s2", projectId: "p2" });
      useChatStore.getState().addSession(meta1);
      useChatStore.getState().addSession(meta2);

      // Replace p1 sessions with a new one
      const meta3 = makeMeta({ id: "s3", projectId: "p1" });
      useChatStore.getState().setSessions([meta3], "p1");

      const sessions = useChatStore.getState().sessions;
      expect(sessions.s1).toBeUndefined();
      expect(sessions.s2).toBeDefined();
      expect(sessions.s3).toBeDefined();
    });

    it("preserves existing session data when re-setting", () => {
      const meta = makeMeta();
      useChatStore.getState().addSession(meta);
      useChatStore.getState().submitQuery("sess-1", "hello");

      // Re-set with updated meta
      useChatStore.getState().setSessions([makeMeta({ state: "running" })], "proj-1");
      const s = useChatStore.getState().sessions["sess-1"];
      expect(s?.meta.state).toBe("running");
      expect(s?.turns).toHaveLength(1); // preserved
    });
  });

  // --- History ---

  describe("setSessionHistory", () => {
    it("sets turns and extracts todos", () => {
      useChatStore.getState().addSession(makeMeta());
      useChatStore.getState().setSessionHistory("sess-1", [
        {
          id: "t1",
          prompt: "test",
          attachments: [],
          events: [
            makeEvent({
              type: "tool_use",
              toolName: "TodoWrite",
              toolInput: { todos: [{ content: "Task 1", status: "pending" }] },
            }),
            makeEvent({ type: "result", stopReason: "end_turn" }),
          ],
          complete: true,
        },
      ]);
      const s = useChatStore.getState().sessions["sess-1"];
      expect(s?.turns).toHaveLength(1);
      expect(s?.todos).toHaveLength(1);
      expect(s?.todos?.[0]?.content).toBe("Task 1");
    });
  });
});
