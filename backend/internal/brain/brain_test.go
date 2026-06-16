package brain

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/memory"
)

func jsonBody(s string) *strings.Reader { return strings.NewReader(s) }

func newSvc(t *testing.T) *Service {
	t.Helper()
	s, err := New(context.Background(), Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAddDedupAndIdentityPin(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	scope := ScopeForProject("p1")

	r1, err := s.Add(ctx, scope, "Project builds with just.", memory.CategoryProject, memory.SourceAgent)
	if err != nil {
		t.Fatal(err)
	}
	// duplicate (different casing/punctuation) returns the same record
	r2, err := s.Add(ctx, scope, "project builds with just", memory.CategoryProject, memory.SourceAgent)
	if err != nil {
		t.Fatal(err)
	}
	if r1.ID != r2.ID {
		t.Fatalf("duplicate add should be idempotent: %s vs %s", r1.ID, r2.ID)
	}
	// identity facts auto-pin
	id, err := s.Add(ctx, scope, "User's name is Mathias.", memory.CategoryIdentity, memory.SourceAgent)
	if err != nil {
		t.Fatal(err)
	}
	if !id.Pinned {
		t.Fatal("identity fact should be auto-pinned")
	}
}

func TestUpdateMarksHuman(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	r, _ := s.Add(ctx, memory.ScopeGlobal, "original", memory.CategoryFact, memory.SourceAgent)
	upd, err := s.Update(ctx, r.ID, "edited by hand", memory.CategoryPreference)
	if err != nil {
		t.Fatal(err)
	}
	if upd.Text != "edited by hand" || upd.Category != memory.CategoryPreference {
		t.Fatalf("update not applied: %+v", upd)
	}
	if upd.Source != memory.SourceHuman {
		t.Fatalf("edit should mark source human, got %s", upd.Source)
	}
}

func TestPinLockToggles(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	r, _ := s.Add(ctx, memory.ScopeGlobal, "a fact", memory.CategoryFact, memory.SourceAgent)
	if p, _ := s.SetPinned(ctx, r.ID, true); !p.Pinned {
		t.Fatal("pin failed")
	}
	if l, _ := s.SetLocked(ctx, r.ID, true); !l.Locked {
		t.Fatal("lock failed")
	}
}

func TestConsolidateFingerprintPersisted(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	s.Add(ctx, memory.ScopeGlobal, "fact one", memory.CategoryFact, memory.SourceAgent)
	s.Add(ctx, memory.ScopeGlobal, "fact two", memory.CategoryFact, memory.SourceAgent)

	rep1, err := s.Consolidate(ctx, memory.ScopeGlobal, nil, memory.DecayPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if rep1.Fingerprint == "" {
		t.Fatal("expected a fingerprint")
	}
	// Second pass with no changes: persisted fingerprint should mark it skipped.
	rep2, err := s.Consolidate(ctx, memory.ScopeGlobal, fakeNoopExtractor{}, memory.DecayPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if !rep2.Skipped {
		t.Fatal("expected second consolidation to be skipped via persisted fingerprint")
	}
}

type fakeNoopExtractor struct{}

func (fakeNoopExtractor) Extract(context.Context, []string) ([]memory.Candidate, error) {
	return nil, nil
}
func (fakeNoopExtractor) Reorganize(_ context.Context, f []memory.Fact) ([]memory.Fact, error) {
	return f, nil
}

func TestMCPAdapterScopeIsolation(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	// session s1 -> project p1; session s2 -> project p2
	resolve := func(_ context.Context, sid string) memory.Scope {
		return ScopeForProject(sid)
	}
	a := NewMCPAdapter(s, resolve)

	if _, err := a.MemoryAdd(ctx, "p1", "use just in p1", "preference"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.MemoryAdd(ctx, "p2", "use make in p2", "preference"); err != nil {
		t.Fatal(err)
	}
	// a global fact added directly
	s.Add(ctx, memory.ScopeGlobal, "global convention applies", memory.CategoryFact, memory.SourceAgent)

	out, err := a.MemorySearch(ctx, "p1", "build tool just")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "just in p1") {
		t.Fatalf("p1 search should find p1 memory: %q", out)
	}
	if strings.Contains(out, "make in p2") {
		t.Fatalf("p1 search must NOT find p2 memory: %q", out)
	}
}

func TestHTTPCreateListPinDelete(t *testing.T) {
	s := newSvc(t)
	h := &Handler{Service: s}

	// create
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/brain/memories", jsonBody(`{"scope":"global","text":"a brain fact","category":"fact"}`))
	if err := h.HandleCreate(rec, req); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status=%d", rec.Code)
	}
	var created memoryDTO
	json.Unmarshal(rec.Body.Bytes(), &created)
	if created.ID == "" || created.Source != string(memory.SourceHuman) {
		t.Fatalf("created dto wrong: %+v", created)
	}

	// list
	rec = httptest.NewRecorder()
	if err := h.HandleList(rec, httptest.NewRequest(http.MethodGet, "/api/brain/memories", nil)); err != nil {
		t.Fatal(err)
	}
	var list []memoryDTO
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 {
		t.Fatalf("list len=%d", len(list))
	}

	// pin
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/brain/memories/"+created.ID+"/pin", jsonBody(`{"pinned":true}`))
	req.SetPathValue("id", created.ID)
	if err := h.HandlePin(rec, req); err != nil {
		t.Fatal(err)
	}
	var pinned memoryDTO
	json.Unmarshal(rec.Body.Bytes(), &pinned)
	if !pinned.Pinned {
		t.Fatal("pin not applied")
	}

	// delete
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/brain/memories/"+created.ID, nil)
	req.SetPathValue("id", created.ID)
	if err := h.HandleDelete(rec, req); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d", rec.Code)
	}
}

func TestHTTPGetMissingReturnsNotFound(t *testing.T) {
	h := &Handler{Service: newSvc(t)}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/brain/memories/nope", nil)
	req.SetPathValue("id", "nope")
	err := h.HandleGet(rec, req)
	if err == nil {
		t.Fatal("expected not-found error")
	}
}
