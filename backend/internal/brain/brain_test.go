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

func TestPinnedPreambleAndListScopes(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	scope := ScopeForProject("p1")

	// A pinned (identity auto-pins) fact + a non-pinned one in the project.
	if _, err := s.Add(ctx, scope, "User's name is Mathias.", memory.CategoryIdentity, memory.SourceAgent); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(ctx, scope, "Project builds with just.", memory.CategoryProject, memory.SourceAgent); err != nil {
		t.Fatal(err)
	}
	// A pinned global fact, plus an unrelated project scope.
	if _, err := s.Add(ctx, memory.ScopeGlobal, "Don't push without asking.", memory.CategoryIdentity, memory.SourceAgent); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(ctx, ScopeForProject("p2"), "other project fact", memory.CategoryFact, memory.SourceAgent); err != nil {
		t.Fatal(err)
	}

	pre := s.PinnedPreamble(ctx, "p1")
	if !strings.Contains(pre, "Mathias") || !strings.Contains(pre, "Don't push") {
		t.Fatalf("preamble should include p1 + global pinned facts, got:\n%s", pre)
	}
	if strings.Contains(pre, "builds with just") {
		t.Fatalf("non-pinned facts must not be injected, got:\n%s", pre)
	}
	if strings.Contains(pre, "other project fact") {
		t.Fatalf("another project's facts must not leak into p1's preamble, got:\n%s", pre)
	}

	scopes, err := s.ListScopes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(scopes) != 3 { // project:p1, project:p2, global
		t.Fatalf("want 3 scopes, got %d: %v", len(scopes), scopes)
	}
}

