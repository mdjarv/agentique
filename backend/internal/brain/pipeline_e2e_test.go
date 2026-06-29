package brain

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mdjarv/agentique/backend/internal/memory"
)

// End-to-end synthetic scenarios for the Band 1 pipeline. These drive the REAL brain.Service
// over a temp filestore (keyword recall, no DB/Chroma/model) — the only LLM seam, the Extractor,
// is stubbed with canned output. Each subtest reads as a narrative of one intended behaviour and
// asserts only through the public methods the server actually calls.

// canExtractor is a controllable Extractor: extract distills episodes into candidate facts;
// reorganize cleans the durable set (identity by default).
type canExtractor struct {
	extract    func(episodes []string) []memory.Candidate
	reorganize func(facts []memory.Fact) []memory.Fact
}

func (e canExtractor) Extract(_ context.Context, eps []string) ([]memory.Candidate, error) {
	if e.extract == nil {
		return nil, nil
	}
	return e.extract(eps), nil
}

func (e canExtractor) Reorganize(_ context.Context, f []memory.Fact) ([]memory.Fact, error) {
	if e.reorganize == nil {
		return f, nil
	}
	return e.reorganize(f), nil
}

// distillTo returns an Extractor that always distills to the one given fact (both at staging time
// and at promotion time) — modelling a deterministic "the model read the transcript and produced
// this fact".
func distillTo(text string, cat memory.Category) canExtractor {
	return canExtractor{extract: func([]string) []memory.Candidate {
		return []memory.Candidate{{Text: text, Category: cat}}
	}}
}

// TestE2E_InjectionGate is the headline: writing is cheap, injection is earned. A session's
// transcript stages a RAW capture that recall will NOT inject; only the churn promotes it to a
// consolidated fact, after which recall injects it (with provenance back to the capture).
func TestE2E_InjectionGate(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)
	scope := ScopeForProject("proj")
	fact := "The project build tool is just, never raw npx commands."
	ex := distillTo(fact, memory.CategoryPreference)

	// 1. A finished session is ingested → staged as a capture (NOT injectable).
	staged, err := svc.LearnFromTranscript(ctx, scope, []TranscriptEvent{
		promptEvent(t, "remember we always run the build with just, never raw npx"),
	}, ex)
	if err != nil || staged != 1 {
		t.Fatalf("expected 1 capture staged, got %d (err %v)", staged, err)
	}
	all, _ := svc.List(ctx, scope)
	if len(all) != 1 || all[0].Source != memory.SourceCapture {
		t.Fatalf("ingest must stage exactly one capture, got %+v", all)
	}

	// 2. Recall does NOT inject the capture (the gate).
	if block, ids := svc.RecallBlock(ctx, "proj", "what build tool does the project use", nil); strings.TrimSpace(block) != "" || len(ids) != 0 {
		t.Fatalf("a raw capture must not be injected: block=%q ids=%v", block, ids)
	}

	// 3. The nightly churn promotes capture → consolidated (with DerivedFrom provenance).
	rep, err := svc.Consolidate(ctx, scope, ex, memory.DecayPolicy{}, false, ConsolidateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Promoted) != 1 || len(rep.Promoted[0].DerivedFrom) != 1 {
		t.Fatalf("churn must promote the capture with provenance, got %+v", rep.Promoted)
	}
	if len(rep.CapturesConsumed) != 1 {
		t.Fatalf("the promoted capture must be consumed, got %+v", rep.CapturesConsumed)
	}

	// 4. NOW recall injects the earned fact.
	block, ids := svc.RecallBlock(ctx, "proj", "what build tool does the project use", nil)
	if !strings.Contains(block, "build tool") || len(ids) == 0 {
		t.Fatalf("after promotion the fact must be injectable: block=%q ids=%v", block, ids)
	}
}

