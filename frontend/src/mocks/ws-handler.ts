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
  TrackedFilesResultSchema,
  UncommittedFilesResultSchema,
  WireEventSchema,
} from "~/lib/generated-schemas";
import {
  MOCK_CHANNEL_TIMELINES,
  MOCK_CHANNELS,
  MOCK_PENDING_APPROVALS,
  MOCK_PENDING_QUESTIONS,
  MOCK_PROJECT_GIT_STATUS,
  MOCK_PROJECTS,
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

    // Push result events to trigger hasUnseenCompletion for sessions the user hasn't viewed
    const simulateUnseen =
      (session.state === "done" && session.completedAt) ||
      session.id === SESSION_IDS.imageGallery ||
      session.id === SESSION_IDS.schedulerTests;
    if (simulateUnseen) {
      setTimeout(() => {
        push(client, "session.event", {
          sessionId: session.id,
          event: {
            type: "result",
            cost: 0,
            duration: 1200,
            usage: { inputTokens: 3000, outputTokens: 800 },
            stopReason: "end_turn",
          },
        });
      }, 700);
    }
  }

  // --- Cycling todo loop for demo ---
  scheduleTodoCycle(client);
}

const TODO_CYCLE_ITEMS = [
  "Scaffold component structure",
  "Add route definitions",
  "Implement data fetching layer",
  "Build list view with filters",
  "Add detail panel",
  "Wire up keyboard shortcuts",
  "Write unit tests",
];

const TODO_CYCLE_SESSION = SESSION_IDS.sprintBoard;
const TODO_CYCLE_INTERVAL = 2000;
const TODO_CYCLE_PAUSE_AT_COMPLETE = 3000;

function scheduleTodoCycle(client: WsClientConnection) {
  let completed = 0;

  function tick() {
    if (completed < TODO_CYCLE_ITEMS.length) {
      completed++;
    } else {
      // Reset — all back to pending
      completed = 0;
    }

    const todos = TODO_CYCLE_ITEMS.map((content, i) => ({
      content,
      status: i < completed ? ("completed" as const) : ("pending" as const),
    }));

    push(client, "session.event", {
      sessionId: TODO_CYCLE_SESSION,
      event: {
        type: "tool_use" as const,
        toolName: "TodoWrite",
        toolInput: { todos },
        toolUseId: `mock-todo-cycle-${Date.now()}`,
      },
    });

    const delay =
      completed === TODO_CYCLE_ITEMS.length ? TODO_CYCLE_PAUSE_AT_COMPLETE : TODO_CYCLE_INTERVAL;
    setTimeout(tick, delay);
  }

  // Start after initial data has loaded
  setTimeout(tick, 1500);
}

// --- Rich mock diff builders ---

