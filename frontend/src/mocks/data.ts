import type { Project } from "~/lib/types";
import type { ProjectGitStatus } from "~/stores/app-store";
import type { PendingApproval, PendingQuestion, SessionMetadata } from "~/stores/chat-store";

// --- Timestamp helpers ---

const now = Date.now();
const hoursAgo = (h: number) => new Date(now - h * 3600000).toISOString();
const daysAgo = (d: number) => new Date(now - d * 86400000).toISOString();

// --- IDs ---

// IDs must be UUID-shaped — sessionShortId uses the first segment (before first "-")
const P = {
  agentique: "aaa00001-0000-4000-8000-000000000001",
  thePint: "aaa00002-0000-4000-8000-000000000002",
  alltixApi: "aaa00003-0000-4000-8000-000000000003",
} as const;

const S = {
  authRefactor: "bbb00001-0000-4000-8000-000000000001",
  wsReconnect: "bbb00002-0000-4000-8000-000000000002",
  darkMode: "bbb00003-0000-4000-8000-000000000003",
  migrationBug: "bbb00004-0000-4000-8000-000000000004",
  updateDeps: "bbb00005-0000-4000-8000-000000000005",
  paymentFlow: "ccc00001-0000-4000-8000-000000000001",
  imageGallery: "ccc00002-0000-4000-8000-000000000002",
  schedulerTests: "ddd00001-0000-4000-8000-000000000001",
} as const;

export { S as SESSION_IDS, P as PROJECT_IDS };

// --- Projects ---

export const MOCK_PROJECTS: Project[] = [
  {
    id: P.agentique,
    name: "agentique",
    path: "/home/user/git/agentique",
    slug: "agentique",
    default_model: "sonnet",
    default_permission_mode: "default",
    default_system_prompt: "",
    sort_order: 1,
    created_at: daysAgo(30),
    updated_at: hoursAgo(2),
  },
  {
    id: P.thePint,
    name: "The Pint",
    path: "/home/user/git/the-pint",
    slug: "the-pint",
    default_model: "sonnet",
    default_permission_mode: "default",
    default_system_prompt: "",
    sort_order: 2,
    created_at: daysAgo(60),
    updated_at: daysAgo(1),
  },
  {
    id: P.alltixApi,
    name: "alltix-api",
    path: "/home/user/git/alltix/api",
    slug: "alltix-api",
    default_model: "sonnet",
    default_permission_mode: "default",
    default_system_prompt: "",
    sort_order: 3,
    created_at: daysAgo(90),
    updated_at: daysAgo(3),
  },
];

// --- Sessions ---

const AGENTIQUE_SESSIONS: SessionMetadata[] = [
  {
    id: S.authRefactor,
    projectId: P.agentique,
    name: "Refactor auth middleware",
    state: "idle",
    connected: true,
    model: "opus",
    permissionMode: "default",
    autoApprove: true,
    worktreePath: "/home/user/git/agentique/.agentique/worktrees/session-auth-refactor",
    worktreeBranch: "session-auth-refactor",
    commitsAhead: 3,
    commitsBehind: 0,
    hasUncommitted: true,
    turnCount: 2,
    createdAt: hoursAgo(6),
    updatedAt: hoursAgo(1),
  },
  {
    id: S.wsReconnect,
    projectId: P.agentique,
    name: "Fix WebSocket reconnect",
    state: "running",
    connected: true,
    model: "opus",
    permissionMode: "default",
    worktreePath: "/home/user/git/agentique/.agentique/worktrees/session-ws-reconnect",
    worktreeBranch: "session-ws-reconnect",
    commitsAhead: 1,
    commitsBehind: 0,
    hasUncommitted: false,
    turnCount: 1,
    createdAt: hoursAgo(2),
    updatedAt: hoursAgo(0.1),
  },
  {
    id: S.darkMode,
    projectId: P.agentique,
    name: "Add dark mode support",
    state: "done",
    connected: false,
    model: "opus",
    permissionMode: "default",
    autoApprove: true,
    worktreePath: "/home/user/git/agentique/.agentique/worktrees/session-dark-mode",
    worktreeBranch: "session-dark-mode",
    worktreeMerged: true,
    commitsAhead: 0,
    commitsBehind: 0,
    prUrl: "https://github.com/example/agentique/pull/42",
    turnCount: 1,
    createdAt: daysAgo(2),
    updatedAt: daysAgo(1),
  },
  {
    id: S.migrationBug,
    projectId: P.agentique,
    name: "Database migration bug",
    state: "failed",
    connected: false,
    model: "sonnet",
    permissionMode: "default",
    worktreePath: "/home/user/git/agentique/.agentique/worktrees/session-migration-bug",
    worktreeBranch: "session-migration-bug",
    commitsAhead: 1,
    commitsBehind: 2,
    hasUncommitted: false,
    turnCount: 1,
    createdAt: daysAgo(3),
    updatedAt: daysAgo(2),
  },
  {
    id: S.updateDeps,
    projectId: P.agentique,
    name: "Update dependencies",
    state: "stopped",
    connected: false,
    model: "haiku",
    permissionMode: "default",
    turnCount: 1,
    createdAt: daysAgo(5),
    updatedAt: daysAgo(4),
  },
];