// TestE2E_ReinforcementStrengthens: re-observing a known durable fact strengthens it in place
// (corroboration + confidence) instead of duplicating it.
func TestE2E_ReinforcementStrengthens(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)
	scope := ScopeForProject("proj")

	r1, err := svc.Add(ctx, scope, "The CI pipeline runs on every push to the main branch.", memory.CategoryFact, memory.SourceAgent)
	if err != nil {
		t.Fatal(err)
	}
	// Observe the same fact again (different casing/punctuation) twice.
	var last memory.Record
	for i := 0; i < 2; i++ {
		last, err = svc.Add(ctx, scope, "the ci pipeline runs on every push to the main branch", memory.CategoryFact, memory.SourceAgent)
		if err != nil {
			t.Fatal(err)
		}
	}
	if last.ID != r1.ID {
		t.Fatalf("re-observation must reinforce the same record, not duplicate: %s vs %s", last.ID, r1.ID)
	}
	if last.Corroborations != 2 {
		t.Fatalf("Corroborations=%d, want 2", last.Corroborations)
	}
	if !(last.ConfidenceScore > r1.ConfidenceScore) {
		t.Fatalf("confidence must rise on re-observation: %v !> %v", last.ConfidenceScore, r1.ConfidenceScore)
	}
	if all, _ := svc.List(ctx, scope); len(all) != 1 {
		t.Fatalf("reinforcement must not create duplicates, got %d records", len(all))
	}
}

// TestE2E_AgeArchiveRevive: a fact left untouched fades out of recall, the churn archives it
// (kept on disk, never deleted), and re-observing it REVIVES it back into recall.
func TestE2E_AgeArchiveRevive(t *testing.T) {
	ctx := context.Background()
	text := "The legacy auth module uses bcrypt for password hashing."
	faded := memory.New(memory.ScopeGlobal, text, memory.CategoryFact, memory.SourceConsolidated)
	faded.ID = "faded"
	faded.LastUsedAt = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) // very cold
	faded.UpdatedAt = faded.LastUsedAt
	svc := svcWithRecords(t, faded) // archiving enabled (ArchiveFloor set)
	query := "how does the legacy auth module store passwords"

	// 1. Cold fact is faded out of recall (computed, not yet archived).
	if block, _ := svc.RecallBlock(ctx, "proj", query, nil); strings.Contains(block, "bcrypt") {
		t.Fatalf("a faded fact should drop from recall: %q", block)
	}

	// 2. The churn ARCHIVES it — on disk, not deleted.
	if _, err := svc.Consolidate(ctx, memory.ScopeGlobal, canExtractor{},
		memory.DecayPolicy{MaxAge: 24 * time.Hour, ArchiveFloor: memory.DefaultArchiveConfidenceFloor}, false, ConsolidateOpts{}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(ctx, "faded")
	if err != nil {
		t.Fatalf("archive must not delete: %v", err)
	}
	if got.Lifecycle != memory.LifecycleArchived {
		t.Fatalf("Lifecycle=%s, want archived", got.Lifecycle)
	}

	// 3. Re-observing the fact REVIVES it (restore-on-re-observe) and it recalls again.
	revived, err := svc.Add(ctx, memory.ScopeGlobal, text, memory.CategoryFact, memory.SourceAgent)
	if err != nil {
		t.Fatal(err)
	}
	if revived.ID != "faded" || revived.Lifecycle != memory.LifecycleActive {
		t.Fatalf("re-observation must revive the archived fact: id=%s lifecycle=%s", revived.ID, revived.Lifecycle)
	}
	if block, ids := svc.RecallBlock(ctx, "proj", query, nil); !strings.Contains(block, "bcrypt") || len(ids) == 0 {
		t.Fatalf("a revived fact must recall again: block=%q ids=%v", block, ids)
	}
}

