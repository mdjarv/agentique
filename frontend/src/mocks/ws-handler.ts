import { ws } from "msw";
import {
  CreatePRResultSchema,
  CreateSessionResultSchema,
  GitSnapshotSchema,
  ListSessionsResultSchema,
  MergeResultSchema,
  WireEventSchema,
} from "~/lib/generated-schemas";
import {
  MOCK_PENDING_APPROVALS,
  MOCK_PENDING_QUESTIONS,
  MOCK_PROJECT_GIT_STATUS,
  MOCK_SESSIONS,
  MOCK_TURNS,
  PROJECT_IDS,
  SESSION_IDS,
} from "./data";
import { validatePayload } from "./validate";

const wsLink = ws.link(/wss?:\/\/.*\/ws$/);

interface ClientMessage {
  id: string;
  type: string;
  payload: Record<string, unknown>;
}

type WsClientConnection = Parameters<
  Parameters<ReturnType<typeof ws.link>["addEventListener"]>[1]
>[0]["client"];

function respond(client: WsClientConnection, id: string, payload: unknown = {}) {
  client.send(JSON.stringify({ id, type: "response", payload }));
}

function respondError(client: WsClientConnection, id: string, message: string) {
  client.send(JSON.stringify({ id, type: "response", error: { message } }));
}

function push(client: WsClientConnection, type: string, payload: unknown) {
  // Validate push payloads against generated schemas where applicable.
  if (type === "session.state") {
    validatePayload(GitSnapshotSchema, payload, "push session.state");
  } else if (type === "session.event" && typeof payload === "object" && payload !== null) {
    const p = payload as Record<string, unknown>;
    if (p.event) {
      validatePayload(WireEventSchema, p.event, "push session.event");
    }
  }
  client.send(JSON.stringify({ type, payload }));
}

/** Schedule push events that simulate async server behavior after project subscription. */
function schedulePushEvents(client: WsClientConnection, projectId: string) {
  const sessions = MOCK_SESSIONS[projectId] ?? [];

  for (const session of sessions) {
    const approval = MOCK_PENDING_APPROVALS[session.id];
    if (approval) {
      setTimeout(() => {
        push(client, "session.tool-permission", {
          sessionId: session.id,
          ...approval,
        });
      }, 400);
    }

    const question = MOCK_PENDING_QUESTIONS[session.id];
    if (question) {
      setTimeout(() => {
        push(client, "session.user-question", {
          sessionId: session.id,
          ...question,
        });
      }, 500);
    }

    // Simulate mid-compaction state for the query optimizer session
    if (session.id === SESSION_IDS.queryOptimizer) {
      setTimeout(() => {
        push(client, "session.event", {
          sessionId: session.id,
          event: { type: "compact_status", status: "compacting" },
        });
      }, 600);
    }
  }
}

let sessionCounter = 0;

