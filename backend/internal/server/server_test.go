package server_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	dbpkg "github.com/mdjarv/agentique/backend/db"
	"github.com/mdjarv/agentique/backend/internal/httperror"
	"github.com/mdjarv/agentique/backend/internal/machine"
	"github.com/mdjarv/agentique/backend/internal/server"
	"github.com/mdjarv/agentique/backend/internal/store"
)

func setupAuthenticatedTestServer(t *testing.T) (*httptest.Server, *store.Queries, func()) {
	t.Helper()

	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := store.RunMigrations(db, dbpkg.Migrations); err != nil {
		db.Close()
		t.Fatalf("run migrations: %v", err)
	}

	queries := store.New(db)
	srv, err := server.New(queries, server.Config{
		AuthEnabled: true,
		RPID:        "localhost",
		RPOrigins:   []string{"http://trusted.example"},
		DB:          db,
	})
	if err != nil {
		db.Close()
		t.Fatalf("create server: %v", err)
	}
	ts := httptest.NewServer(srv)
	return ts, queries, func() {
		srv.Shutdown()
		ts.Close()
		db.Close()
	}
}

func createCookieSession(t *testing.T, queries *store.Queries, admin bool) string {
	t.Helper()
	userID := "00000000-0000-4000-8000-000000000001"
	isAdmin := int64(0)
	if admin {
		isAdmin = 1
	}
	if _, err := queries.CreateUser(context.Background(), store.CreateUserParams{
		ID: userID, DisplayName: "operator", IsAdmin: isAdmin,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	token := "test-cookie-token"
	if err := queries.CreateAuthSession(context.Background(), store.CreateAuthSessionParams{
		Token:     token,
		ID:        sql.NullString{String: "test-session-id", Valid: true},
		UserID:    userID,
		ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		Kind:      "cookie",
	}); err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	return token
}

func setupTestServer(t *testing.T) (*httptest.Server, func()) {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	if err := store.RunMigrations(db, dbpkg.Migrations); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	queries := store.New(db)
	srv, err := server.New(queries, server.Config{DB: db})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	ts := httptest.NewServer(srv)

	cleanup := func() {
		ts.Close()
		db.Close()
	}

	return ts, cleanup
}

func TestHealthCheck(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/health")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", body["status"])
	}
}

func TestCreateProject(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	// Use the temp directory from t.TempDir() as a valid path on disk.
	validPath := t.TempDir()

	payload := `{"name":"test","path":"` + strings.ReplaceAll(validPath, `\`, `\\`) + `"}`
	resp, err := http.Post(ts.URL+"/api/projects", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 201, got %d: %s", resp.StatusCode, body)
	}

	var project map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&project); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if project["id"] == nil || project["id"] == "" {
		t.Fatal("expected project to have an id")
	}
	if project["name"] != "test" {
		t.Fatalf("expected name 'test', got %q", project["name"])
	}
	if project["path"] != validPath {
		t.Fatalf("expected path %q, got %q", validPath, project["path"])
	}
}

func TestCrossOriginCookieMutationIsRejectedBeforeHandler(t *testing.T) {
	ts, queries, cleanup := setupAuthenticatedTestServer(t)
	defer cleanup()

	token := createCookieSession(t, queries, true)
	projectPath := t.TempDir()
	payload := `{"name":"cross-origin","path":"` + strings.ReplaceAll(projectPath, `\`, `\\`) + `"}`
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/projects", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Origin", "http://evil.example")
	req.Header.Set("Content-Type", "text/plain")
	req.AddCookie(&http.Cookie{Name: "agentique_session", Value: token})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 403: %s", resp.StatusCode, body)
	}

	projects, err := queries.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	if len(projects) != 0 {
		t.Fatalf("cross-origin request created %d projects", len(projects))
	}
}

func TestJSONMutationRejectsSimpleTextBody(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	projectPath := t.TempDir()
	payload := `{"name":"wrong-content-type","path":"` + strings.ReplaceAll(projectPath, `\`, `\\`) + `"}`
	resp, err := http.Post(ts.URL+"/api/projects", "text/plain", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 415: %s", resp.StatusCode, body)
	}
}

func TestAuthDisabledRESTRejectsForeignOrigin(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/projects", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Origin", "http://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 403: %s", resp.StatusCode, body)
	}
}

func TestAuthDisabledRejectsNonLoopbackHostEvenWhenOriginMatches(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	for _, path := range []string{"/api/projects", "/ws"} {
		t.Run(path, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, ts.URL+path, nil)
			if err != nil {
				t.Fatalf("create request: %v", err)
			}
			req.Host = "attacker.example"
			req.Header.Set("Origin", "http://attacker.example")
			if path == "/ws" {
				req.Header.Set("Connection", "Upgrade")
				req.Header.Set("Upgrade", "websocket")
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusForbidden {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d, want 403: %s", resp.StatusCode, body)
			}
		})
	}
}