const THE_PINT_SESSIONS: SessionMetadata[] = [
  {
    id: S.paymentFlow,
    projectId: P.thePint,
    name: "Payment flow redesign",
    state: "running",
    connected: true,
    model: "opus",
    permissionMode: "plan",
    worktreePath: "/home/user/git/the-pint/.agentique/worktrees/session-payment-flow",
    worktreeBranch: "session-payment-flow",
    commitsAhead: 0,
    commitsBehind: 0,
    hasUncommitted: false,
    turnCount: 1,
    createdAt: hoursAgo(1),
    updatedAt: hoursAgo(0.5),
  },
  {
    id: S.imageGallery,
    projectId: P.thePint,
    name: "Image gallery component",
    state: "idle",
    connected: true,
    model: "sonnet",
    permissionMode: "default",
    autoApprove: true,
    worktreePath: "/home/user/git/the-pint/.agentique/worktrees/session-image-gallery",
    worktreeBranch: "session-image-gallery",
    commitsAhead: 2,
    commitsBehind: 0,
    hasUncommitted: false,
    turnCount: 1,
    createdAt: hoursAgo(4),
    updatedAt: hoursAgo(3),
  },
];

const ALLTIX_SESSIONS: SessionMetadata[] = [
  {
    id: S.schedulerTests,
    projectId: P.alltixApi,
    name: "Scheduler test coverage",
    state: "idle",
    connected: true,
    model: "opus",
    permissionMode: "default",
    turnCount: 1,
    createdAt: daysAgo(1),
    updatedAt: hoursAgo(12),
  },
];

export const MOCK_SESSIONS: Record<string, SessionMetadata[]> = {
  [P.agentique]: AGENTIQUE_SESSIONS,
  [P.thePint]: THE_PINT_SESSIONS,
  [P.alltixApi]: ALLTIX_SESSIONS,
};

// --- Project git status ---

export const MOCK_PROJECT_GIT_STATUS: Record<string, ProjectGitStatus> = {
  [P.agentique]: {
    projectId: P.agentique,
    branch: "master",
    hasRemote: true,
    aheadRemote: 0,
    behindRemote: 1,
    uncommittedCount: 0,
  },
  [P.thePint]: {
    projectId: P.thePint,
    branch: "main",
    hasRemote: true,
    aheadRemote: 2,
    behindRemote: 0,
    uncommittedCount: 3,
  },
  [P.alltixApi]: {
    projectId: P.alltixApi,
    branch: "develop",
    hasRemote: true,
    aheadRemote: 0,
    behindRemote: 0,
    uncommittedCount: 0,
  },
};

// --- Push events (sent after subscription) ---

export const MOCK_PENDING_APPROVALS: Record<string, PendingApproval> = {
  [S.wsReconnect]: {
    approvalId: "mock-approval-001",
    toolName: "Bash",
    input: {
      command: "cd frontend && npm test -- --run src/lib/ws-client.test.ts",
      description: "Run WebSocket client tests",
    },
  },
};