func TestRecallBlock(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	p1 := ScopeForProject("p1")

	// A pinned identity fact (must NOT appear in the recall block — it's already in
	// the pinned preamble), a relevant non-pinned fact, an irrelevant one, and a
	// fact in another project (must not leak across scopes even if its words match).
	if _, err := s.Add(ctx, p1, "User's name is Mathias.", memory.CategoryIdentity, memory.SourceAgent); err != nil {
		t.Fatal(err)
	}
	rel, err := s.Add(ctx, p1, "Database migrations use goose numbering.", memory.CategoryProject, memory.SourceAgent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(ctx, p1, "Frontend uses tailwind classes.", memory.CategoryProject, memory.SourceAgent); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(ctx, ScopeForProject("p2"), "secret p2 database thing", memory.CategoryFact, memory.SourceAgent); err != nil {
		t.Fatal(err)
	}

	block, _ := s.RecallBlock(ctx, "p1", "how do database migrations work with goose", nil)
	if !strings.Contains(block, "goose numbering") {
		t.Fatalf("recall block should surface the relevant fact, got:\n%s", block)
	}
	if strings.Contains(block, "Mathias") {
		t.Fatalf("pinned facts belong in the preamble, not the recall block, got:\n%s", block)
	}
	if strings.Contains(block, "tailwind") {
		t.Fatalf("irrelevant facts must not be recalled, got:\n%s", block)
	}
	if strings.Contains(block, "secret") {
		t.Fatalf("another project's fact must not leak into p1's recall, got:\n%s", block)
	}

	// Injecting a fact is a retrieval-practice event: BumpUses/LastUsedAt stamped so
	// two-factor strength starts accruing real read signal (the whole point).
	got, err := s.Get(ctx, rel.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Uses != 1 {
		t.Fatalf("recalled fact Uses should be bumped to 1, got %d", got.Uses)
	}
	if got.LastUsedAt.IsZero() {
		t.Fatal("recalled fact LastUsedAt should be stamped")
	}

	// Delta recall: a fact already surfaced this session (passed in exclude) is not
	// re-injected, even though it still matches the query.
	if b, _ := s.RecallBlock(ctx, "p1", "how do database migrations work with goose", map[string]struct{}{rel.ID: {}}); b != "" {
		t.Fatalf("an already-seen fact must not be re-injected, got:\n%s", b)
	}

	// A too-thin prompt and a no-match query both yield no injection (and no bump).
	if b, _ := s.RecallBlock(ctx, "p1", "ok", nil); b != "" {
		t.Fatalf("a low-content prompt should yield no recall block, got:\n%s", b)
	}
	if b, _ := s.RecallBlock(ctx, "p1", "xyzzy plugh unrelated nonsense", nil); b != "" {
		t.Fatalf("a query that matches nothing should yield no recall block, got:\n%s", b)
	}
	if got2, _ := s.Get(ctx, rel.ID); got2.Uses != 1 {
		t.Fatalf("Uses must not advance on excluded/thin/no-match recalls, got %d", got2.Uses)
	}
}

func TestImportRecordsDedupAndFlags(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	scope := ScopeForProject("p1")
	if _, err := s.Add(ctx, scope, "Project builds with just.", memory.CategoryProject, memory.SourceAgent); err != nil {
		t.Fatal(err)
	}

	recs := []memory.Record{
		{Text: "project builds with just", Category: memory.CategoryProject, Source: memory.SourceConsolidated}, // dup → skip
		{Text: "User prefers concise replies.", Category: memory.CategoryPreference, Source: memory.SourceHuman, Pinned: true},
		{Text: "Locked convention.", Category: memory.CategoryFact, Source: memory.SourceConsolidated, Locked: true},
		{Text: "   ", Category: memory.CategoryFact}, // empty → skip
	}
	n, err := s.ImportRecords(ctx, scope, recs)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("want 2 imported (dup + empty skipped), got %d", n)
	}

	all, _ := s.List(ctx, scope)
	var pinned, locked int
	for _, r := range all {
		if r.Pinned {
			pinned++
		}
		if r.Locked {
			locked++
		}
	}
	if pinned != 1 || locked != 1 {
		t.Fatalf("import must preserve pinned/locked flags: pinned=%d locked=%d", pinned, locked)
	}

	if n2, _ := s.ImportRecords(ctx, scope, recs); n2 != 0 {
		t.Fatalf("re-import should be idempotent, got %d new", n2)
	}
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

	rep1, err := s.Consolidate(ctx, memory.ScopeGlobal, nil, memory.DecayPolicy{}, false, TidyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep1.Fingerprint == "" {
		t.Fatal("expected a fingerprint")
	}
	// Second pass with no changes: persisted fingerprint should mark it skipped.
	rep2, err := s.Consolidate(ctx, memory.ScopeGlobal, fakeNoopExtractor{}, memory.DecayPolicy{}, false, TidyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !rep2.Skipped {
		t.Fatal("expected second consolidation to be skipped via persisted fingerprint")
	}
	// Third pass with force=true: the unchanged set is reorganized anyway (not skipped).
	rep3, err := s.Consolidate(ctx, memory.ScopeGlobal, fakeNoopExtractor{}, memory.DecayPolicy{}, false, TidyOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if rep3.Skipped {
		t.Fatal("force should re-run the reorganization despite an unchanged fingerprint")
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

// MemoryFlag (RFC-LD D2) weakens a recalled fact for review, is scoped to the
// session's own project + global, and never deletes.
func TestMCPMemoryFlag(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	resolve := func(_ context.Context, sid string) memory.Scope { return ScopeForProject(sid) }
	a := NewMCPAdapter(s, resolve)

	own, _ := s.Add(ctx, ScopeForProject("p1"), "the API base path is /v1", memory.CategoryFact, memory.SourceAgent)
	other, _ := s.Add(ctx, ScopeForProject("p2"), "p2 only fact", memory.CategoryFact, memory.SourceAgent)

	// An agent in p1 can flag its own fact; it is weakened, not deleted.
	if _, err := a.MemoryFlag(ctx, "p1", own.ID, "it's actually /v2 now"); err != nil {
		t.Fatalf("flagging own fact should succeed: %v", err)
	}
	got, err := s.Get(ctx, own.ID)
	if err != nil {
		t.Fatalf("flagged fact must still exist (never deleted): %v", err)
	}
	if got.ConfidenceScore > memory.AmbiguousScoreThreshold || got.ReviewNote == "" {
		t.Fatalf("flagged fact should be weakened with a note, got score=%.2f note=%q", got.ConfidenceScore, got.ReviewNote)
	}

	// An agent in p1 cannot flag a p2 fact.
	if _, err := a.MemoryFlag(ctx, "p1", other.ID, "nope"); err == nil {
		t.Fatal("flagging another project's fact must be rejected")
	}
}

// MemoryUsed (RFC-LD D2 positive half) strengthens a recalled fact an agent confirmed
// useful: it raises confidence and Helped, is scoped to the session's own project +
// global, and never touches another project's facts.
func TestMCPMemoryUsed(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	resolve := func(_ context.Context, sid string) memory.Scope { return ScopeForProject(sid) }
	a := NewMCPAdapter(s, resolve)

	own, _ := s.Add(ctx, ScopeForProject("p1"), "deploys run via just release", memory.CategoryFact, memory.SourceAgent)
	other, _ := s.Add(ctx, ScopeForProject("p2"), "p2 only fact", memory.CategoryFact, memory.SourceAgent)
	before, _ := s.Get(ctx, own.ID)

	if _, err := a.MemoryUsed(ctx, "p1", own.ID); err != nil {
		t.Fatalf("marking own fact useful should succeed: %v", err)
	}
	got, _ := s.Get(ctx, own.ID)
	if got.Helped != 1 {
		t.Fatalf("MemoryUsed should increment Helped, got %d", got.Helped)
	}
	if got.ConfidenceScore <= before.ConfidenceScore {
		t.Fatalf("a positive outcome should raise confidence: %.4f !> %.4f", got.ConfidenceScore, before.ConfidenceScore)
	}

	// An agent in p1 cannot mark a p2 fact.
	if _, err := a.MemoryUsed(ctx, "p1", other.ID); err == nil {
		t.Fatal("marking another project's fact must be rejected")
	}
}

// OperatingContract elevates only high-confidence, non-flagged preferences to standing
// instructions; a fresh inferred preference must EARN its place (human confirm or outcome
// corroboration), and non-preferences / flagged prefs never appear.
func TestOperatingContract(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	p1 := ScopeForProject("p1")

	// A fresh agent-inferred preference (0.8) is below the act-on gate → not yet in the contract.
	pref, _ := s.Add(ctx, p1, "Never push without being asked.", memory.CategoryPreference, memory.SourceAgent)
	// A human-authored preference (ground truth) → in the contract immediately.
	if _, err := s.Add(ctx, p1, "Commit on the current branch; do not branch unprompted.", memory.CategoryPreference, memory.SourceHuman); err != nil {
		t.Fatal(err)
	}
	// A high-confidence non-preference fact → never in the contract.
	if _, err := s.Add(ctx, p1, "The build tool is just.", memory.CategoryProject, memory.SourceHuman); err != nil {
		t.Fatal(err)
	}

	contract := s.OperatingContract(ctx, "p1")
	if strings.Contains(contract, "Never push") {
		t.Fatalf("a fresh inferred pref should NOT yet drive behavior:\n%s", contract)
	}
	if !strings.Contains(contract, "Commit on the current branch") {
		t.Fatalf("a human-authored pref should be in the contract:\n%s", contract)
	}
	if strings.Contains(contract, "build tool is just") {
		t.Fatalf("a non-preference fact must not appear in the contract:\n%s", contract)
	}

	// One positive outcome graduates the inferred pref past the gate → now in the contract.
	if _, err := s.MarkHelped(ctx, pref.ID); err != nil {
		t.Fatal(err)
	}
	if got := s.OperatingContract(ctx, "p1"); !strings.Contains(got, "Never push") {
		t.Fatalf("a corroborated pref should graduate into the contract:\n%s", got)
	}

	// A contradiction demotes it back out (flagged → excluded even though score may matter).
	if _, err := s.Flag(ctx, pref.ID, "user pushed manually this session"); err != nil {
		t.Fatal(err)
	}
	if got := s.OperatingContract(ctx, "p1"); strings.Contains(got, "Never push") {
		t.Fatalf("a flagged pref must drop out of the contract:\n%s", got)
	}
}

// Applying a plan must clear the held preview job, so GET /consolidate/job stops
// serving it and the frontend can't re-hydrate an already-applied proposal on every
// remount (the "same proposal shows up every time" bug).
func TestApplyClearsHeldJob(t *testing.T) {
	s := newSvc(t)
	h := &Handler{Service: s}

	// Simulate a completed global preview sitting in memory.
	h.publishJob(JobState{ID: "j1", Kind: "global", Phase: phaseDone})
	if h.currentJob() == nil {
		t.Fatal("precondition: a finished job should be held")
	}

	// Apply an (empty) global plan — succeeds with no changes.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/brain/consolidate/global/apply", jsonBody(`{"plan":{}}`))
	if err := h.HandleApplyGlobal(rec, req); err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if h.currentJob() != nil {
		t.Fatalf("apply must clear the held job so it doesn't re-hydrate, got %+v", h.currentJob())
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
