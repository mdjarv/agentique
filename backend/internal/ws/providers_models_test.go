package ws_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/providers"
	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/mdjarv/agentique/backend/internal/ws"
)

func claudeCatalog(t *testing.T, res providers.ListModelsResult) []providers.ModelInfo {
	t.Helper()
	for _, p := range res.Providers {
		if p.Provider == "claude" {
			return p.Models
		}
	}
	t.Fatal("no claude provider in providers.models response")
	return nil
}

// decodeModels round-trips the response payload through JSON so the test reads
// exactly what the frontend would receive off the wire.
func decodeModels(t *testing.T, resp ws.ServerResponse) providers.ListModelsResult {
	t.Helper()
	raw, err := json.Marshal(resp.Payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var out providers.ListModelsResult
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return out
}

func modelLabel(models []providers.ModelInfo, slug string) string {
	for _, m := range models {
		if m.Slug == slug {
			return m.DisplayName
		}
	}
	return ""
}

// TestProvidersModelsServesLearnedLabels drives the real wire path: a resolution
// observed from a session's init event must show up as a live version label on
// the next providers.models request, with no rebuild in between.
func TestProvidersModelsServesLearnedLabels(t *testing.T) {
	// Keep the developer's own ~/.claude.json out of the assertions.
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	ts, queries, cleanup := setupTestServer(t)
	defer cleanup()

	conn := dialWS(t, ts)
	defer conn.Close()

	resp := sendAndReceive(t, conn, "providers.models", "1", map[string]any{})
	if resp.Error != nil {
		t.Fatalf("providers.models error: %v", resp.Error)
	}
	before := decodeModels(t, resp)
	if got := modelLabel(claudeCatalog(t, before), "opus"); got != "Opus" {
		t.Fatalf("pre-learn opus label = %q, want Opus", got)
	}

	// What the session pipeline writes when the CLI reports its resolved model.
	if err := queries.UpsertModelResolution(context.Background(), store.UpsertModelResolutionParams{
		Provider:   "claude",
		Slug:       "opus",
		ResolvedID: "claude-opus-5",
	}); err != nil {
		t.Fatalf("upsert resolution: %v", err)
	}

	resp = sendAndReceive(t, conn, "providers.models", "2", map[string]any{})
	if resp.Error != nil {
		t.Fatalf("providers.models error: %v", resp.Error)
	}
	after := decodeModels(t, resp)
	models := claudeCatalog(t, after)
	if got := modelLabel(models, "opus"); got != "Opus 5" {
		t.Errorf("post-learn opus label = %q, want Opus 5", got)
	}
	for _, m := range models {
		if m.Slug == "opus" && m.ResolvedID != "claude-opus-5" {
			t.Errorf("opus resolvedId = %q, want claude-opus-5", m.ResolvedID)
		}
	}
}