export const MOCK_PENDING_QUESTIONS: Record<string, PendingQuestion> = {
  [S.paymentFlow]: {
    questionId: "mock-question-001",
    questions: [
      {
        question: "How should the payment steps be organized?",
        header: "STEP LAYOUT",
        options: [
          { label: "Linear wizard", description: "One step at a time, sequential" },
          {
            label: "Stepper with navigation",
            description: "Step indicators, can jump between steps",
          },
          { label: "Accordion", description: "All steps visible, collapse/expand" },
        ],
      },
      {
        question: "Which payment methods should be supported?",
        header: "PAYMENT METHODS",
        options: [
          { label: "Credit/Debit card" },
          { label: "Swish" },
          { label: "Invoice (Klarna)" },
          { label: "Apple Pay / Google Pay" },
        ],
        multiSelect: true,
      },
      {
        question: "Where should the order summary appear during checkout?",
        header: "ORDER SUMMARY",
        options: [
          {
            label: "Sticky sidebar",
            description: "Always visible on desktop, collapsible on mobile",
          },
          { label: "Collapsible header", description: "Expandable summary above the form" },
          { label: "Final review step", description: "Separate step before confirming payment" },
        ],
      },
      {
        question: "How should field validation work?",
        header: "VALIDATION",
        options: [
          { label: "Inline on blur", description: "Validate each field when it loses focus" },
          { label: "On step change", description: "Validate all fields when moving to next step" },
          { label: "Both", description: "Inline hints + full validation on step change" },
        ],
      },
    ],
  },
};

// --- Turn history (wire format — raw events as the backend would return) ---

// Small SVG placeholder for image attachment mock
const PLACEHOLDER_IMAGE =
  "data:image/svg+xml;base64,PHN2ZyB3aWR0aD0iNDAwIiBoZWlnaHQ9IjMwMCIgeG1sbnM9Imh0dHA6Ly93d3cudzMub3JnLzIwMDAvc3ZnIj48cmVjdCB3aWR0aD0iNDAwIiBoZWlnaHQ9IjMwMCIgZmlsbD0iIzFhMWEyZSIvPjxyZWN0IHg9IjIwIiB5PSIyMCIgd2lkdGg9IjM2MCIgaGVpZ2h0PSIyNjAiIHJ4PSI4IiBmaWxsPSIjMjUyNTNhIiBzdHJva2U9IiMzMzMzNTUiLz48dGV4dCB4PSI1MCUiIHk9IjQ1JSIgZmlsbD0iIzg4ODhhYSIgZm9udC1mYW1pbHk9InNhbnMtc2VyaWYiIGZvbnQtc2l6ZT0iMTgiIHRleHQtYW5jaG9yPSJtaWRkbGUiPkN1cnJlbnQgVXBsb2FkIFVJPC90ZXh0Pjx0ZXh0IHg9IjUwJSIgeT0iNTglIiBmaWxsPSIjNjY2Njg4IiBmb250LWZhbWlseT0ic2Fucy1zZXJpZiIgZm9udC1zaXplPSIxMiIgdGV4dC1hbmNob3I9Im1pZGRsZSI+U2NyZWVuc2hvdCBmcm9tIGJyb3dzZXI8L3RleHQ+PC9zdmc+";

// Helper to build wire-format events for history turns
interface WireEvent {
  type: string;
  [key: string]: unknown;
}

function text(content: string): WireEvent {
  return { type: "text", content };
}

function thinking(content: string): WireEvent {
  return { type: "thinking", content };
}

function toolUse(
  toolName: string,
  toolInput: Record<string, unknown>,
  category: string,
  toolId?: string,
): WireEvent {
  return {
    type: "tool_use",
    toolId: toolId ?? `mock-tool-${Math.random().toString(36).slice(2, 8)}`,
    toolName,
    toolInput,
    category,
  };
}

function toolResult(toolId: string, content: string): WireEvent {
  return {
    type: "tool_result",
    toolId,
    content: [{ type: "text", text: content }],
  };
}

function result(duration: number): WireEvent {
  return {
    type: "result",
    cost: 0,
    duration,
    usage: { inputTokens: Math.floor(duration * 2.5), outputTokens: Math.floor(duration * 0.8) },
    stopReason: "end_turn",
  };
}

function errorEvent(message: string, fatal = false): WireEvent {
  return { type: "error", message, fatal };
}

// --- Turn data per session ---

interface MockTurn {
  prompt: string;
  attachments?: { id: string; name: string; mimeType: string; dataUrl: string }[];
  events: WireEvent[];
}

