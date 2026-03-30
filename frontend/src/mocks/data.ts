import type {
  BehaviorPresets,
  Project,
  ProjectGitStatus,
  ProjectTag,
  SessionInfo,
  Tag,
} from "~/lib/generated-types";
import type { PendingApproval, PendingQuestion } from "~/stores/chat-store";

// --- Timestamp helpers ---

const DEFAULT_PRESETS: BehaviorPresets = {
  autoCommit: true,
  suggestParallel: true,
  planFirst: false,
  terse: false,
};

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
  queryOptimizer: "bbb00006-0000-4000-8000-000000000006",
} as const;

const T = {
  work: "ttt00001-0000-4000-8000-000000000001",
  personal: "ttt00002-0000-4000-8000-000000000002",
  infra: "ttt00003-0000-4000-8000-000000000003",
} as const;

export { S as SESSION_IDS, P as PROJECT_IDS, T as TAG_IDS };

// --- Tags ---

export const MOCK_TAGS: Tag[] = [
  {
    id: T.work,
    name: "Work",
    color: "blue",
    sort_order: 0,
    created_at: daysAgo(30),
    updated_at: daysAgo(30),
  },
  {
    id: T.personal,
    name: "Personal",
    color: "green",
    sort_order: 1,
    created_at: daysAgo(30),
    updated_at: daysAgo(30),
  },
  {
    id: T.infra,
    name: "Infra",
    color: "orange",
    sort_order: 2,
    created_at: daysAgo(14),
    updated_at: daysAgo(14),
  },
];

export const MOCK_PROJECT_TAGS: ProjectTag[] = [
  { project_id: P.agentique, tag_id: T.personal },
  { project_id: P.agentique, tag_id: T.infra },
  { project_id: P.thePint, tag_id: T.personal },
  { project_id: P.alltixApi, tag_id: T.work },
];

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
    default_behavior_presets: "{}",
    sort_order: 1,
    favorite: 1,
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
    default_behavior_presets: "{}",
    sort_order: 2,
    favorite: 0,
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
    default_behavior_presets: "{}",
    sort_order: 3,
    favorite: 0,
    created_at: daysAgo(90),
    updated_at: daysAgo(3),
  },
];

// --- Sessions ---

const AGENTIQUE_SESSIONS: SessionInfo[] = [
  {
    id: S.authRefactor,
    projectId: P.agentique,
    name: "Refactor auth middleware",
    state: "idle",
    connected: true,
    model: "opus",
    permissionMode: "default",
    autoApproveMode: "auto",
    behaviorPresets: DEFAULT_PRESETS,
    worktreePath: "/home/user/git/agentique/.agentique/worktrees/session-auth-refactor",
    worktreeBranch: "session-auth-refactor",
    gitVersion: 1,
    commitsAhead: 3,
    commitsBehind: 0,
    hasUncommitted: true,
    turnCount: 2,
    totalCost: 0,
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
    autoApproveMode: "manual",
    behaviorPresets: DEFAULT_PRESETS,
    worktreePath: "/home/user/git/agentique/.agentique/worktrees/session-ws-reconnect",
    worktreeBranch: "session-ws-reconnect",
    gitVersion: 1,
    commitsAhead: 1,
    commitsBehind: 0,
    hasUncommitted: false,
    turnCount: 1,
    totalCost: 0,
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
    autoApproveMode: "auto",
    behaviorPresets: DEFAULT_PRESETS,
    worktreePath: "/home/user/git/agentique/.agentique/worktrees/session-dark-mode",
    worktreeBranch: "session-dark-mode",
    worktreeMerged: true,
    gitVersion: 1,
    commitsAhead: 0,
    commitsBehind: 0,
    prUrl: "https://github.com/example/agentique/pull/42",
    turnCount: 1,
    totalCost: 0,
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
    autoApproveMode: "manual",
    behaviorPresets: DEFAULT_PRESETS,
    worktreePath: "/home/user/git/agentique/.agentique/worktrees/session-migration-bug",
    worktreeBranch: "session-migration-bug",
    gitVersion: 1,
    commitsAhead: 1,
    commitsBehind: 2,
    hasUncommitted: false,
    turnCount: 1,
    totalCost: 0,
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
    autoApproveMode: "manual",
    behaviorPresets: DEFAULT_PRESETS,
    gitVersion: 1,
    commitsAhead: 0,
    commitsBehind: 0,
    turnCount: 1,
    totalCost: 0,
    createdAt: daysAgo(5),
    updatedAt: daysAgo(4),
  },
  {
    id: S.queryOptimizer,
    projectId: P.agentique,
    name: "Optimize query planner",
    state: "idle",
    connected: true,
    model: "opus",
    permissionMode: "default",
    autoApproveMode: "auto",
    behaviorPresets: DEFAULT_PRESETS,
    worktreePath: "/home/user/git/agentique/.agentique/worktrees/session-query-optimizer",
    worktreeBranch: "session-query-optimizer",
    gitVersion: 1,
    commitsAhead: 5,
    commitsBehind: 0,
    hasUncommitted: false,
    turnCount: 3,
    totalCost: 0,
    createdAt: hoursAgo(4),
    updatedAt: hoursAgo(0.5),
  },
];

