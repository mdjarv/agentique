import { ws } from "msw";
import {
  CleanResultSchema,
  CommandsResultSchema,
  CommitMessageResultSchema,
  CreatePRResultSchema,
  CreateSessionResultSchema,
  DiffResultSchema,
  GitSnapshotSchema,
  HistoryResultSchema,
  ListSessionsResultSchema,
  MergeResultSchema,
  PRDescriptionResultSchema,
  ProjectCommitResultSchema,
  ProjectGitStatusSchema,
  RebaseResultSchema,
  SessionCommitResultSchema,
  TagListResultSchema,
  TagSchema,
  TrackedFilesResultSchema,
  UncommittedFilesResultSchema,
  WireEventSchema,
} from "~/lib/generated-schemas";
import type { Tag } from "~/lib/generated-types";
import {
  MOCK_PENDING_APPROVALS,
  MOCK_PENDING_QUESTIONS,
  MOCK_PROJECTS,
  MOCK_PROJECT_GIT_STATUS,
  MOCK_PROJECT_TAGS,
  MOCK_SESSIONS,
  MOCK_TAGS,
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
let tagCounter = 0;

// Mutable copies for tag/project-tag mutations
const mockTags = [...MOCK_TAGS];
const mockProjectTags = [...MOCK_PROJECT_TAGS];

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
      const payload = { turns };
      respond(
        client,
        msg.id,
        validatePayload(HistoryResultSchema, payload, "session.history response"),
      );
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
      respond(
        client,
        msg.id,
        validatePayload(ProjectGitStatusSchema, status, "project.git-status response"),
      );
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

    case "session.diff": {
      const diffPayload = {
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
      };
      respond(
        client,
        msg.id,
        validatePayload(DiffResultSchema, diffPayload, "session.diff response"),
      );
      break;
    }

    case "session.refresh-git": {
      const sid = p.sessionId as string;
      // Find the session metadata for realistic git state
      const allSessions = Object.values(MOCK_SESSIONS).flat();
      const session = allSessions.find((s) => s.id === sid);
      const gitPayload = {
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
      };
      respond(
        client,
        msg.id,
        validatePayload(GitSnapshotSchema, gitPayload, "session.refresh-git response"),
      );
      break;
    }

    case "session.uncommitted-files": {
      const filesPayload = {
        files: [
          { path: "backend/internal/auth/middleware.go", status: "modified" },
          { path: "backend/internal/auth/middleware_test.go", status: "modified" },
        ],
      };
      respond(
        client,
        msg.id,
        validatePayload(
          UncommittedFilesResultSchema,
          filesPayload,
          "session.uncommitted-files response",
        ),
      );
      break;
    }

    case "session.generate-commit-message": {
      const commitMsgPayload = {
        title: "refactor: migrate auth middleware to jwt.Verify API",
        description:
          "Replace deprecated ValidateToken with jwt.Verify, add structured error handling for expired/malformed tokens, update integration tests.",
      };
      respond(
        client,
        msg.id,
        validatePayload(
          CommitMessageResultSchema,
          commitMsgPayload,
          "session.generate-commit-message response",
        ),
      );
      break;
    }

    case "session.generate-pr-description": {
      const prDescPayload = {
        title: "Refactor auth middleware to use new JWT validation",
        body: "## Summary\n- Replaced deprecated `ValidateToken` with `jwt.Verify`\n- Added structured error handling (expired, malformed, revoked)\n- Added 3 new integration tests\n\n## Test plan\n- [x] Unit tests pass\n- [x] Integration tests cover new error paths\n- [ ] Manual test with expired token",
      };
      respond(
        client,
        msg.id,
        validatePayload(
          PRDescriptionResultSchema,
          prDescPayload,
          "session.generate-pr-description response",
        ),
      );
      break;
    }

    case "session.commit": {
      const commitPayload = { commitHash: "abc1234" };
      respond(
        client,
        msg.id,
        validatePayload(SessionCommitResultSchema, commitPayload, "session.commit response"),
      );
      break;
    }

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

    case "session.rebase": {
      const rebasePayload = { status: "rebased" };
      respond(
        client,
        msg.id,
        validatePayload(RebaseResultSchema, rebasePayload, "session.rebase response"),
      );
      break;
    }

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

    case "session.clean": {
      const cleanPayload = { status: "cleaned" };
      respond(
        client,
        msg.id,
        validatePayload(CleanResultSchema, cleanPayload, "session.clean response"),
      );
      push(client, "session.deleted", { sessionId: p.sessionId });
      break;
    }

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

    case "project.tracked-files": {
      const trackedPayload = {
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
      };
      respond(
        client,
        msg.id,
        validatePayload(TrackedFilesResultSchema, trackedPayload, "project.tracked-files response"),
      );
      break;
    }

    case "project.commands": {
      const commandsPayload = {
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
          {
            name: "challenge",
            source: "user",
            description: "Apply critical self-review to plans and decisions",
          },
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
      };
      respond(
        client,
        msg.id,
        validatePayload(CommandsResultSchema, commandsPayload, "project.commands response"),
      );
      break;
    }

    case "project.commit": {
      const projectCommitPayload = { commitHash: "abc1234" };
      respond(
        client,
        msg.id,
        validatePayload(ProjectCommitResultSchema, projectCommitPayload, "project.commit response"),
      );
      break;
    }

    case "tag.list": {
      const tagListPayload = { tags: mockTags, projectTags: mockProjectTags };
      respond(
        client,
        msg.id,
        validatePayload(TagListResultSchema, tagListPayload, "tag.list response"),
      );
      break;
    }

    case "tag.create": {
      const tagName = p.name as string;
      if (mockTags.some((t) => t.name.toLowerCase() === tagName.toLowerCase())) {
        respondError(client, msg.id, "tag name already exists");
        break;
      }
      const newTag = {
        id: `ttt-mock-${++tagCounter}`,
        name: tagName,
        color: p.color as string,
        sort_order: mockTags.length,
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      };
      mockTags.push(newTag);
      respond(client, msg.id, validatePayload(TagSchema, newTag, "tag.create response"));
      push(client, "tag.created", newTag);
      break;
    }

    case "tag.update": {
      const idx = mockTags.findIndex((t) => t.id === p.id);
      const existing = mockTags[idx];
      if (idx >= 0 && existing) {
        const updated: Tag = {
          ...existing,
          name: p.name as string,
          color: p.color as string,
          updated_at: new Date().toISOString(),
        };
        mockTags[idx] = updated;
        respond(client, msg.id, validatePayload(TagSchema, updated, "tag.update response"));
        push(client, "tag.updated", updated);
      } else {
        respondError(client, msg.id, "tag not found");
      }
      break;
    }

    case "tag.delete": {
      const delIdx = mockTags.findIndex((t) => t.id === p.id);
      if (delIdx >= 0) {
        mockTags.splice(delIdx, 1);
        // Remove project-tag associations
        for (let i = mockProjectTags.length - 1; i >= 0; i--) {
          const pt = mockProjectTags[i];
          if (pt && pt.tag_id === p.id) mockProjectTags.splice(i, 1);
        }
      }
      respond(client, msg.id);
      push(client, "tag.deleted", { id: p.id });
      break;
    }

    case "project.set-favorite": {
      // In mock mode, just echo back the project with updated favorite
      const proj = MOCK_PROJECTS.find((proj) => proj.id === p.projectId);
      if (proj) {
        proj.favorite = p.favorite ? 1 : 0;
        respond(client, msg.id, proj);
        push(client, "project.updated", proj);
      } else {
        respondError(client, msg.id, "project not found");
      }
      break;
    }

    case "project.set-tags": {
      const projectId = p.projectId as string;
      const tagIds = (p.tagIds as string[]) ?? [];
      // Update mutable project-tags
      for (let i = mockProjectTags.length - 1; i >= 0; i--) {
        const pt = mockProjectTags[i];
        if (pt && pt.project_id === projectId) mockProjectTags.splice(i, 1);
      }
      for (const tagId of tagIds) {
        mockProjectTags.push({ project_id: projectId, tag_id: tagId });
      }
      const assignedTags = mockTags.filter((t) => tagIds.includes(t.id));
      respond(client, msg.id, assignedTags);
      push(client, "project.tags-updated", { projectId, tags: assignedTags });
      break;
    }

    case "project.reorder":
      respond(client, msg.id);
      break;

    case "session.set-model":
    case "session.set-permission":
    case "session.set-auto-approve":
    case "session.resolve-approval":
    case "session.resolve-question":
    case "session.interrupt":
    case "project.fetch":
    case "project.push":
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