const authRefactorTurns: MockTurn[] = [
  {
    prompt: "Refactor the auth middleware to use the new token validation library",
    events: [
      thinking(
        "I need to examine the current auth middleware implementation and understand the deprecated API it uses, then update it to the new jwt.Verify approach.",
      ),
      text("I'll start by reading the current implementation."),
      (() => {
        const id = "mock-tool-ar-01";
        return toolUse(
          "Read",
          { file_path: "/home/user/git/agentique/backend/internal/auth/middleware.go" },
          "file_read",
          id,
        );
      })(),
      toolResult(
        "mock-tool-ar-01",
        `package auth

import (
\t"net/http"
\t"strings"
)

// Middleware validates JWT tokens on incoming requests.
func Middleware(next http.Handler) http.Handler {
\treturn http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
\t\ttoken := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
\t\tif token == "" {
\t\t\thttp.Error(w, "unauthorized", http.StatusUnauthorized)
\t\t\treturn
\t\t}
\t\t// DEPRECATED: ValidateToken will be removed in v2
\t\tclaims, err := ValidateToken(token)
\t\tif err != nil {
\t\t\thttp.Error(w, "invalid token", http.StatusUnauthorized)
\t\t\treturn
\t\t}
\t\tctx := WithClaims(r.Context(), claims)
\t\tnext.ServeHTTP(w, r.WithContext(ctx))
\t})
}`,
      ),
      text(
        "The current implementation uses the deprecated `ValidateToken` function. I'll update it to use the new `jwt.Verify` API with structured error handling.",
      ),
      (() => {
        const id = "mock-tool-ar-02";
        return toolUse(
          "Edit",
          {
            file_path: "/home/user/git/agentique/backend/internal/auth/middleware.go",
            old_string:
              '\t\tclaims, err := ValidateToken(token)\n\t\tif err != nil {\n\t\t\thttp.Error(w, "invalid token", http.StatusUnauthorized)\n\t\t\treturn\n\t\t}',
            new_string:
              '\t\tclaims, err := jwt.Verify(token, jwt.WithAudience("agentique"))\n\t\tif err != nil {\n\t\t\tswitch {\n\t\t\tcase errors.Is(err, jwt.ErrExpired):\n\t\t\t\thttp.Error(w, "token expired", http.StatusUnauthorized)\n\t\t\tcase errors.Is(err, jwt.ErrMalformed):\n\t\t\t\thttp.Error(w, "malformed token", http.StatusBadRequest)\n\t\t\tdefault:\n\t\t\t\thttp.Error(w, "invalid token", http.StatusUnauthorized)\n\t\t\t}\n\t\t\treturn\n\t\t}',
          },
          "file_write",
          id,
        );
      })(),
      toolResult("mock-tool-ar-02", "File edited successfully."),
      (() => {
        const id = "mock-tool-ar-03";
        return toolUse(
          "Bash",
          {
            command: "cd backend && go test ./internal/auth/... -count=1",
            description: "Run auth tests",
          },
          "command",
          id,
        );
      })(),
      toolResult("mock-tool-ar-03", "ok  \tagentique/internal/auth\t0.042s\n\nPASS"),
      toolUse(
        "TodoWrite",
        {
          todos: [
            { content: "Update ValidateToken to jwt.Verify", status: "completed" },
            { content: "Add structured error handling", status: "completed" },
            { content: "Update integration tests for new error types", status: "pending" },
            { content: "Update API documentation", status: "pending" },
          ],
        },
        "task",
        "mock-tool-ar-04",
      ),
      toolResult("mock-tool-ar-04", "Todos updated."),
      text(
        "Auth middleware refactored. Changed `ValidateToken()` to `jwt.Verify()` with audience validation, added structured error handling for expired and malformed tokens. Tests pass.",
      ),
      result(45000),
    ],
  },
  {
    prompt: "Also update the integration test to cover the new error cases",
    events: [
      thinking(
        "I need to add test cases for expired, malformed, and revoked tokens to the integration test suite.",
      ),
      (() => {
        const id = "mock-tool-ar-05";
        return toolUse(
          "Read",
          { file_path: "/home/user/git/agentique/backend/internal/auth/middleware_test.go" },
          "file_read",
          id,
        );
      })(),
      toolResult(
        "mock-tool-ar-05",
        `package auth_test

import (
\t"net/http"
\t"net/http/httptest"
\t"testing"
)

func TestMiddleware_ValidToken(t *testing.T) {
\t// ... existing test
}

func TestMiddleware_MissingToken(t *testing.T) {
\t// ... existing test
}`,
      ),
      (() => {
        const id = "mock-tool-ar-06";
        return toolUse(
          "Edit",
          {
            file_path: "/home/user/git/agentique/backend/internal/auth/middleware_test.go",
            old_string: "// end of file",
            new_string:
              "func TestMiddleware_ExpiredToken(t *testing.T) { ... }\nfunc TestMiddleware_MalformedToken(t *testing.T) { ... }\nfunc TestMiddleware_RevokedToken(t *testing.T) { ... }",
          },
          "file_write",
          id,
        );
      })(),
      toolResult("mock-tool-ar-06", "File edited successfully."),
      (() => {
        const id = "mock-tool-ar-07";
        return toolUse(
          "Bash",
          {
            command: "cd backend && go test ./internal/auth/... -v -count=1",
            description: "Run auth tests verbose",
          },
          "command",
          id,
        );
      })(),
      toolResult(
        "mock-tool-ar-07",
        `=== RUN   TestMiddleware_ValidToken
--- PASS: TestMiddleware_ValidToken (0.01s)
=== RUN   TestMiddleware_MissingToken
--- PASS: TestMiddleware_MissingToken (0.00s)
=== RUN   TestMiddleware_ExpiredToken
--- PASS: TestMiddleware_ExpiredToken (0.01s)
=== RUN   TestMiddleware_MalformedToken
--- PASS: TestMiddleware_MalformedToken (0.00s)
=== RUN   TestMiddleware_RevokedToken
--- PASS: TestMiddleware_RevokedToken (0.01s)
PASS
ok  \tagentique/internal/auth\t0.038s`,
      ),
      toolUse(
        "TodoWrite",
        {
          todos: [
            { content: "Update ValidateToken to jwt.Verify", status: "completed" },
            { content: "Add structured error handling", status: "completed" },
            { content: "Update integration tests for new error types", status: "completed" },
            { content: "Update API documentation", status: "pending" },
          ],
        },
        "task",
        "mock-tool-ar-08",
      ),
      toolResult("mock-tool-ar-08", "Todos updated."),
      text(
        "Added 3 new test cases covering expired tokens, malformed tokens, and revoked tokens. All 5 tests pass.",
      ),
      result(32000),
    ],
  },
];