/** Dispatch a WS request to the appropriate mock handler. */
function dispatch(client: WsClientConnection, msg: ClientMessage) {
  const p = msg.payload;

  switch (msg.type) {
    case "project.subscribe":
      respond(client, msg.id);
      schedulePushEvents(client, p.projectId as string);
      break;

    case "session.list": {
      const sessions = MOCK_SESSIONS[p.projectId as string] ?? [];
      const payload = { sessions };
      validatePayload(ListSessionsResultSchema, payload, "session.list response");
      respond(client, msg.id, payload);
      break;
    }

    case "session.history": {
      const turns = MOCK_TURNS[p.sessionId as string] ?? [];
      respond(client, msg.id, { turns });
      break;
    }

    case "project.git-status": {
      const status = MOCK_PROJECT_GIT_STATUS[p.projectId as string] ?? {
        projectId: p.projectId,
        branch: "main",
        hasRemote: true,
        aheadRemote: 0,
        behindRemote: 0,
        uncommittedCount: 0,
      };
      respond(client, msg.id, status);
      break;
    }

    case "session.create": {
      const id = `mock-created-${++sessionCounter}`;
      const createResult = {
        sessionId: id,
        name: (p.name as string) || `Session ${sessionCounter}`,
        state: "idle",
        connected: true,
        model: (p.model as string) ?? "sonnet",
        permissionMode: p.planMode ? "plan" : "default",
        autoApprove: (p.autoApprove as boolean) ?? false,
        effort: p.effort as string,
        maxBudget: p.maxBudget as number,
        maxTurns: p.maxTurns as number,
        createdAt: new Date().toISOString(),
      };
      validatePayload(CreateSessionResultSchema, createResult, "session.create response");
      respond(client, msg.id, createResult);
      break;
    }

    case "session.query":
      // Ack immediately. In future: schedule streaming push events here.
      respond(client, msg.id);
      // Send a state change to "running", then simulate a simple response
      push(client, "session.state", {
        sessionId: p.sessionId,
        state: "running",
        connected: true,
        version: Date.now(),
      });
      setTimeout(() => {
        push(client, "session.event", {
          sessionId: p.sessionId,
          event: {
            type: "text",
            content:
              "[Mock mode] This is a simulated response. The MSW mock backend does not run real Claude sessions.",
          },
        });
      }, 300);
      setTimeout(() => {
        push(client, "session.event", {
          sessionId: p.sessionId,
          event: {
            type: "result",
            cost: 0,
            duration: 500,
            usage: { inputTokens: 100, outputTokens: 50 },
            stopReason: "end_turn",
          },
        });
        push(client, "session.state", {
          sessionId: p.sessionId,
          state: "idle",
          connected: true,
          hasDirtyWorktree: false,
          hasUncommitted: false,
          worktreeMerged: false,
          commitsAhead: 0,
          commitsBehind: 0,
          branchMissing: false,
          version: Date.now(),
        });
      }, 600);
      break;

    case "session.rename":
      respond(client, msg.id);
      push(client, "session.renamed", {
        sessionId: p.sessionId,
        name: p.name,
      });
      break;

    case "session.delete":
      respond(client, msg.id);
      push(client, "session.deleted", { sessionId: p.sessionId });
      break;

    case "session.delete-bulk": {
      const ids = (p.sessionIds as string[]) ?? [];
      respond(client, msg.id, {
        results: ids.map((id) => ({ sessionId: id, success: true })),
      });
      for (const id of ids) {
        push(client, "session.deleted", { sessionId: id });
      }
      break;
    }

    case "session.diff":
      respond(client, msg.id, {
        hasDiff: true,
        summary: "2 files changed, 15 insertions(+), 3 deletions(-)",
        files: [
          { path: "src/lib/ws-client.ts", insertions: 12, deletions: 3, status: "modified" },
          { path: "src/lib/ws-client.test.ts", insertions: 3, deletions: 0, status: "added" },
        ],
        diff: `diff --git a/src/lib/ws-client.ts b/src/lib/ws-client.ts
index abc1234..def5678 100644
--- a/src/lib/ws-client.ts
+++ b/src/lib/ws-client.ts
@@ -74,6 +74,15 @@ export class WsClient {
+      const jitter = Math.random() * 1000;
+      const delay = ev.code === 1013
+        ? Math.max(5000, this.reconnectDelay)
+        : this.reconnectDelay;`,
        truncated: false,
      });
      break;

    case "session.refresh-git": {
      const sid = p.sessionId as string;
      // Find the session metadata for realistic git state
      const allSessions = Object.values(MOCK_SESSIONS).flat();
      const session = allSessions.find((s) => s.id === sid);
      respond(client, msg.id, {
        sessionId: sid,
        state: session?.state ?? "idle",
        connected: true,
        hasDirtyWorktree: session?.hasDirtyWorktree ?? false,
        hasUncommitted: session?.hasUncommitted ?? false,
        worktreeMerged: session?.worktreeMerged ?? false,
        commitsAhead: session?.commitsAhead ?? 0,
        commitsBehind: session?.commitsBehind ?? 0,
        branchMissing: session?.branchMissing ?? false,
        version: Date.now(),
      });
      break;
    }

    case "session.uncommitted-files":
      respond(client, msg.id, {
        files: [
          { path: "backend/internal/auth/middleware.go", status: "modified" },
          { path: "backend/internal/auth/middleware_test.go", status: "modified" },
        ],
      });
      break;

    case "session.generate-commit-message":
      respond(client, msg.id, {
        title: "refactor: migrate auth middleware to jwt.Verify API",
        description:
          "Replace deprecated ValidateToken with jwt.Verify, add structured error handling for expired/malformed tokens, update integration tests.",
      });
      break;

    case "session.generate-pr-description":
      respond(client, msg.id, {
        title: "Refactor auth middleware to use new JWT validation",
        body: "## Summary\n- Replaced deprecated `ValidateToken` with `jwt.Verify`\n- Added structured error handling (expired, malformed, revoked)\n- Added 3 new integration tests\n\n## Test plan\n- [x] Unit tests pass\n- [x] Integration tests cover new error paths\n- [ ] Manual test with expired token",
      });
      break;

    case "session.commit":
      respond(client, msg.id, { commitHash: "abc1234" });
      break;

    case "session.merge": {
      const mergeResult = { status: "merged", commitHash: "def5678" };
      validatePayload(MergeResultSchema, mergeResult, "session.merge response");
      respond(client, msg.id, mergeResult);
      push(client, "session.state", {
        sessionId: p.sessionId,
        state: "idle",
        connected: true,
        hasDirtyWorktree: false,
        hasUncommitted: false,
        worktreeMerged: true,
        commitsAhead: 0,
        commitsBehind: 0,
        branchMissing: false,
        version: Date.now(),
      });
      break;
    }

    case "session.create-pr": {
      const prResult = {
        status: "created",
        url: "https://github.com/example/repo/pull/99",
      };
      validatePayload(CreatePRResultSchema, prResult, "session.create-pr response");
      respond(client, msg.id, prResult);
      push(client, "session.pr-updated", {
        sessionId: p.sessionId,
        prUrl: "https://github.com/example/repo/pull/99",
      });
      break;
    }

    case "session.rebase":
      respond(client, msg.id, { status: "rebased" });
      break;

    case "session.mark-done":
      respond(client, msg.id);
      push(client, "session.state", {
        sessionId: p.sessionId,
        state: "done",
        connected: false,
        hasDirtyWorktree: false,
        hasUncommitted: false,
        worktreeMerged: false,
        commitsAhead: 0,
        commitsBehind: 0,
        branchMissing: false,
        version: Date.now(),
      });
      break;

    case "session.clean":
      respond(client, msg.id, { status: "cleaned" });
      push(client, "session.deleted", { sessionId: p.sessionId });
      break;

    case "session.stop":
      respond(client, msg.id);
      push(client, "session.state", {
        sessionId: p.sessionId,
        state: "stopped",
        connected: false,
        hasDirtyWorktree: false,
        hasUncommitted: false,
        worktreeMerged: false,
        commitsAhead: 0,
        commitsBehind: 0,
        branchMissing: false,
        version: Date.now(),
      });
      break;

    case "project.tracked-files":
      respond(client, msg.id, {
        files: [
          "README.md",
          "CLAUDE.md",
          "justfile",
          "backend/cmd/agentique/main.go",
          "backend/internal/server/server.go",
          "backend/internal/session/service.go",
          "backend/internal/session/state.go",
          "backend/internal/ws/hub.go",
          "backend/internal/ws/handler.go",
          "backend/internal/store/queries.sql.go",
          "backend/db/queries/sessions.sql",
          "backend/db/queries/projects.sql",
          "backend/db/migrations/001_initial.sql",
          "frontend/src/main.tsx",
          "frontend/src/index.css",
          "frontend/src/components/chat/MessageComposer.tsx",
          "frontend/src/components/chat/AutocompletePopup.tsx",
          "frontend/src/components/chat/TurnBlock.tsx",
          "frontend/src/components/chat/MessageList.tsx",
          "frontend/src/components/layout/ProjectList.tsx",
          "frontend/src/hooks/useWebSocket.ts",
          "frontend/src/hooks/useAutocomplete.ts",
          "frontend/src/stores/chat-store.ts",
          "frontend/src/stores/app-store.ts",
          "frontend/src/lib/ws-client.ts",
          "frontend/src/lib/project-actions.ts",
          "frontend/vite.config.ts",
          "frontend/package.json",
          "docs/websocket-protocol.md",
          "docs/database-schema.md",
        ],
      });
      break;

    case "project.commands":
      respond(client, msg.id, {
        commands: [
          {
            name: "commit",
            source: "project",
            description: "Smart commit with conventional messages",
          },
          {
            name: "review-pr",
            source: "project",
            description: "Review a pull request with detailed feedback",
          },
          { name: "simplify", source: "user", description: "Simplify and refactor selected code" },
          { name: "got", source: "user", description: "Run and analyze tests" },
          {
            name: "tdd",
            source: "user",
            description: "Test-driven development with red-green-refactor",
          },
          { name: "challenge", source: "user" },
          {
            name: "investigate",
            source: "project",
            description:
              "Investigate a YouTrack issue with team deep dives, producing an actionable work document",
          },
          {
            name: "reflect-session",
            source: "user",
            description: "Analyze current session and update CLAUDE.md",
          },
          {
            name: "diff-review",
            source: "user",
            description: "Visual HTML diff review with code analysis",
          },
          {
            name: "fact-check",
            source: "user",
            description: "Verify document accuracy against the codebase",
          },
        ],
      });
      break;

    case "session.set-model":
    case "session.set-permission":
    case "session.set-auto-approve":
    case "session.resolve-approval":
    case "session.resolve-question":
    case "session.interrupt":
    case "project.fetch":
    case "project.push":
    case "project.commit":
      respond(client, msg.id);
      break;

    default:
      respondError(client, msg.id, `[Mock] Unhandled message type: ${msg.type}`);
  }
}

// --- Exported MSW handler ---

export const wsHandler = wsLink.addEventListener("connection", ({ client }) => {
  console.log("[MSW] WebSocket connection intercepted");

  client.addEventListener("message", (event) => {
    try {
      const msg = JSON.parse(event.data as string) as ClientMessage;
      dispatch(client, msg);
    } catch (err) {
      console.error("[MSW] Failed to handle WS message:", err);
    }
  });
});

// Export for use in scenarios/tests that need to send push events to an active connection
export { wsLink, SESSION_IDS, PROJECT_IDS };