func TestCrossOriginBearerRequestAndPreflightAreAllowed(t *testing.T) {
	ts, queries, cleanup := setupAuthenticatedTestServer(t)
	defer cleanup()

	createCookieSession(t, queries, true)
	const bearer = "test-remote-bearer"
	if err := queries.CreateAuthSession(context.Background(), store.CreateAuthSessionParams{
		Token:     bearer,
		ID:        sql.NullString{String: "test-bearer-session", Valid: true},
		UserID:    "00000000-0000-4000-8000-000000000001",
		ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		Kind:      "bearer",
	}); err != nil {
		t.Fatalf("create bearer session: %v", err)
	}

	preflight, err := http.NewRequest(http.MethodOptions, ts.URL+"/api/projects/example", nil)
	if err != nil {
		t.Fatalf("create preflight: %v", err)
	}
	preflight.Header.Set("Origin", "https://primary.example")
	preflight.Header.Set("Access-Control-Request-Method", http.MethodPatch)
	preflight.Header.Set("Access-Control-Request-Headers", "authorization, content-type")
	preflightResp, err := http.DefaultClient.Do(preflight)
	if err != nil {
		t.Fatalf("send preflight: %v", err)
	}
	preflightResp.Body.Close()
	if preflightResp.StatusCode != http.StatusNoContent || !strings.Contains(preflightResp.Header.Get("Access-Control-Allow-Methods"), http.MethodPatch) {
		t.Fatalf("preflight status/methods = %d %q", preflightResp.StatusCode, preflightResp.Header.Get("Access-Control-Allow-Methods"))
	}

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/projects", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Origin", "https://primary.example")
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != "*" || resp.Header.Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("unsafe bearer CORS headers: origin=%q credentials=%q", resp.Header.Get("Access-Control-Allow-Origin"), resp.Header.Get("Access-Control-Allow-Credentials"))
	}
}

func TestMachineCatalogRequiresFullAccess(t *testing.T) {
	ts, queries, cleanup := setupAuthenticatedTestServer(t)
	defer cleanup()

	token := createCookieSession(t, queries, false)
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/machines", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: "agentique_session", Value: token})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 403: %s", resp.StatusCode, body)
	}
}

func TestMachineCatalogCredentialsAreNeverCacheable(t *testing.T) {
	ts, queries, cleanup := setupAuthenticatedTestServer(t)
	defer cleanup()

	token := createCookieSession(t, queries, true)
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/machines", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: "agentique_session", Value: token})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", resp.Header.Get("Cache-Control"))
	}
}

func TestMachineCatalogRejectsInsecureRemoteAddress(t *testing.T) {
	ts, queries, cleanup := setupAuthenticatedTestServer(t)
	defer cleanup()

	token := createCookieSession(t, queries, true)
	body := `{"label":"remote","baseUrl":"http://remote.example:19201","token":"secret","sessionId":"session","identityKey":"key","addedAt":"2026-08-23T00:00:00Z"}`
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/api/machines/20000000-0000-4000-8000-000000000002", strings.NewReader(body))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "agentique_session", Value: token})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 400: %s", resp.StatusCode, body)
	}
}