// TestE2E_ExemptionsNeverArchive: human ground-truth and evergreen facts survive the archive
// churn no matter how cold.
func TestE2E_ExemptionsNeverArchive(t *testing.T) {
	ctx := context.Background()
	stale := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	human := memory.New(memory.ScopeGlobal, "The user's name is Mathias.", memory.CategoryFact, memory.SourceHuman)
	human.ID = "human"
	human.LastUsedAt, human.UpdatedAt = stale, stale
	evergreen := memory.New(memory.ScopeGlobal, "The team always squashes commits before merge.", memory.CategoryIdentity, memory.SourceConsolidated)
	evergreen.ID = "evergreen" // CategoryIdentity → Volatility evergreen
	evergreen.LastUsedAt, evergreen.UpdatedAt = stale, stale
	svc := svcWithRecords(t, human, evergreen)

	rep, err := svc.Consolidate(ctx, memory.ScopeGlobal, canExtractor{},
		memory.DecayPolicy{MaxAge: 24 * time.Hour, ArchiveFloor: memory.DefaultArchiveConfidenceFloor}, false, ConsolidateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Decayed) != 0 {
		t.Fatalf("human/evergreen facts must never archive, got %+v", rep.Decayed)
	}
	for _, id := range []string{"human", "evergreen"} {
		got, err := svc.Get(ctx, id)
		if err != nil || got.Lifecycle != memory.LifecycleActive {
			t.Fatalf("%s must stay active (err %v, lifecycle %s)", id, err, got.Lifecycle)
		}
	}
}

// TestE2E_SnapshotRestoreReversibility: a churn is reversible — snapshot, mutate, restore.
func TestE2E_SnapshotRestoreReversibility(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)
	scope := ScopeForProject("proj")
	if _, err := svc.Add(ctx, scope, "An original fact worth keeping.", memory.CategoryFact, memory.SourceAgent); err != nil {
		t.Fatal(err)
	}

	// The pre-churn snapshot the automation takes. Use a fixed past timestamp so the snapshot id
	// can't collide (1s resolution) with Restore's own wall-clock safety snapshot in a fast test.
	info, err := snapshotAt(svc.dir, 0, time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	// Mutate the brain after the snapshot.
	if _, err := svc.Add(ctx, scope, "A regrettable later fact.", memory.CategoryFact, memory.SourceAgent); err != nil {
		t.Fatal(err)
	}
	if all, _ := svc.List(ctx, scope); len(all) != 2 {
		t.Fatalf("expected 2 facts before restore, got %d", len(all))
	}

	// Restore to the snapshot — back to the single original fact (offline op on the same dir).
	if err := Restore(svc.dir, info.ID, 0); err != nil {
		t.Fatal(err)
	}
	// A fresh Service reads the restored tree (the live one's cache is bypassed by an offline restore).
	svc2, err := New(ctx, Config{Dir: svc.dir})
	if err != nil {
		t.Fatal(err)
	}
	all, _ := svc2.List(ctx, scope)
	if len(all) != 1 || !strings.Contains(all[0].Text, "original") {
		t.Fatalf("restore must roll the brain back to the snapshot, got %+v", all)
	}
}

// TestE2E_DurableJobSurvivesRestart: a session-end learn job persisted before a "crash" is
// replayed by a fresh queue and still ingests — no silent loss.
func TestE2E_DurableJobSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	svc := newSvc(t)
	db := newFakeJobStore()
	ex := distillTo("The deploy runbook lives in docs/deploy.md.", memory.CategoryFact)
	learn := func(_ context.Context, j Job) (bool, error) {
		n, err := svc.LearnFromTranscript(ctx, j.Scope, j.Events, ex)
		return n > 0, err
	}

	// "Process 1" enqueues the job durably, then crashes before draining.
	q1 := NewJobQueue(db, nil, 5, map[string]JobHandler{}) // no handler → nothing drains
	if err := q1.Enqueue(ctx, JobKindLearn, "proj", []TranscriptEvent{promptEvent(t, "the deploy runbook is in docs/deploy.md")}); err != nil {
		t.Fatal(err)
	}
	if db.count() != 1 {
		t.Fatalf("job must be durable before any drain, got %d rows", db.count())
	}

	// "Process 2" restarts with the learn handler and drains on startup — the job replays.
	q2 := NewJobQueue(db, nil, 5, map[string]JobHandler{JobKindLearn: learn})
	q2.Drain(ctx)

	all, _ := svc.List(ctx, ScopeForProject("proj"))
	if len(all) != 1 || all[0].Source != memory.SourceCapture {
		t.Fatalf("the resumed job must have ingested the transcript as a capture, got %+v", all)
	}
	if db.count() != 0 {
		t.Fatalf("the completed job must be deleted, got %d rows", db.count())
	}
}