const THE_PINT_SESSIONS: SessionInfo[] = [
  {
    id: S.paymentFlow,
    projectId: P.thePint,
    name: "Payment flow redesign",
    state: "running",
    connected: true,
    model: "opus",
    permissionMode: "plan",
    autoApproveMode: "manual",
    behaviorPresets: DEFAULT_PRESETS,
    worktreePath: "/home/user/git/the-pint/.agentique/worktrees/session-payment-flow",
    worktreeBranch: "session-payment-flow",
    gitVersion: 1,
    commitsAhead: 0,
    commitsBehind: 0,
    hasUncommitted: false,
    turnCount: 1,
    totalCost: 0,
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
    autoApproveMode: "auto",
    behaviorPresets: DEFAULT_PRESETS,
    worktreePath: "/home/user/git/the-pint/.agentique/worktrees/session-image-gallery",
    worktreeBranch: "session-image-gallery",
    gitVersion: 1,
    commitsAhead: 2,
    commitsBehind: 0,
    hasUncommitted: false,
    turnCount: 1,
    totalCost: 0,
    createdAt: hoursAgo(4),
    updatedAt: hoursAgo(3),
  },
];

const ALLTIX_SESSIONS: SessionInfo[] = [
  {
    id: S.schedulerTests,
    projectId: P.alltixApi,
    name: "Scheduler test coverage",
    state: "idle",
    connected: true,
    model: "opus",
    permissionMode: "default",
    autoApproveMode: "manual",
    behaviorPresets: DEFAULT_PRESETS,
    gitVersion: 1,
    commitsAhead: 0,
    commitsBehind: 0,
    turnCount: 1,
    totalCost: 0,
    createdAt: daysAgo(1),
    updatedAt: hoursAgo(12),
  },
];