function buildCommittedDiff(sessionId: string) {
  if (sessionId === SESSION_IDS.authRefactor) {
    return RICH_COMMITTED_DIFF;
  }
  // Default sparse diff for other sessions
  return {
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
}

function buildUncommittedDiff(sessionId: string) {
  if (sessionId === SESSION_IDS.authRefactor) {
    return RICH_UNCOMMITTED_DIFF;
  }
  return {
    hasDiff: true,
    summary: "2 files changed, 15 insertions(+), 3 deletions(-)",
    files: [
      { path: "internal/auth/middleware.go", insertions: 12, deletions: 3, status: "modified" },
      { path: "internal/auth/middleware_test.go", insertions: 3, deletions: 0, status: "added" },
    ],
    diff: "",
    truncated: false,
  };
}

const RICH_COMMITTED_DIFF = {
  hasDiff: true,
  summary: "18 files changed, 847 insertions(+), 234 deletions(-)",
  files: [
    // Modified — core changes
    {
      path: "backend/internal/auth/middleware.go",
      insertions: 89,
      deletions: 42,
      status: "modified",
    },
    {
      path: "backend/internal/auth/jwt/token_validator.go",
      insertions: 156,
      deletions: 78,
      status: "modified",
    },
    {
      path: "backend/internal/auth/jwt/claims.go",
      insertions: 34,
      deletions: 12,
      status: "modified",
    },
    { path: "backend/internal/server/routes.go", insertions: 8, deletions: 3, status: "modified" },
    // Long path — deeply nested
    {
      path: "backend/internal/auth/providers/oauth2/google/callback_handler.go",
      insertions: 67,
      deletions: 31,
      status: "modified",
    },
    {
      path: "backend/internal/auth/providers/oauth2/github/callback_handler.go",
      insertions: 45,
      deletions: 28,
      status: "modified",
    },
    // Added files
    {
      path: "backend/internal/auth/jwt/token_validator_test.go",
      insertions: 210,
      deletions: 0,
      status: "added",
    },
    {
      path: "backend/internal/auth/middleware_test.go",
      insertions: 145,
      deletions: 0,
      status: "added",
    },
    { path: "backend/internal/auth/errors.go", insertions: 38, deletions: 0, status: "added" },
    { path: "docs/auth-migration-guide.md", insertions: 24, deletions: 0, status: "added" },
    // Deleted files
    {
      path: "backend/internal/auth/legacy_validator.go",
      insertions: 0,
      deletions: 89,
      status: "deleted",
    },
    {
      path: "backend/internal/auth/legacy_validator_test.go",
      insertions: 0,
      deletions: 67,
      status: "deleted",
    },
    // Renamed
    {
      path: "backend/internal/auth/jwt/verify.go",
      insertions: 12,
      deletions: 8,
      status: "renamed",
    },
    // Config / small edits
    { path: "backend/go.mod", insertions: 2, deletions: 1, status: "modified" },
    { path: "backend/go.sum", insertions: 14, deletions: 7, status: "modified" },
    // Frontend touch (cross-stack change)
    {
      path: "frontend/src/lib/api/auth-client.ts",
      insertions: 18,
      deletions: 5,
      status: "modified",
    },
    { path: "frontend/src/hooks/useAuth.ts", insertions: 6, deletions: 2, status: "modified" },
    // Migration
    {
      path: "backend/db/migrations/015_add_token_revocation_table.sql",
      insertions: 22,
      deletions: 0,
      status: "added",
    },
  ],
  diff: `diff --git a/backend/internal/auth/middleware.go b/backend/internal/auth/middleware.go
index a1b2c3d..e4f5g6h 100644
--- a/backend/internal/auth/middleware.go
+++ b/backend/internal/auth/middleware.go
@@ -1,12 +1,15 @@
 package auth

 import (
+\t"errors"
 \t"net/http"
 \t"strings"

-\t"github.com/example/agentique/internal/auth/legacy"
+\t"github.com/example/agentique/internal/auth/jwt"
 )

+var ErrNoToken = errors.New("no authorization token provided")
+
 // Middleware validates the JWT token in the Authorization header.
 func Middleware(validator jwt.Validator) func(http.Handler) http.Handler {
 \treturn func(next http.Handler) http.Handler {
@@ -15,18 +18,32 @@ func Middleware(validator jwt.Validator) func(http.Handler) http.Handler {
 \t\t\t\ttoken := extractBearerToken(r)
 \t\t\t\tif token == "" {
-\t\t\t\t\thttp.Error(w, "unauthorized", http.StatusUnauthorized)
+\t\t\t\t\twriteError(w, ErrNoToken, http.StatusUnauthorized)
 \t\t\t\t\treturn
 \t\t\t\t}

-\t\t\t\tclaims, err := legacy.ValidateToken(token)
+\t\t\t\tclaims, err := validator.Verify(r.Context(), token)
 \t\t\t\tif err != nil {
-\t\t\t\t\thttp.Error(w, "invalid token", http.StatusUnauthorized)
+\t\t\t\t\tswitch {
+\t\t\t\t\tcase errors.Is(err, jwt.ErrTokenExpired):
+\t\t\t\t\t\twriteError(w, err, http.StatusUnauthorized)
+\t\t\t\t\tcase errors.Is(err, jwt.ErrTokenMalformed):
+\t\t\t\t\t\twriteError(w, err, http.StatusBadRequest)
+\t\t\t\t\tcase errors.Is(err, jwt.ErrTokenRevoked):
+\t\t\t\t\t\twriteError(w, err, http.StatusForbidden)
+\t\t\t\t\tdefault:
+\t\t\t\t\t\twriteError(w, err, http.StatusUnauthorized)
+\t\t\t\t\t}
 \t\t\t\t\treturn
 \t\t\t\t}

 \t\t\t\tctx := WithClaims(r.Context(), claims)
 \t\t\t\tnext.ServeHTTP(w, r.WithContext(ctx))
 \t\t\t}))
 \t}
 }
+
+func writeError(w http.ResponseWriter, err error, status int) {
+\thttp.Error(w, err.Error(), status)
+}
diff --git a/backend/internal/auth/jwt/token_validator.go b/backend/internal/auth/jwt/token_validator.go
index 1234567..abcdefg 100644
--- a/backend/internal/auth/jwt/token_validator.go
+++ b/backend/internal/auth/jwt/token_validator.go
@@ -1,6 +1,8 @@
 package jwt

 import (
+\t"context"
+\t"crypto/rsa"
 \t"fmt"
 \t"time"

@@ -12,18 +14,42 @@ type Validator interface {
-\tValidate(token string) (*Claims, error)
+\tVerify(ctx context.Context, token string) (*Claims, error)
 }

-type validator struct {
-\tsecret []byte
+type rsaValidator struct {
+\tpublicKey *rsa.PublicKey
+\tissuer    string
+\taudience  string
 }

-func NewValidator(secret []byte) Validator {
-\treturn &validator{secret: secret}
+func NewValidator(publicKey *rsa.PublicKey, issuer, audience string) Validator {
+\treturn &rsaValidator{
+\t\tpublicKey: publicKey,
+\t\tissuer:    issuer,
+\t\taudience:  audience,
+\t}
 }

-func (v *validator) Validate(token string) (*Claims, error) {
-\tparsed, err := golangJwt.Parse(token, func(t *golangJwt.Token) (interface{}, error) {
-\t\treturn v.secret, nil
+func (v *rsaValidator) Verify(ctx context.Context, token string) (*Claims, error) {
+\tparsed, err := golangJwt.Parse(token, func(t *golangJwt.Token) (any, error) {
+\t\tif _, ok := t.Method.(*golangJwt.SigningMethodRSA); !ok {
+\t\t\treturn nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
+\t\t}
+\t\treturn v.publicKey, nil
 \t})
 \tif err != nil {
-\t\treturn nil, fmt.Errorf("parse token: %w", err)
+\t\treturn nil, classifyError(err)
+\t}
+
+\tclaims, ok := parsed.Claims.(golangJwt.MapClaims)
+\tif !ok || !parsed.Valid {
+\t\treturn nil, ErrTokenMalformed
+\t}
+
+\tif !claims.VerifyIssuer(v.issuer, true) {
+\t\treturn nil, fmt.Errorf("invalid issuer: expected %s", v.issuer)
+\t}
+\tif !claims.VerifyAudience(v.audience, true) {
+\t\treturn nil, fmt.Errorf("invalid audience: expected %s", v.audience)
 \t}

 \treturn extractClaims(parsed)
diff --git a/backend/internal/auth/jwt/token_validator_test.go b/backend/internal/auth/jwt/token_validator_test.go
new file mode 100644
index 0000000..1234567
--- /dev/null
+++ b/backend/internal/auth/jwt/token_validator_test.go
@@ -0,0 +1,48 @@
+package jwt_test
+
+import (
+\t"context"
+\t"crypto/rand"
+\t"crypto/rsa"
+\t"testing"
+\t"time"
+
+\tgolangJwt "github.com/golang-jwt/jwt/v5"
+)
+
+func TestVerify_ValidToken(t *testing.T) {
+\tkey, _ := rsa.GenerateKey(rand.Reader, 2048)
+\tv := NewValidator(&key.PublicKey, "test-issuer", "test-audience")
+
+\ttoken := generateTestToken(t, key, time.Hour)
+\tclaims, err := v.Verify(context.Background(), token)
+\tif err != nil {
+\t\tt.Fatalf("unexpected error: %v", err)
+\t}
+\tif claims.Subject == "" {
+\t\tt.Error("expected non-empty subject")
+\t}
+}
+
+func TestVerify_ExpiredToken(t *testing.T) {
+\tkey, _ := rsa.GenerateKey(rand.Reader, 2048)
+\tv := NewValidator(&key.PublicKey, "test-issuer", "test-audience")
+
+\ttoken := generateTestToken(t, key, -time.Hour)
+\t_, err := v.Verify(context.Background(), token)
+\tif err == nil {
+\t\tt.Fatal("expected error for expired token")
+\t}
+}
+
+func TestVerify_MalformedToken(t *testing.T) {
+\tkey, _ := rsa.GenerateKey(rand.Reader, 2048)
+\tv := NewValidator(&key.PublicKey, "test-issuer", "test-audience")
+
+\t_, err := v.Verify(context.Background(), "not.a.valid.token")
+\tif err == nil {
+\t\tt.Fatal("expected error for malformed token")
+\t}
+}
diff --git a/backend/internal/auth/errors.go b/backend/internal/auth/errors.go
new file mode 100644
index 0000000..2345678
--- /dev/null
+++ b/backend/internal/auth/errors.go
@@ -0,0 +1,12 @@
+package auth
+
+import "errors"
+
+var (
+\tErrTokenExpired   = errors.New("token expired")
+\tErrTokenMalformed = errors.New("token malformed")
+\tErrTokenRevoked   = errors.New("token revoked")
+\tErrNoToken        = errors.New("no authorization token")
+)
diff --git a/backend/internal/auth/legacy_validator.go b/backend/internal/auth/legacy_validator.go
deleted file mode 100644
index 9876543..0000000
--- a/backend/internal/auth/legacy_validator.go
+++ /dev/null
@@ -1,45 +0,0 @@
-package auth
-
-import (
-\t"fmt"
-\t"time"
-)
-
-// ValidateToken validates the given JWT token string.
-// Deprecated: Use jwt.Validator.Verify instead.
-func ValidateToken(token string) (*Claims, error) {
-\tif token == "" {
-\t\treturn nil, fmt.Errorf("empty token")
-\t}
-\t// Legacy HMAC validation — no context, no revocation check
-\tparsed, err := parseHMAC(token)
-\tif err != nil {
-\t\treturn nil, err
-\t}
-\tif parsed.ExpiresAt.Before(time.Now()) {
-\t\treturn nil, fmt.Errorf("token expired")
-\t}
-\treturn parsed, nil
-}
diff --git a/backend/internal/auth/providers/oauth2/google/callback_handler.go b/backend/internal/auth/providers/oauth2/google/callback_handler.go
index 5555555..6666666 100644
--- a/backend/internal/auth/providers/oauth2/google/callback_handler.go
+++ b/backend/internal/auth/providers/oauth2/google/callback_handler.go
@@ -23,9 +23,14 @@ func (h *CallbackHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
 \tcode := r.URL.Query().Get("code")
 \tif code == "" {
-\t\thttp.Error(w, "missing code", http.StatusBadRequest)
+\t\twriteJSONError(w, "missing authorization code", http.StatusBadRequest)
 \t\treturn
 \t}

-\ttoken, err := h.exchange(code)
+\ttoken, err := h.exchange(r.Context(), code)
 \tif err != nil {
-\t\thttp.Error(w, "exchange failed", http.StatusInternalServerError)
+\t\tlog.Printf("oauth2 exchange failed: %v", err)
+\t\twriteJSONError(w, "authentication failed", http.StatusInternalServerError)
 \t\treturn
 \t}
+
+\tclaims, err := h.validator.Verify(r.Context(), token.AccessToken)
+\tif err != nil {
+\t\twriteJSONError(w, "token verification failed", http.StatusUnauthorized)
+\t\treturn
+\t}
diff --git a/frontend/src/lib/api/auth-client.ts b/frontend/src/lib/api/auth-client.ts
index 7777777..8888888 100644
--- a/frontend/src/lib/api/auth-client.ts
+++ b/frontend/src/lib/api/auth-client.ts
@@ -8,7 +8,12 @@ export async function refreshToken(): Promise<string> {
-  const res = await fetch("/api/auth/refresh", { method: "POST" });
+  const res = await fetch("/api/auth/refresh", {
+    method: "POST",
+    headers: {
+      "Content-Type": "application/json",
+    },
+    credentials: "same-origin",
+  });
   if (!res.ok) {
-    throw new Error("refresh failed");
+    const body = await res.json().catch(() => ({}));
+    throw new AuthError(body.message ?? "token refresh failed", res.status);
   }
diff --git a/backend/db/migrations/015_add_token_revocation_table.sql b/backend/db/migrations/015_add_token_revocation_table.sql
new file mode 100644
index 0000000..3456789
--- /dev/null
+++ b/backend/db/migrations/015_add_token_revocation_table.sql
@@ -0,0 +1,12 @@
+-- +goose Up
+CREATE TABLE token_revocations (
+    id INTEGER PRIMARY KEY AUTOINCREMENT,
+    jti TEXT NOT NULL UNIQUE,
+    revoked_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
+    reason TEXT NOT NULL DEFAULT '',
+    expires_at TIMESTAMP NOT NULL
+);
+CREATE INDEX idx_token_revocations_jti ON token_revocations(jti);
+
+-- +goose Down
+DROP TABLE IF EXISTS token_revocations;`,
  truncated: false,
};

const RICH_UNCOMMITTED_DIFF = {
  hasDiff: true,
  summary: "4 files changed, 52 insertions(+), 8 deletions(-)",
  files: [
    {
      path: "backend/internal/auth/middleware.go",
      insertions: 18,
      deletions: 5,
      status: "modified",
    },
    {
      path: "backend/internal/auth/jwt/claims.go",
      insertions: 22,
      deletions: 3,
      status: "modified",
    },
    {
      path: "backend/internal/auth/jwt/revocation_store.go",
      insertions: 45,
      deletions: 0,
      status: "added",
    },
    {
      path: "backend/internal/config/auth_config.go",
      insertions: 12,
      deletions: 0,
      status: "added",
    },
  ],
  diff: `diff --git a/backend/internal/auth/middleware.go b/backend/internal/auth/middleware.go
index e4f5g6h..9a8b7c6 100644
--- a/backend/internal/auth/middleware.go
+++ b/backend/internal/auth/middleware.go
@@ -18,6 +18,7 @@ func Middleware(validator jwt.Validator) func(http.Handler) http.Handler {
 \t\t\t\tclaims, err := validator.Verify(r.Context(), token)
 \t\t\t\tif err != nil {
 \t\t\t\t\tswitch {
+\t\t\t\t\t// TODO: add rate limiting for repeated auth failures
 \t\t\t\t\tcase errors.Is(err, jwt.ErrTokenExpired):
 \t\t\t\t\t\twriteError(w, err, http.StatusUnauthorized)
 \t\t\t\t\tcase errors.Is(err, jwt.ErrTokenMalformed):
@@ -42,6 +43,18 @@ func writeError(w http.ResponseWriter, err error, status int) {
 \thttp.Error(w, err.Error(), status)
 }
+
+// LogAuthFailure records authentication failures for monitoring.
+func LogAuthFailure(r *http.Request, err error) {
+\tip := r.RemoteAddr
+\tif forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
+\t\tip = forwarded
+\t}
+\tlog.Printf("auth failure: ip=%s path=%s err=%v", ip, r.URL.Path, err)
+}
diff --git a/backend/internal/auth/jwt/claims.go b/backend/internal/auth/jwt/claims.go
index abcdef1..2345678 100644
--- a/backend/internal/auth/jwt/claims.go
+++ b/backend/internal/auth/jwt/claims.go
@@ -8,9 +8,14 @@ type Claims struct {
 \tSubject   string
 \tEmail     string
 \tRoles     []string
-\tExpiresAt time.Time
+\tIssuedAt  time.Time
+\tExpiresAt time.Time
+\tJTI       string
 }
+
+func (c *Claims) IsExpired() bool {
+\treturn time.Now().After(c.ExpiresAt)
+}
diff --git a/backend/internal/auth/jwt/revocation_store.go b/backend/internal/auth/jwt/revocation_store.go
new file mode 100644
index 0000000..abcde12
--- /dev/null
+++ b/backend/internal/auth/jwt/revocation_store.go
@@ -0,0 +1,32 @@
+package jwt
+
+import (
+\t"context"
+\t"database/sql"
+\t"time"
+)
+
+type RevocationStore struct {
+\tdb *sql.DB
+}
+
+func NewRevocationStore(db *sql.DB) *RevocationStore {
+\treturn &RevocationStore{db: db}
+}
+
+func (s *RevocationStore) IsRevoked(ctx context.Context, jti string) (bool, error) {
+\tvar count int
+\terr := s.db.QueryRowContext(ctx,
+\t\t"SELECT COUNT(*) FROM token_revocations WHERE jti = ?", jti,
+\t).Scan(&count)
+\tif err != nil {
+\t\treturn false, err
+\t}
+\treturn count > 0, nil
+}
+
+func (s *RevocationStore) Revoke(ctx context.Context, jti, reason string, expiresAt time.Time) error {
+\t_, err := s.db.ExecContext(ctx,
+\t\t"INSERT INTO token_revocations (jti, reason, expires_at) VALUES (?, ?, ?)",
+\t\tjti, reason, expiresAt,
+\t)
+\treturn err
+}
diff --git a/backend/internal/config/auth_config.go b/backend/internal/config/auth_config.go
new file mode 100644
index 0000000..fedcba9
--- /dev/null
+++ b/backend/internal/config/auth_config.go
@@ -0,0 +1,12 @@
+package config
+
+type AuthConfig struct {
+\tPublicKeyPath string
+\tIssuer        string
+\tAudience      string
+}`,
  truncated: false,
};

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
      const allTurns = MOCK_TURNS[p.sessionId as string] ?? [];
      const limit = (p.limit as number) || 0;
      const turns = limit > 0 ? allTurns.slice(-limit) : allTurns;
      const payload = {
        turns,
        hasMore: limit > 0 && allTurns.length > turns.length,
        totalTurns: allTurns.length,
      };
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
        autoApproveMode: (p.autoApproveMode as string) ?? "manual",
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
      const diffPayload = buildCommittedDiff(p.sessionId as string);
      respond(
        client,
        msg.id,
        validatePayload(DiffResultSchema, diffPayload, "session.diff response"),
      );
      break;
    }

    case "session.commit-log": {
      const now = Date.now();
      const hoursAgo = (h: number) => new Date(now - h * 3600000).toISOString();
      const commits =
        (p.sessionId as string) === SESSION_IDS.authRefactor
          ? [
              {
                hash: "a1b2c3d",
                message: "refactor: migrate auth middleware to jwt.Verify API",
                body: "Replace deprecated ValidateToken with jwt.Verify, add structured\nerror handling for expired/malformed tokens, update integration tests.\n\nBreaking change: ValidateToken is removed. All callers must migrate\nto the Validator interface.",
                timestamp: hoursAgo(1),
              },
              {
                hash: "e4f5g6h",
                message: "feat: add token revocation store and migration",
                body: "Adds a SQLite-backed revocation store that checks JTI claims\nagainst a revocation table. Includes goose migration 015.",
                timestamp: hoursAgo(2.5),
              },
              {
                hash: "i7j8k9l",
                message: "test: add jwt validator unit tests",
                timestamp: hoursAgo(4),
              },
            ]
          : [
              {
                hash: "abc1234",
                message: "fix: handle edge case in reconnect logic",
                timestamp: hoursAgo(0.5),
              },
            ];
      respond(client, msg.id, { commits });
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
      const files =
        (p.sessionId as string) === SESSION_IDS.authRefactor
          ? [
              { path: "backend/internal/auth/middleware.go", status: "modified" },
              { path: "backend/internal/auth/jwt/claims.go", status: "modified" },
              { path: "backend/internal/auth/jwt/revocation_store.go", status: "added" },
              { path: "backend/internal/config/auth_config.go", status: "added" },
            ]
          : [
              { path: "backend/internal/auth/middleware.go", status: "modified" },
              { path: "backend/internal/auth/middleware_test.go", status: "modified" },
            ];
      const filesPayload = { files };
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
      const mode = p.mode as string;
      const mergeResult = { status: "merged", commitHash: "def5678" };
      validatePayload(MergeResultSchema, mergeResult, "session.merge response");
      respond(client, msg.id, mergeResult);
      const state = mode === "delete" ? "stopped" : mode === "complete" ? "done" : "idle";
      push(client, "session.state", {
        sessionId: p.sessionId,
        state,
        connected: state === "idle",
        hasDirtyWorktree: false,
        hasUncommitted: false,
        worktreeMerged: mode !== "merge",
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

    case "project.reorder": {
      const ids = (p.projectIds as string[]) ?? [];
      for (let i = 0; i < ids.length; i++) {
        const proj = MOCK_PROJECTS.find((pr) => pr.id === ids[i]);
        if (proj) proj.sort_order = i + 1;
      }
      respond(client, msg.id);
      break;
    }

    case "session.set-model":
    case "session.set-permission":
    case "session.set-auto-approve":
    case "session.set-icon":
    case "session.resolve-approval":
    case "session.resolve-question":
    case "session.interrupt":
    case "project.fetch":
    case "project.push":
      respond(client, msg.id);
      break;

    case "channel.list": {
      const channels = MOCK_CHANNELS[p.projectId as string] ?? [];
      respond(client, msg.id, channels);
      break;
    }

    case "channel.info": {
      const allChannels = Object.values(MOCK_CHANNELS).flat();
      const channel = allChannels.find((c) => c.id === p.channelId);
      if (channel) respond(client, msg.id, channel);
      else respondError(client, msg.id, "channel not found");
      break;
    }

    case "channel.timeline": {
      const events = MOCK_CHANNEL_TIMELINES[p.channelId as string] ?? [];
      respond(client, msg.id, events);
      break;
    }

    case "channel.create":
    case "channel.delete":
    case "channel.dissolve":
    case "channel.join":
    case "channel.leave":
    case "channel.send-message":
    case "channel.broadcast":
    case "channel.create-swarm":
      respond(client, msg.id);
      break;

    case "session.uncommitted-diff": {
      const uncommittedPayload = buildUncommittedDiff(p.sessionId as string);
      respond(client, msg.id, uncommittedPayload);
      break;
    }

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
export { PROJECT_IDS, SESSION_IDS, wsLink };
