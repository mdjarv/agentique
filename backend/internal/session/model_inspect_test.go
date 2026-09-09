package session

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/store"
)

// fakeInspectQueries stubs the two reads ModelInspector makes.
type fakeInspectQueries struct {
	session     store.Session
	sessionErr  error
	resolution  store.ModelResolution
	resErr      error
	resRequests []store.GetModelResolutionParams
}

func (f *fakeInspectQueries) GetSession(_ context.Context, _ string) (store.Session, error) {
	return f.session, f.sessionErr
}

func (f *fakeInspectQueries) GetModelResolution(_ context.Context, arg store.GetModelResolutionParams) (store.ModelResolution, error) {
	f.resRequests = append(f.resRequests, arg)
	return f.resolution, f.resErr
}

func nullString(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }

func TestInspectSessionModelPrefersLivePipeline(t *testing.T) {
	q := &fakeInspectQueries{session: store.Session{
		ID:            "s1",
		Model:         "opus",
		Provider:      "claude",
		ResolvedModel: "claude-opus-5",
		ResolvedAt:    nullString("2026-09-09T10:00:00Z"),
	}}
	live := func(string) string { return "claude-opus-5" }

	got, err := NewModelInspector(q, live).InspectSessionModel(context.Background(), "s1")
	if err != nil {
		t.Fatalf("InspectSessionModel: %v", err)
	}
	want := ModelReport{
		SessionID:       "s1",
		Provider:        "claude",
		RequestedSlug:   "opus",
		ResolvedModelID: "claude-opus-5",
		ResolvedAt:      "2026-09-09T10:00:00Z",
		Source:          ModelSourceInitEvent,
	}
	if got != want {
		t.Fatalf("report = %+v, want %+v", got, want)
	}
	if len(q.resRequests) != 0 {
		t.Errorf("catalog consulted despite a live answer: %+v", q.resRequests)
	}
}

// A live id the row does not yet name is reported without a stamp: the row
// dates the id it carries, and inventing "now" would date a reading nothing
// took.
func TestInspectSessionModelLiveAheadOfRowHasNoStamp(t *testing.T) {
	q := &fakeInspectQueries{session: store.Session{
		ID:         "s1",
		Model:      "opus",
		Provider:   "claude",
		ResolvedAt: nullString("2026-09-01T10:00:00Z"),
	}}

	got, err := NewModelInspector(q, func(string) string { return "claude-opus-5" }).
		InspectSessionModel(context.Background(), "s1")
	if err != nil {
		t.Fatalf("InspectSessionModel: %v", err)
	}
	if got.ResolvedModelID != "claude-opus-5" || got.Source != ModelSourceInitEvent {
		t.Fatalf("report = %+v", got)
	}
	if got.ResolvedAt != "" {
		t.Errorf("ResolvedAt = %q, want empty", got.ResolvedAt)
	}
}

func TestInspectSessionModelReadsRowWhenNotLive(t *testing.T) {
	q := &fakeInspectQueries{session: store.Session{
		ID:            "s1",
		Model:         "opus",
		Provider:      "claude",
		ResolvedModel: "claude-opus-5",
		ResolvedAt:    nullString("2026-09-09T10:00:00Z"),
	}}

	got, err := NewModelInspector(q, nil).InspectSessionModel(context.Background(), "s1")
	if err != nil {
		t.Fatalf("InspectSessionModel: %v", err)
	}
	if got.Source != ModelSourceInitEvent || got.ResolvedModelID != "claude-opus-5" || got.ResolvedAt != "2026-09-09T10:00:00Z" {
		t.Fatalf("report = %+v", got)
	}
}

func TestInspectSessionModelFallsBackToCatalog(t *testing.T) {
	q := &fakeInspectQueries{
		session: store.Session{ID: "s1", Model: "opus", Provider: "claude"},
		resolution: store.ModelResolution{
			Provider:   "claude",
			Slug:       "opus",
			ResolvedID: "claude-opus-5",
			LastSeenAt: "2026-09-08T09:00:00Z",
		},
	}

	got, err := NewModelInspector(q, func(string) string { return "" }).
		InspectSessionModel(context.Background(), "s1")
	if err != nil {
		t.Fatalf("InspectSessionModel: %v", err)
	}
	want := ModelReport{
		SessionID:       "s1",
		Provider:        "claude",
		RequestedSlug:   "opus",
		ResolvedModelID: "claude-opus-5",
		ResolvedAt:      "2026-09-08T09:00:00Z",
		Source:          ModelSourceCatalog,
	}
	if got != want {
		t.Fatalf("report = %+v, want %+v", got, want)
	}
	if len(q.resRequests) != 1 || q.resRequests[0].Slug != "opus" || q.resRequests[0].Provider != "claude" {
		t.Errorf("catalog lookup = %+v", q.resRequests)
	}
}

func TestInspectSessionModelUnresolvedIsStated(t *testing.T) {
	tests := []struct {
		name string
		q    *fakeInspectQueries
	}{
		{
			name: "nothing learned for the slug",
			q: &fakeInspectQueries{
				session: store.Session{ID: "s1", Model: "opus", Provider: "claude"},
				resErr:  sql.ErrNoRows,
			},
		},
		{
			name: "catalog unreadable",
			q: &fakeInspectQueries{
				session: store.Session{ID: "s1", Model: "opus", Provider: "claude"},
				resErr:  errors.New("database is locked"),
			},
		},
		{
			name: "no slug to look up",
			q:    &fakeInspectQueries{session: store.Session{ID: "s1", Provider: "claude"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewModelInspector(tt.q, nil).InspectSessionModel(context.Background(), "s1")
			if err != nil {
				t.Fatalf("InspectSessionModel: %v", err)
			}
			if got.Source != ModelSourceUnresolved {
				t.Errorf("Source = %q, want %q", got.Source, ModelSourceUnresolved)
			}
			if got.ResolvedModelID != "" || got.ResolvedAt != "" {
				t.Errorf("report = %+v, want no resolved id or stamp", got)
			}
			if got.Provider != "claude" {
				t.Errorf("Provider = %q", got.Provider)
			}
		})
	}
}

// An unknown session is an error, not an unresolved report: "we could not find
// it" and "it has not resolved yet" are different answers.
func TestInspectSessionModelMissingSessionErrors(t *testing.T) {
	q := &fakeInspectQueries{sessionErr: sql.ErrNoRows}

	if _, err := NewModelInspector(q, nil).InspectSessionModel(context.Background(), "gone"); err == nil {
		t.Fatal("want an error for a session that does not exist")
	}
}

// A missing provider defaults the same way every other surface defaults it, so
// the catalog lookup asks for a provider that exists.
func TestInspectSessionModelNormalizesProvider(t *testing.T) {
	q := &fakeInspectQueries{
		session: store.Session{ID: "s1", Model: "opus"},
		resErr:  sql.ErrNoRows,
	}

	got, err := NewModelInspector(q, nil).InspectSessionModel(context.Background(), "s1")
	if err != nil {
		t.Fatalf("InspectSessionModel: %v", err)
	}
	if got.Provider != normalizeProvider("") {
		t.Errorf("Provider = %q, want %q", got.Provider, normalizeProvider(""))
	}
	if len(q.resRequests) != 1 || q.resRequests[0].Provider != normalizeProvider("") {
		t.Errorf("catalog lookup = %+v", q.resRequests)
	}
}