const wsReconnectTurns: MockTurn[] = [
  {
    prompt:
      "The WebSocket reconnect logic sometimes gets stuck in a loop when the server returns 503. Debug and fix.",
    events: [
      thinking(
        "Let me investigate the reconnect logic. A 503 typically means the server is temporarily unavailable. If the reconnect doesn't back off properly for this case, it could hammer the server.",
      ),
      text(
        "I'll look at the WebSocket client implementation to understand the reconnect behavior.",
      ),
      (() => {
        const id = "mock-tool-wr-01";
        return toolUse(
          "Grep",
          {
            pattern: "reconnect|backoff|onclose",
            glob: "**/*.ts",
            output_mode: "files_with_matches",
          },
          "file_read",
          id,
        );
      })(),
      toolResult(
        "mock-tool-wr-01",
        "frontend/src/lib/ws-client.ts\nfrontend/src/hooks/useWebSocket.ts",
      ),
      (() => {
        const id = "mock-tool-wr-02";
        return toolUse(
          "Read",
          { file_path: "/home/user/git/agentique/frontend/src/lib/ws-client.ts" },
          "file_read",
          id,
        );
      })(),
      toolResult(
        "mock-tool-wr-02",
        `// ... ws-client.ts contents (210 lines)
// Key issue: onclose handler always uses same backoff regardless of close code
ws.onclose = (ev) => {
  // reconnectDelay resets to 500 on successful connect
  // but doesn't differentiate between close codes
  this.reconnectTimer = setTimeout(() => this.connect(), this.reconnectDelay);
  this.reconnectDelay = Math.min(this.reconnectDelay * 2, 30000);
};`,
      ),
      text(
        "Found the issue. The `onclose` handler uses the same exponential backoff regardless of the close code. For 503 (service unavailable), the server sends close code 1013 (Try Again Later). The client should:\n\n1. Check the close code\n2. For 1013, use a longer initial delay\n3. Add jitter to prevent thundering herd\n\nLet me fix this.",
      ),
      // This is where the pending tool approval kicks in - the Edit tool_use is shown,
      // but the Bash tool (running tests) needs approval
      (() => {
        const id = "mock-tool-wr-03";
        return toolUse(
          "Edit",
          {
            file_path: "/home/user/git/agentique/frontend/src/lib/ws-client.ts",
            old_string:
              "this.reconnectTimer = setTimeout(() => this.connect(), this.reconnectDelay);",
            new_string:
              "const jitter = Math.random() * 1000;\nconst delay = ev.code === 1013 ? Math.max(5000, this.reconnectDelay) : this.reconnectDelay;\nthis.reconnectTimer = setTimeout(() => this.connect(), delay + jitter);",
          },
          "file_write",
          id,
        );
      })(),
      toolResult("mock-tool-wr-03", "File edited successfully."),
      // Turn incomplete — still running, next tool needs approval
    ],
  },
];