func TestRemovingMachineRevokesRemoteBearerFirst(t *testing.T) {
	remoteID := "20000000-0000-4000-8000-000000000002"
	identity, err := machine.LoadOrCreateSigningIdentity(t.TempDir(), remoteID)
	if err != nil {
		t.Fatalf("create remote identity: %v", err)
	}
	var revoked atomic.Bool
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agentique/environment":
			httperror.JSON(w, http.StatusOK, map[string]string{
				"machineId": remoteID, "identityKey": identity.PublicKey(),
			})
		case "/api/auth/identity-proof":
			var body struct {
				Nonce string `json:"nonce"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			proof, err := identity.SignChallenge(body.Nonce)
			if err != nil {
				http.Error(w, "bad nonce", http.StatusBadRequest)
				return
			}
			httperror.JSON(w, http.StatusOK, map[string]string{
				"machineId": remoteID, "identityKey": identity.PublicKey(), "proof": proof,
			})
		case "/api/auth/session":
			if r.Method != http.MethodDelete || r.Header.Get("Authorization") != "Bearer remote-secret" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			revoked.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer remote.Close()

	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := store.RunMigrations(db, dbpkg.Migrations); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	queries := store.New(db)
	srv, err := server.New(queries, server.Config{
		AuthEnabled:       true,
		RPID:              "localhost",
		RPOrigins:         []string{"http://trusted.example"},
		MachineHTTPClient: remote.Client(),
		DB:                db,
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	primary := httptest.NewServer(srv)
	defer func() {
		srv.Shutdown()
		primary.Close()
	}()
	cookie := createCookieSession(t, queries, true)
	if err := queries.UpsertMachine(context.Background(), store.UpsertMachineParams{
		MachineID: remoteID, Label: "remote", BaseUrl: remote.URL, Token: "remote-secret",
		AddedAt: time.Now().UTC().Format(time.RFC3339), SessionID: "remote-session",
		IdentityKey: identity.PublicKey(),
	}); err != nil {
		t.Fatalf("store remote machine: %v", err)
	}

	req, err := http.NewRequest(http.MethodDelete, primary.URL+"/api/machines/"+remoteID, nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: "agentique_session", Value: cookie})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("remove machine: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 204: %s", resp.StatusCode, body)
	}
	if !revoked.Load() {
		t.Fatal("catalog row was removed without revoking the remote bearer")
	}
	if _, err := queries.GetMachine(context.Background(), remoteID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("machine row still exists or lookup failed: %v", err)
	}
}

func TestMachinePresentationUpdateCannotReplaceCredentials(t *testing.T) {
	ts, queries, cleanup := setupAuthenticatedTestServer(t)
	defer cleanup()
	identity, err := machine.LoadOrCreateSigningIdentity(t.TempDir(), "20000000-0000-4000-8000-000000000002")
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}
	const machineID = "20000000-0000-4000-8000-000000000002"
	if err := queries.UpsertMachine(context.Background(), store.UpsertMachineParams{
		MachineID: machineID, Label: "old", BaseUrl: "https://remote.example", Token: "secret",
		AddedAt: "2026-08-23T00:00:00Z", Icon: "server", SessionID: "session",
		IdentityKey: identity.PublicKey(),
	}); err != nil {
		t.Fatalf("store machine: %v", err)
	}
	cookie := createCookieSession(t, queries, true)
	req, err := http.NewRequest(
		http.MethodPatch,
		ts.URL+"/api/machines/"+machineID+"/presentation",
		strings.NewReader(`{"label":"new","icon":"laptop"}`),
	)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "agentique_session", Value: cookie})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("update presentation: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d: %s", resp.StatusCode, body)
	}
	entry, err := queries.GetMachine(context.Background(), machineID)
	if err != nil {
		t.Fatalf("load machine: %v", err)
	}
	if entry.Label != "new" || entry.Icon != "laptop" {
		t.Fatalf("presentation not updated: %+v", entry)
	}
	if entry.Token != "secret" || entry.BaseUrl != "https://remote.example" || entry.SessionID != "session" || entry.IdentityKey != identity.PublicKey() {
		t.Fatalf("presentation update changed credentials: %+v", entry)
	}
}

func TestListProjectsEmpty(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/api/projects")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}

	trimmed := strings.TrimSpace(string(body))
	if trimmed != "[]" {
		t.Fatalf("expected empty array [], got %q", trimmed)
	}
}

func TestListProjectsWithData(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	validPath := t.TempDir()
	payload := `{"name":"test","path":"` + strings.ReplaceAll(validPath, `\`, `\\`) + `"}`
	resp, err := http.Post(ts.URL+"/api/projects", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("unexpected error creating project: %v", err)
	}
	resp.Body.Close()

	resp, err = http.Get(ts.URL + "/api/projects")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var projects []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&projects); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}
}

func TestDeleteProject(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	// Create a project first.
	validPath := t.TempDir()
	payload := `{"name":"to-delete","path":"` + strings.ReplaceAll(validPath, `\`, `\\`) + `"}`
	resp, err := http.Post(ts.URL+"/api/projects", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("unexpected error creating project: %v", err)
	}

	var created map[string]any
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	id := created["id"].(string)

	// Delete the project.
	req, err := http.NewRequest(http.MethodDelete, ts.URL+"/api/projects/"+id, nil)
	if err != nil {
		t.Fatalf("unexpected error creating request: %v", err)
	}
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", resp.StatusCode)
	}

	// List should now be empty.
	resp, err = http.Get(ts.URL + "/api/projects")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	trimmed := strings.TrimSpace(string(body))
	if trimmed != "[]" {
		t.Fatalf("expected empty array after delete, got %q", trimmed)
	}
}

func TestCreateProjectMissingName(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Post(ts.URL+"/api/projects", "application/json", strings.NewReader(`{"name":"","path":"/tmp"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestCreateProjectMissingPath(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Post(ts.URL+"/api/projects", "application/json", strings.NewReader(`{"name":"test","path":""}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestSPAFallback(t *testing.T) {
	ts, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := http.Get(ts.URL + "/nonexistent-route")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}

	// SPA fallback should serve index.html (either stub or real frontend)
	if !strings.Contains(string(body), "<html") {
		t.Fatalf("expected HTML response for SPA fallback, got %q", string(body))
	}
}
