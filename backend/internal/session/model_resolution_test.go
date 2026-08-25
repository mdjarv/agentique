package session

import (
	"context"
	"errors"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/store"
)

// fakeResolutionQueries records the two writes persistResolvedModel makes and
// stubs the rest of sessionQueries.
type fakeResolutionQueries struct {
	sessionQueries // nil — unused methods panic loudly rather than passing silently

	sessionUpdates []store.UpdateSessionResolvedModelParams
	upserts        []store.UpsertModelResolutionParams
	sessionErr     error
	upsertErr      error
}

func (f *fakeResolutionQueries) UpdateSessionResolvedModel(_ context.Context, arg store.UpdateSessionResolvedModelParams) error {
	f.sessionUpdates = append(f.sessionUpdates, arg)
	return f.sessionErr
}

func (f *fakeResolutionQueries) UpsertModelResolution(_ context.Context, arg store.UpsertModelResolutionParams) error {
	f.upserts = append(f.upserts, arg)
	return f.upsertErr
}

func TestPersistResolvedModelRecordsAliasMapping(t *testing.T) {
	q := &fakeResolutionQueries{}
	persistResolvedModel(sessionParams{id: "s1", model: "opus", provider: "claude", queries: q}, "claude-opus-5")

	if len(q.sessionUpdates) != 1 || q.sessionUpdates[0].ResolvedModel != "claude-opus-5" || q.sessionUpdates[0].ID != "s1" {
		t.Fatalf("session update = %+v", q.sessionUpdates)
	}
	if len(q.upserts) != 1 {
		t.Fatalf("upserts = %+v, want 1", q.upserts)
	}
	want := store.UpsertModelResolutionParams{Provider: "claude", Slug: "opus", ResolvedID: "claude-opus-5"}
	if q.upserts[0] != want {
		t.Errorf("upsert = %+v, want %+v", q.upserts[0], want)
	}
}

func TestPersistResolvedModelDefaultsProvider(t *testing.T) {
	q := &fakeResolutionQueries{}
	persistResolvedModel(sessionParams{id: "s1", model: "opus", queries: q}, "claude-opus-5")

	if len(q.upserts) != 1 || q.upserts[0].Provider != "claude" {
		t.Fatalf("upserts = %+v, want provider claude", q.upserts)
	}
}

func TestPersistResolvedModelSkipsNonAliasSlugs(t *testing.T) {
	tests := []struct {
		name       string
		slug       string
		resolvedID string
		wantWrites int
	}{
		{"pinned id teaches nothing", "claude-opus-5", "claude-opus-5", 0},
		{"no configured slug", "", "claude-opus-5", 0},
		{"alias", "opus", "claude-opus-5", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &fakeResolutionQueries{}
			persistResolvedModel(sessionParams{id: "s1", model: tt.slug, queries: q}, tt.resolvedID)
			if len(q.upserts) != tt.wantWrites {
				t.Errorf("upserts = %d, want %d", len(q.upserts), tt.wantWrites)
			}
			// The session row is history and is written regardless of slug shape.
			if len(q.sessionUpdates) != 1 {
				t.Errorf("session updates = %d, want 1", len(q.sessionUpdates))
			}
		})
	}
}

func TestPersistResolvedModelIgnoresEmptyID(t *testing.T) {
	q := &fakeResolutionQueries{}
	persistResolvedModel(sessionParams{id: "s1", model: "opus", queries: q}, "")
	if len(q.sessionUpdates) != 0 || len(q.upserts) != 0 {
		t.Errorf("wrote on empty model ID: %+v %+v", q.sessionUpdates, q.upserts)
	}
}

func TestPersistResolvedModelSurvivesWriteFailures(t *testing.T) {
	q := &fakeResolutionQueries{sessionErr: errors.New("locked"), upsertErr: errors.New("locked")}
	// A catalog hint that cannot be stored must not take the session down.
	persistResolvedModel(sessionParams{id: "s1", model: "opus", queries: q}, "claude-opus-5")
	if len(q.upserts) == 0 {
		t.Error("upsert was never attempted")
	}
}

func TestPipelineConfigBroadcastsResolvedModel(t *testing.T) {
	q := &fakeResolutionQueries{}
	var pushType string
	var payload PushSessionModelResolved
	p := sessionParams{
		id:       "s1",
		model:    "opus[1m]",
		provider: "claude",
		queries:  q,
		broadcast: func(gotType string, gotPayload any) {
			pushType = gotType
			payload = gotPayload.(PushSessionModelResolved)
		},
	}

	buildPipelineConfig(&Session{}, p).OnResolvedModel("claude-opus-5[1m]")

	if pushType != "session.model-resolved" {
		t.Fatalf("push type = %q", pushType)
	}
	if payload.SessionID != "s1" || payload.ResolvedModel != "claude-opus-5[1m]" {
		t.Errorf("payload = %+v", payload)
	}
}
