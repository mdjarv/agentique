package brain

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mdjarv/agentique/backend/internal/memory"
	"github.com/mdjarv/agentique/backend/internal/memory/filestore"
)

// svcWithRecords seeds records into a fresh brain dir (via the filestore, before the Service
// reads it cold) so tests can set fields the public API doesn't expose (LastUsedAt, Lifecycle).
func svcWithRecords(t *testing.T, recs ...memory.Record) *Service {
	t.Helper()
	dir := t.TempDir()
	fs := filestore.New(dir)
	for _, r := range recs {
		if err := fs.Put(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}
	svc, err := New(context.Background(), Config{Dir: dir, ArchiveFloor: memory.DefaultArchiveConfidenceFloor})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestService_Consolidate_ArchivesFaded(t *testing.T) {
	ctx := context.Background()
	faded := memory.New(memory.ScopeGlobal, "a long-untouched brain fact about ledger reconciliation", memory.CategoryFact, memory.SourceConsolidated)
	faded.ID = "fade"
	faded.LastUsedAt = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	faded.UpdatedAt = faded.LastUsedAt
	svc := svcWithRecords(t, faded)

	rep, err := svc.Consolidate(ctx, memory.ScopeGlobal, nil,
		memory.DecayPolicy{MaxAge: 24 * time.Hour, ArchiveFloor: memory.DefaultArchiveConfidenceFloor}, false, ConsolidateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Decayed) != 1 || rep.Decayed[0].ID != "fade" {
		t.Fatalf("expected 'fade' archived, got %+v", rep.Decayed)
	}
	got, err := svc.Get(ctx, "fade")
	if err != nil {
		t.Fatalf("archived fact must not be deleted: %v", err)
	}
	if got.Lifecycle != memory.LifecycleArchived {
		t.Fatalf("Lifecycle=%s, want archived", got.Lifecycle)
	}
}

func TestRestoreActiveRefreshesLastUsedAt(t *testing.T) {
	ctx := context.Background()
	arch := memory.New(memory.ScopeGlobal, "an archived fact to revive", memory.CategoryFact, memory.SourceConsolidated)
	arch.ID = "arch"
	arch.Lifecycle = memory.LifecycleArchived
	arch.LastUsedAt = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	arch.UpdatedAt = arch.LastUsedAt
	svc := svcWithRecords(t, arch)

	// A hand-edit (Update) revives the archived fact: flips it back to active and restarts the
	// disuse clock so it is not immediately re-archived.
	before := time.Now().UTC()
	got, err := svc.Update(ctx, "arch", "an archived fact to revive, edited", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Lifecycle != memory.LifecycleActive {
		t.Fatalf("Update must un-archive: Lifecycle=%s", got.Lifecycle)
	}
	if got.LastUsedAt.Before(before) {
		t.Fatalf("Update must refresh LastUsedAt on un-archive: %v < %v", got.LastUsedAt, before)
	}
}

func TestAddRevivesArchivedDuplicate(t *testing.T) {
	ctx := context.Background()
	arch := memory.New(memory.ScopeGlobal, "an archived fact that gets re-observed", memory.CategoryFact, memory.SourceConsolidated)
	arch.ID = "arch"
	arch.Lifecycle = memory.LifecycleArchived
	arch.LastUsedAt = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	arch.UpdatedAt = arch.LastUsedAt
	svc := svcWithRecords(t, arch)

	// Re-observing the archived fact's text reinforces AND revives it (same id, now active) —
	// instead of burying the renewed corroboration in the cold tier.
	got, err := svc.Add(ctx, memory.ScopeGlobal, "an archived fact that gets re-observed", memory.CategoryFact, memory.SourceAgent)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "arch" {
		t.Fatalf("re-observation should match the archived fact, got %s", got.ID)
	}
	if got.Lifecycle != memory.LifecycleActive {
		t.Fatalf("a re-observed archived fact must be revived, got %s", got.Lifecycle)
	}
	if got.Corroborations != 1 {
		t.Fatalf("Corroborations=%d, want 1", got.Corroborations)
	}
	recs, err := svc.List(ctx, memory.ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("revive must not create a duplicate record: got %d", len(recs))
	}
}

func TestOperatingContract_SkipsArchived(t *testing.T) {
	ctx := context.Background()
	scope := ScopeForProject("p1")
	arch := memory.New(scope, "the user prefers tabs over spaces", memory.CategoryPreference, memory.SourceHuman)
	arch.ID = "arch"
	arch.Lifecycle = memory.LifecycleArchived
	live := memory.New(scope, "the user prefers dark mode in the editor", memory.CategoryPreference, memory.SourceHuman)
	live.ID = "live"
	svc := svcWithRecords(t, arch, live)

	oc := svc.OperatingContract(ctx, "p1")
	if strings.Contains(oc, "tabs over spaces") {
		t.Fatalf("an archived preference must not enter the operating contract:\n%s", oc)
	}
	if !strings.Contains(oc, "dark mode") {
		t.Fatalf("a live high-confidence preference should be in the contract:\n%s", oc)
	}
}

// The read-time fade belongs to the model-facing pull and to nothing else.
//
// RecallForPull is what the assistant's `recall` verb reads, so a fact that has gone
// cold stops being asserted to a head a while before the churn archives it. Recall is
// the memory page's search box and `agentique brain search` — a person curating what
// is about to be forgotten — and a fact it hid there would still be sitting in the
// list beside it.
func TestOnlyTheModelFacingPullFadesAColdFact(t *testing.T) {
	ctx := context.Background()
	faded := memory.New(memory.ScopeGlobal, "The legacy auth module uses bcrypt for password hashing.",
		memory.CategoryFact, memory.SourceConsolidated)
	faded.ID = "faded"
	faded.LastUsedAt = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) // very cold
	faded.UpdatedAt = faded.LastUsedAt
	svc := svcWithRecords(t, faded) // archiving enabled (ArchiveFloor set)

	scopes := []memory.Scope{memory.ScopeGlobal}
	query := "how does the legacy auth module store passwords"

	pull, err := svc.RecallForPull(ctx, scopes, query, 5)
	if err != nil {
		t.Fatalf("RecallForPull() = %v", err)
	}
	if len(pull.Recalled) != 0 {
		t.Errorf("a faded fact reached a head's pull: %+v", pull.Recalled)
	}

	browse, err := svc.Recall(ctx, scopes, query, 5)
	if err != nil {
		t.Fatalf("Recall() = %v", err)
	}
	if len(browse.Recalled) != 1 || browse.Recalled[0].ID != "faded" {
		t.Errorf("browsing recall hid a live row it can still see in the list: %+v", browse.Recalled)
	}
}

// With archiving off (floor 0) nothing fades anywhere, which is the deploy-safety
// half of the same contract: a stray archive-confidence-floor can never evict a live
// fact from recall while the churn is not archiving.
func TestNothingFadesWithArchivingOff(t *testing.T) {
	ctx := context.Background()
	cold := memory.New(memory.ScopeGlobal, "The legacy auth module uses bcrypt for password hashing.",
		memory.CategoryFact, memory.SourceConsolidated)
	cold.ID = "cold"
	cold.LastUsedAt = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	cold.UpdatedAt = cold.LastUsedAt

	dir := t.TempDir()
	fs := filestore.New(dir)
	if err := fs.Put(ctx, cold); err != nil {
		t.Fatal(err)
	}
	svc, err := New(ctx, Config{Dir: dir}) // no ArchiveFloor
	if err != nil {
		t.Fatal(err)
	}

	res, err := svc.RecallForPull(ctx, []memory.Scope{memory.ScopeGlobal},
		"how does the legacy auth module store passwords", 5)
	if err != nil {
		t.Fatalf("RecallForPull() = %v", err)
	}
	if len(res.Recalled) != 1 {
		t.Errorf("archiving is off, so nothing may fade: %+v", res.Recalled)
	}
}
