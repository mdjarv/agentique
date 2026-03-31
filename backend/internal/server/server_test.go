package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	dbpkg "github.com/mdjarv/agentique/backend/db"
	"github.com/mdjarv/agentique/backend/internal/server"
	"github.com/mdjarv/agentique/backend/internal/store"
)

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
	srv, err := server.New(queries, server.Config{})
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

	var body map[string]string
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