export const MOCK_SESSIONS: Record<string, SessionInfo[]> = {
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

function result(
  duration: number,
  ctx?: { contextWindow: number; inputTokens: number; outputTokens: number },
): WireEvent {
  return {
    type: "result",
    cost: 0,
    duration,
    usage: { inputTokens: Math.floor(duration * 2.5), outputTokens: Math.floor(duration * 0.8) },
    stopReason: "end_turn",
    ...(ctx && {
      contextWindow: ctx.contextWindow,
      inputTokens: ctx.inputTokens,
      outputTokens: ctx.outputTokens,
    }),
  };
}

function compactBoundary(trigger: "manual" | "auto", preTokens: number): WireEvent {
  return { type: "compact_boundary", trigger, preTokens };
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
        "I need to add test cases for expired, malformed, and revoked tokens to the integration test suite. Let me first understand the existing test structure and helpers.",
      ),
      toolUse(
        "Grep",
        { pattern: "TestMiddleware", path: "backend/internal/auth/" },
        "file_read",
        "mock-tool-ar-05a",
      ),
      toolResult(
        "mock-tool-ar-05a",
        "middleware_test.go:12: func TestMiddleware_ValidToken\nmiddleware_test.go:30: func TestMiddleware_MissingToken",
      ),
      toolUse(
        "Read",
        { file_path: "/home/user/git/agentique/backend/internal/auth/middleware_test.go" },
        "file_read",
        "mock-tool-ar-05b",
      ),
      toolResult(
        "mock-tool-ar-05b",
        'package auth_test\n\nimport (\n\t"net/http"\n\t"testing"\n)\n\nfunc TestMiddleware_ValidToken(t *testing.T) {\n\t// ... existing test\n}\n\nfunc TestMiddleware_MissingToken(t *testing.T) {\n\t// ... existing test\n}',
      ),
      toolUse(
        "Read",
        { file_path: "/home/user/git/agentique/backend/internal/auth/testhelpers_test.go" },
        "file_read",
        "mock-tool-ar-05c",
      ),
      toolResult(
        "mock-tool-ar-05c",
        "package auth_test\n\nfunc newTestServer(t *testing.T) *httptest.Server { ... }\nfunc issueToken(claims jwt.Claims) string { ... }",
      ),
      toolUse(
        "Read",
        { file_path: "/home/user/git/agentique/backend/internal/auth/jwt.go" },
        "file_read",
        "mock-tool-ar-05d",
      ),
      toolResult(
        "mock-tool-ar-05d",
        'package auth\n\nvar (\n\tErrExpired   = errors.New("token expired")\n\tErrMalformed = errors.New("malformed token")\n\tErrRevoked   = errors.New("revoked token")\n)',
      ),
      toolUse(
        "Glob",
        { pattern: "backend/internal/auth/**_test.go" },
        "file_read",
        "mock-tool-ar-05e",
      ),
      toolResult(
        "mock-tool-ar-05e",
        "backend/internal/auth/middleware_test.go\nbackend/internal/auth/jwt_test.go",
      ),
      toolUse(
        "Read",
        { file_path: "/home/user/git/agentique/backend/internal/auth/jwt_test.go" },
        "file_read",
        "mock-tool-ar-05f",
      ),
      toolResult(
        "mock-tool-ar-05f",
        "package auth_test\n\nfunc TestVerify_ValidToken(t *testing.T) { ... }",
      ),
      toolUse(
        "Edit",
        {
          file_path: "/home/user/git/agentique/backend/internal/auth/middleware_test.go",
          old_string: "// end",
          new_string: "func TestMiddleware_ExpiredToken...",
        },
        "file_write",
        "mock-tool-ar-06",
      ),
      toolResult("mock-tool-ar-06", "File edited successfully."),
      toolUse(
        "Edit",
        {
          file_path: "/home/user/git/agentique/backend/internal/auth/middleware_test.go",
          old_string: "func TestMiddleware_ExpiredToken...",
          new_string:
            "func TestMiddleware_ExpiredToken(t *testing.T) { ... }\nfunc TestMiddleware_MalformedToken(t *testing.T) { ... }",
        },
        "file_write",
        "mock-tool-ar-06b",
      ),
      toolResult("mock-tool-ar-06b", "File edited successfully."),
      toolUse(
        "Edit",
        {
          file_path: "/home/user/git/agentique/backend/internal/auth/testhelpers_test.go",
          old_string: "func issueToken",
          new_string: "func issueExpiredToken...",
        },
        "file_write",
        "mock-tool-ar-06c",
      ),
      toolResult("mock-tool-ar-06c", "File edited successfully."),
      toolUse(
        "Bash",
        {
          command: "cd backend && go test ./internal/auth/... -count=1",
          description: "Run auth tests",
        },
        "command",
        "mock-tool-ar-07",
      ),
      toolResult(
        "mock-tool-ar-07",
        "--- FAIL: TestMiddleware_RevokedToken (0.00s)\n    middleware_test.go:58: expected 401, got 400\nFAIL",
      ),
      toolUse(
        "Edit",
        {
          file_path: "/home/user/git/agentique/backend/internal/auth/middleware.go",
          old_string: "case errors.Is(err, jwt.ErrRevoked):",
          new_string:
            'case errors.Is(err, jwt.ErrRevoked):\n\t\t\t\thttp.Error(w, "revoked token", http.StatusUnauthorized)',
        },
        "file_write",
        "mock-tool-ar-07b",
      ),
      toolResult("mock-tool-ar-07b", "File edited successfully."),
      toolUse(
        "Bash",
        {
          command: "cd backend && go test ./internal/auth/... -v -count=1",
          description: "Run auth tests verbose",
        },
        "command",
        "mock-tool-ar-07c",
      ),
      toolResult(
        "mock-tool-ar-07c",
        "=== RUN   TestMiddleware_ValidToken\n--- PASS (0.01s)\n=== RUN   TestMiddleware_ExpiredToken\n--- PASS (0.01s)\n=== RUN   TestMiddleware_MalformedToken\n--- PASS (0.00s)\n=== RUN   TestMiddleware_RevokedToken\n--- PASS (0.01s)\nPASS\nok  \tagentique/internal/auth\t0.038s",
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

const queryOptimizerTurns: MockTurn[] = [
  {
    prompt:
      "Refactor the SQL query planner to use a cost-based optimizer instead of the current heuristic approach",
    events: [
      thinking(
        "This is a significant refactor. I need to understand the current query planner, the AST representation, and how queries flow through the system. Let me start by reading the existing code.",
      ),
      (() => {
        const id = "mock-tool-qo-01";
        return toolUse("Read", { file_path: "internal/planner/planner.go" }, "file_read", id);
      })(),
      toolResult(
        "mock-tool-qo-01",
        "package planner\n\n// Planner uses heuristic rules to choose join order and index selection.\ntype Planner struct {\n\ttables  map[string]*TableStats\n\tindexes map[string][]*Index\n}\n\nfunc (p *Planner) Plan(stmt *ast.SelectStmt) (*Plan, error) {\n\t// Current: always use left-deep join trees, pick first matching index\n\t...\n}",
      ),
      (() => {
        const id = "mock-tool-qo-02";
        return toolUse("Grep", { pattern: "func.*Plan\\(", path: "internal/" }, "file_read", id);
      })(),
      toolResult(
        "mock-tool-qo-02",
        "internal/planner/planner.go:45: func (p *Planner) Plan(stmt *ast.SelectStmt) (*Plan, error)\ninternal/planner/join.go:12: func (p *Planner) PlanJoin(tables []*Table) (*JoinPlan, error)\ninternal/planner/index.go:8: func (p *Planner) PlanIndexScan(t *Table, pred *ast.Predicate) (*IndexPlan, error)\ninternal/executor/executor.go:23: func (e *Executor) Execute(plan *Plan) (*Result, error)",
      ),
      (() => {
        const id = "mock-tool-qo-03";
        return toolUse("Read", { file_path: "internal/planner/join.go" }, "file_read", id);
      })(),
      toolResult(
        "mock-tool-qo-03",
        "package planner\n\n// PlanJoin picks join order using a fixed left-to-right strategy.\nfunc (p *Planner) PlanJoin(tables []*Table) (*JoinPlan, error) {\n\tvar plan JoinPlan\n\tfor i, t := range tables {\n\t\tif i == 0 {\n\t\t\tplan.Root = &ScanNode{Table: t}\n\t\t\tcontinue\n\t\t}\n\t\tplan.Root = &JoinNode{\n\t\t\tLeft:  plan.Root,\n\t\t\tRight: &ScanNode{Table: t},\n\t\t\tType:  NestedLoopJoin,\n\t\t}\n\t}\n\treturn &plan, nil\n}",
      ),
      text(
        "I see the current planner uses a naive left-to-right join ordering and always picks nested loop joins. I'll implement a cost-based optimizer using dynamic programming for join ordering (like System R) and a cost model based on table statistics.\n\nHere's my plan:\n1. Add a `CostModel` that estimates row counts and I/O costs\n2. Implement DP-based join enumeration\n3. Add index selection based on selectivity estimates\n4. Keep the heuristic path as a fallback for queries with >8 tables",
      ),
      (() => {
        const id = "mock-tool-qo-04";
        return toolUse(
          "Write",
          {
            file_path: "internal/planner/cost.go",
            content:
              "package planner\n\n// CostModel estimates the cost of query plan operators.\ntype CostModel struct {\n\tstats map[string]*TableStats\n}\n\nfunc (c *CostModel) ScanCost(t *Table) float64 { ... }\nfunc (c *CostModel) JoinCost(left, right PlanNode, jtype JoinType) float64 { ... }\nfunc (c *CostModel) IndexScanCost(t *Table, idx *Index, selectivity float64) float64 { ... }",
          },
          "file_write",
          id,
        );
      })(),
      toolResult("mock-tool-qo-04", "File written successfully."),
      (() => {
        const id = "mock-tool-qo-05";
        return toolUse(
          "Write",
          {
            file_path: "internal/planner/dp_join.go",
            content:
              "package planner\n\n// DPJoinEnumerator uses dynamic programming to find optimal join orders.\n// Based on the System R algorithm with pruning for large join graphs.\ntype DPJoinEnumerator struct {\n\tcost  *CostModel\n\tmemo  map[tableSet]*dpEntry\n}\n\nfunc (d *DPJoinEnumerator) Enumerate(tables []*Table, preds []*JoinPred) (*JoinPlan, error) { ... }",
          },
          "file_write",
          id,
        );
      })(),
      toolResult("mock-tool-qo-05", "File written successfully."),
      result(65000, { contextWindow: 200000, inputTokens: 142000, outputTokens: 28000 }),
    ],
  },
  {
    prompt: "Continue — implement the cost estimation and wire it into the existing executor",
    events: [
      thinking(
        "Now I need to implement the actual cost estimation logic, update the planner to use it, and make sure the executor can handle the new plan types. This involves touching several files.",
      ),
      (() => {
        const id = "mock-tool-qo-06";
        return toolUse("Read", { file_path: "internal/planner/stats.go" }, "file_read", id);
      })(),
      toolResult(
        "mock-tool-qo-06",
        "package planner\n\ntype TableStats struct {\n\tRowCount    int64\n\tAvgRowSize  int\n\tDistinct    map[string]int64  // column -> distinct value count\n\tHistograms  map[string]*Histogram\n}\n\ntype Histogram struct {\n\tBuckets []Bucket\n}\n\ntype Bucket struct {\n\tLower, Upper interface{}\n\tCount        int64\n\tDistinct     int64\n}",
      ),
      (() => {
        const id = "mock-tool-qo-07";
        return toolUse(
          "Edit",
          {
            file_path: "internal/planner/cost.go",
            old_string: "func (c *CostModel) ScanCost(t *Table) float64 { ... }",
            new_string:
              "func (c *CostModel) ScanCost(t *Table) float64 {\n\ts := c.stats[t.Name]\n\tif s == nil { return 1000 }\n\treturn float64(s.RowCount) * float64(s.AvgRowSize) / pageSize\n}",
          },
          "file_write",
          id,
        );
      })(),
      toolResult("mock-tool-qo-07", "File edited successfully."),
      (() => {
        const id = "mock-tool-qo-08";
        return toolUse(
          "Edit",
          {
            file_path: "internal/planner/planner.go",
            old_string: "// Current: always use left-deep join trees",
            new_string: "// Use cost-based optimizer with DP join enumeration",
          },
          "file_write",
          id,
        );
      })(),
      toolResult("mock-tool-qo-08", "File edited successfully."),
      (() => {
        const id = "mock-tool-qo-09";
        return toolUse(
          "Bash",
          {
            command: "cd /home/user/git/agentique && go test ./internal/planner/... -v -count=1",
            description: "Run planner tests",
          },
          "command",
          id,
        );
      })(),
      toolResult(
        "mock-tool-qo-09",
        "=== RUN   TestCostModelScan\n--- PASS: TestCostModelScan (0.00s)\n=== RUN   TestDPJoinTwoTables\n--- PASS: TestDPJoinTwoTables (0.00s)\n=== RUN   TestDPJoinThreeTables\n--- PASS: TestDPJoinThreeTables (0.01s)\n=== RUN   TestPlannerEndToEnd\n--- PASS: TestPlannerEndToEnd (0.02s)\nok  \tagentique/internal/planner\t0.089s",
      ),
      text(
        "All planner tests pass. The cost-based optimizer is wired in and producing better join orders for the test queries. The 3-table join test now correctly chooses hash join for the large table pair instead of nested loop.",
      ),
      result(78000, { contextWindow: 200000, inputTokens: 185000, outputTokens: 7000 }),
      compactBoundary("auto", 192000),
    ],
  },
  {
    prompt: "Add benchmarks comparing the old heuristic planner vs the new cost-based optimizer",
    events: [
      thinking("Context was compacted. I'll add benchmarks to quantify the improvement."),
      (() => {
        const id = "mock-tool-qo-10";
        return toolUse(
          "Write",
          {
            file_path: "internal/planner/bench_test.go",
            content:
              'package planner_test\n\nimport "testing"\n\nfunc BenchmarkHeuristicPlanner(b *testing.B) {\n\tfor i := 0; i < b.N; i++ {\n\t\tp := NewHeuristicPlanner(testStats)\n\t\tp.Plan(fiveTableJoin)\n\t}\n}\n\nfunc BenchmarkCostBasedPlanner(b *testing.B) {\n\tfor i := 0; i < b.N; i++ {\n\t\tp := NewCostPlanner(testStats)\n\t\tp.Plan(fiveTableJoin)\n\t}\n}',
          },
          "file_write",
          id,
        );
      })(),
      toolResult("mock-tool-qo-10", "File written successfully."),
      (() => {
        const id = "mock-tool-qo-11";
        return toolUse(
          "Bash",
          {
            command:
              "cd /home/user/git/agentique && go test ./internal/planner/... -bench=. -benchmem -count=3",
            description: "Run planner benchmarks",
          },
          "command",
          id,
        );
      })(),
      toolResult(
        "mock-tool-qo-11",
        "goos: linux\ngoarch: amd64\npkg: agentique/internal/planner\nBenchmarkHeuristicPlanner-16    \t  125834\t      9487 ns/op\t    4096 B/op\t      52 allocs/op\nBenchmarkCostBasedPlanner-16    \t   89245\t     13421 ns/op\t    8192 B/op\t      78 allocs/op\nPASS\nok  \tagentique/internal/planner\t4.231s",
      ),
      text(
        "Benchmarks show the cost-based planner is ~40% slower in planning time (13.4us vs 9.5us) but this is negligible compared to query execution time. The real win is in execution — the cost-based planner picks significantly better join orders:\n\n| Query | Heuristic | Cost-based | Speedup |\n|-------|-----------|------------|--------|\n| 3-table join | 45ms | 12ms | 3.7x |\n| 5-table join | 890ms | 95ms | 9.4x |\n| Star schema | 2.1s | 180ms | 11.7x |\n\nThe DP enumeration adds memory overhead (8KB vs 4KB) but this is trivial. For queries with >8 tables, we fall back to the heuristic to keep planning time bounded.",
      ),
      result(42000, { contextWindow: 200000, inputTokens: 58000, outputTokens: 12000 }),
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
  [S.queryOptimizer]: queryOptimizerTurns,
};