const darkModeTurns: MockTurn[] = [
  {
    prompt: "Add a dark mode toggle to the settings page. Use CSS custom properties for theming.",
    events: [
      thinking(
        "I'll implement dark mode using CSS custom properties and a React context for the theme state.",
      ),
      text(
        "I'll implement dark mode with:\n1. CSS custom properties for color tokens\n2. A `ThemeProvider` context\n3. A toggle component in settings\n4. `prefers-color-scheme` media query for default",
      ),
      (() => {
        const id = "mock-tool-dm-01";
        return toolUse(
          "Agent",
          {
            description: "Explore current theme/color setup",
            subagent_type: "Explore",
            prompt: "Find all CSS color definitions and theme-related code",
          },
          "agent",
          id,
        );
      })(),
      toolResult(
        "mock-tool-dm-01",
        "Found color definitions in:\n- src/index.css (Tailwind theme)\n- src/components/ui/ (component-level colors)\n\nCurrently hardcoded to dark theme. No theme toggle exists.",
      ),
      (() => {
        const id = "mock-tool-dm-02";
        return toolUse(
          "Write",
          { file_path: "src/contexts/theme.tsx", content: "// ThemeProvider implementation..." },
          "file_write",
          id,
        );
      })(),
      toolResult("mock-tool-dm-02", "File written successfully."),
      (() => {
        const id = "mock-tool-dm-03";
        return toolUse(
          "Edit",
          {
            file_path: "src/index.css",
            old_string: ":root {",
            new_string: ':root, [data-theme="dark"] {',
          },
          "file_write",
          id,
        );
      })(),
      toolResult("mock-tool-dm-03", "File edited successfully."),
      (() => {
        const id = "mock-tool-dm-04";
        return toolUse(
          "Bash",
          { command: "cd frontend && npx tsc --noEmit", description: "Typecheck" },
          "command",
          id,
        );
      })(),
      toolResult("mock-tool-dm-04", "No errors found."),
      text(
        "Dark mode implemented. The toggle persists to localStorage and respects `prefers-color-scheme` on first visit.",
      ),
      result(67000),
    ],
  },
];

const migrationBugTurns: MockTurn[] = [
  {
    prompt:
      "The migration 007_add_session_tags is failing in production. The error is: UNIQUE constraint failed: session_tags.session_id, session_tags.tag",
    events: [
      thinking(
        "A unique constraint failure during migration means there's duplicate data already in the table, or the migration is trying to insert data that conflicts.",
      ),
      (() => {
        const id = "mock-tool-mb-01";
        return toolUse(
          "Read",
          { file_path: "/home/user/git/agentique/backend/db/migrations/007_add_session_tags.sql" },
          "file_read",
          id,
        );
      })(),
      toolResult(
        "mock-tool-mb-01",
        `-- +goose Up
CREATE TABLE session_tags (
    session_id TEXT NOT NULL REFERENCES sessions(id),
    tag TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(session_id, tag)
);

-- Backfill tags from session names
INSERT INTO session_tags (session_id, tag)
SELECT id, 'imported' FROM sessions;`,
      ),
      text(
        "Found it. The migration creates the table with a UNIQUE constraint and then backfills with an `INSERT` that would fail if any session already has the 'imported' tag. In production, this migration may have partially run before.",
      ),
      (() => {
        const id = "mock-tool-mb-02";
        return toolUse(
          "Bash",
          {
            command:
              "cd backend && sqlite3 agentique.db \"SELECT count(*) FROM session_tags WHERE tag='imported';\"",
            description: "Check existing tags",
          },
          "command",
          id,
        );
      })(),
      toolResult("mock-tool-mb-02", ""),
      errorEvent(
        "Session timed out waiting for Claude CLI response. The Claude API may be experiencing issues.",
        true,
      ),
    ],
  },
];

const updateDepsTurns: MockTurn[] = [
  {
    prompt: "Update all Go dependencies to latest and run tests",
    events: [
      thinking(
        "I'll update Go module dependencies and verify everything still compiles and passes tests.",
      ),
      (() => {
        const id = "mock-tool-ud-01";
        return toolUse(
          "Bash",
          {
            command: "cd backend && go get -u ./... && go mod tidy",
            description: "Update Go deps",
          },
          "command",
          id,
        );
      })(),
      toolResult(
        "mock-tool-ud-01",
        "go: upgraded github.com/mattn/go-sqlite3 v1.14.22 => v1.14.24\ngo: upgraded golang.org/x/net v0.25.0 => v0.28.0\ngo: upgraded nhooyr.io/websocket v1.8.10 => v1.8.17",
      ),
      text("Dependencies updated. Running tests..."),
      (() => {
        const id = "mock-tool-ud-02";
        return toolUse(
          "Bash",
          { command: "cd backend && go test ./... -count=1", description: "Run all Go tests" },
          "command",
          id,
        );
      })(),
      toolResult(
        "mock-tool-ud-02",
        "ok  \tagentique/internal/auth\t0.042s\nok  \tagentique/internal/session\t1.234s\nok  \tagentique/internal/store\t0.567s\nok  \tagentique/internal/ws\t0.089s\n\nPASS",
      ),
      text(
        "All Go dependencies updated and tests pass. Updated 3 packages:\n- `go-sqlite3`: 1.14.22 -> 1.14.24\n- `golang.org/x/net`: 0.25.0 -> 0.28.0\n- `nhooyr.io/websocket`: 1.8.10 -> 1.8.17",
      ),
      result(28000),
    ],
  },
];

const paymentFlowTurns: MockTurn[] = [
  {
    prompt:
      "I want to redesign the payment flow. Currently it's a single long form, but I want a multi-step wizard. Explore the current code and suggest an approach.",
    events: [
      thinking(
        "Let me explore the current payment flow implementation to understand the form structure, validation, and state management before proposing a redesign.",
      ),
      (() => {
        const id = "mock-tool-pf-01";
        return toolUse(
          "Agent",
          {
            description: "Explore payment flow code",
            subagent_type: "Explore",
            prompt:
              "Find all files related to payment, checkout, or order flow. Look at form structure, validation, state management.",
          },
          "agent",
          id,
        );
      })(),
      toolResult(
        "mock-tool-pf-01",
        `Found payment-related files:
- src/pages/checkout/CheckoutPage.tsx (420 lines) — monolithic form
- src/pages/checkout/PaymentForm.tsx (180 lines) — card input fields
- src/pages/checkout/OrderSummary.tsx (95 lines) — order total display
- src/hooks/useCheckout.ts (120 lines) — form state + submission
- src/api/orders.ts — order creation API

The CheckoutPage renders everything in one scroll: shipping → payment → review → submit.
Validation runs on form submit. No step navigation.`,
      ),
      text(
        "I've analyzed the current checkout flow. It's a single 420-line monolithic form. Before I start coding the wizard, I need your input on a few design decisions.",
      ),
      // Turn is incomplete — waiting for the question dialog
    ],
  },
];

const imageGalleryTurns: MockTurn[] = [
  {
    prompt:
      "Here's the current image gallery. I want to add drag-and-drop reordering and lazy loading.",
    attachments: [
      {
        id: "mock-attach-001",
        name: "gallery-screenshot.png",
        mimeType: "image/png",
        dataUrl: PLACEHOLDER_IMAGE,
      },
    ],
    events: [
      thinking(
        "The user shared a screenshot of the current gallery. I need to add drag-and-drop reordering (probably using dnd-kit or react-beautiful-dnd) and lazy loading (IntersectionObserver or a library like react-lazyload).",
      ),
      text(
        "I can see the current grid layout. I'll add:\n1. **Drag-and-drop reordering** using `@dnd-kit/sortable` — lightweight, accessible, and works well with grid layouts\n2. **Lazy loading** using `IntersectionObserver` — no extra dependency needed\n\nLet me implement both.",
      ),
      (() => {
        const id = "mock-tool-ig-01";
        return toolUse(
          "Read",
          { file_path: "/home/user/git/the-pint/src/components/ImageGallery.tsx" },
          "file_read",
          id,
        );
      })(),
      toolResult(
        "mock-tool-ig-01",
        `export function ImageGallery({ images }: { images: GalleryImage[] }) {
  return (
    <div className="grid grid-cols-3 gap-4">
      {images.map((img) => (
        <div key={img.id} className="aspect-square overflow-hidden rounded-lg">
          <img src={img.url} alt={img.alt} className="h-full w-full object-cover" />
        </div>
      ))}
    </div>
  );
}`,
      ),
      (() => {
        const id = "mock-tool-ig-02";
        return toolUse(
          "Bash",
          {
            command:
              "cd /home/user/git/the-pint && npm install @dnd-kit/core @dnd-kit/sortable @dnd-kit/utilities",
            description: "Install dnd-kit",
          },
          "command",
          id,
        );
      })(),
      toolResult("mock-tool-ig-02", "added 3 packages in 2.1s"),
      (() => {
        const id = "mock-tool-ig-03";
        return toolUse(
          "Write",
          {
            file_path: "/home/user/git/the-pint/src/components/ImageGallery.tsx",
            content: "// Rewritten with DndContext, SortableContext, and IntersectionObserver...",
          },
          "file_write",
          id,
        );
      })(),
      toolResult("mock-tool-ig-03", "File written successfully."),
      (() => {
        const id = "mock-tool-ig-04";
        return toolUse(
          "Write",
          {
            file_path: "/home/user/git/the-pint/src/hooks/useLazyImage.ts",
            content: "// IntersectionObserver hook for lazy loading...",
          },
          "file_write",
          id,
        );
      })(),
      toolResult("mock-tool-ig-04", "File written successfully."),
      text(
        "Gallery rewritten with drag-and-drop reordering and lazy loading:\n- Drag to reorder uses `@dnd-kit/sortable` with collision detection\n- Images lazy-load with `IntersectionObserver` (128px root margin for pre-loading)\n- Drag overlay shows a semi-transparent preview\n- Order changes fire `onReorder` callback with the new array",
      ),
      result(52000),
    ],
  },
];

const schedulerTestsTurns: MockTurn[] = [
  {
    prompt: "Review the scheduler package test coverage and identify gaps",
    events: [
      thinking(
        "I'll analyze the scheduler package to understand what's tested and what's missing.",
      ),
      (() => {
        const id = "mock-tool-st-01";
        return toolUse(
          "Bash",
          {
            command: "cd /home/user/git/alltix/api && go test -cover ./internal/scheduler/...",
            description: "Check scheduler test coverage",
          },
          "command",
          id,
        );
      })(),
      toolResult(
        "mock-tool-st-01",
        "ok  \talltix-api/internal/scheduler\t0.234s\tcoverage: 47.2% of statements",
      ),
      (() => {
        const id = "mock-tool-st-02";
        return toolUse(
          "Bash",
          {
            command:
              "cd /home/user/git/alltix/api && go test -coverprofile=coverage.out ./internal/scheduler/... && go tool cover -func=coverage.out | grep -v '100.0%'",
            description: "Find uncovered functions",
          },
          "command",
          id,
        );
      })(),
      toolResult(
        "mock-tool-st-02",
        `alltix-api/internal/scheduler/scheduler.go:45:\tRun\t\t\t38.5%
alltix-api/internal/scheduler/scheduler.go:89:\thandleError\t\t0.0%
alltix-api/internal/scheduler/scheduler.go:112:\tretryWithBackoff\t0.0%
alltix-api/internal/scheduler/scheduler.go:145:\tcleanupStale\t\t22.2%
alltix-api/internal/scheduler/cron.go:23:\t\tParseCron\t\t85.7%
alltix-api/internal/scheduler/cron.go:67:\t\tNextRun\t\t\t66.7%`,
      ),
      text(
        "Coverage is at 47.2%. Key gaps:\n\n| Function | Coverage | Priority |\n|----------|----------|----------|\n| `handleError` | 0% | High — error paths are critical |\n| `retryWithBackoff` | 0% | High — retry logic is subtle |\n| `Run` | 38.5% | Medium — main loop, some paths covered |\n| `cleanupStale` | 22.2% | Medium — cleanup edge cases |\n| `NextRun` | 66.7% | Low — mostly covered |\n\nRecommend starting with `handleError` and `retryWithBackoff` — both are 0% and handle failure scenarios that matter most in production.",
      ),
      result(18000),
    ],
  },
];

export const MOCK_TURNS: Record<string, MockTurn[]> = {
  [S.authRefactor]: authRefactorTurns,
  [S.wsReconnect]: wsReconnectTurns,
  [S.darkMode]: darkModeTurns,
  [S.migrationBug]: migrationBugTurns,
  [S.updateDeps]: updateDepsTurns,
  [S.paymentFlow]: paymentFlowTurns,
  [S.imageGallery]: imageGalleryTurns,
  [S.schedulerTests]: schedulerTestsTurns,
};
