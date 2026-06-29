package brain

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mdjarv/agentique/backend/internal/httperror"
	"github.com/mdjarv/agentique/backend/internal/memory"
)

// TestService_RestoreSnapshot_InvalidatesCache is the load-bearing F4 test: a snapshot
// restore rewrites files underneath the read-through cache, so RestoreSnapshot MUST
// invalidate the cache or List would keep serving the pre-restore (stale) corpus. The test
// warms the cache with a post-snapshot state right before restoring, so the assertion fails
// if the invalidation is missing.
func TestService_RestoreSnapshot_InvalidatesCache(t *testing.T) {
	svc := newSvc(t)
	ctx := context.Background()

	// Seed fact A and snapshot the tree.
	a := memory.New(memory.ScopeGlobal, "original text", memory.CategoryFact, memory.SourceAgent)
	if err := svc.store.Put(ctx, a); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.List(ctx); err != nil { // warm the cache
		t.Fatal(err)
	}
	// Snapshot the (original) tree at a fixed PAST time so its id can't collide with the
	// pre-restore safety snapshot RestoreSnapshot takes at now() — snapshot ids have 1-second
	// resolution and a same-second re-snapshot would O_TRUNC-overwrite this one with the
	// post-snapshot state, masking the restore. Mirrors how snapshot_test.go injects times.
	info, err := snapshotAt(svc.dir, svc.snapshotRetain, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// Mutate AFTER the snapshot: change A and add B. Both go through the cache (Put
	// invalidates), then warm the cache again so it holds the post-snapshot state.
	if _, err := svc.Update(ctx, a.ID, "changed after snapshot", ""); err != nil {
		t.Fatalf("Update: %v", err)
	}
	b := memory.New(memory.ScopeGlobal, "added after snapshot", memory.CategoryFact, memory.SourceAgent)
	if err := svc.store.Put(ctx, b); err != nil {
		t.Fatal(err)
	}
	warm, err := svc.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if findText(warm, a.ID) != "changed after snapshot" || !has(warm, b.ID) {
		t.Fatalf("precondition: cache should hold the post-snapshot state, got %+v", warm)
	}

	// Restore — must roll the tree back AND invalidate the warmed cache.
	if err := svc.RestoreSnapshot(info.ID); err != nil {
		t.Fatalf("RestoreSnapshot: %v", err)
	}

	after, err := svc.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := findText(after, a.ID); got != "original text" {
		t.Errorf("A = %q after restore, want 'original text' (stale cache?)", got)
	}
	if has(after, b.ID) {
		t.Errorf("B should be gone after restore (it post-dates the snapshot); stale cache?")
	}
}

func findText(recs []memory.Record, id string) string {
	for _, r := range recs {
		if r.ID == id {
			return r.Text
		}
	}
	return ""
}

func has(recs []memory.Record, id string) bool {
	for _, r := range recs {
		if r.ID == id {
			return true
		}
	}
	return false
}

func TestSnapshotEndpoints_RoundTrip(t *testing.T) {
	svc := newSvc(t)
	ctx := context.Background()
	if err := svc.store.Put(ctx, memory.New(memory.ScopeGlobal, "a fact", memory.CategoryFact, memory.SourceAgent)); err != nil {
		t.Fatal(err)
	}
	h := &Handler{Service: svc}

	mux := http.NewServeMux()
	mux.Handle("GET /api/brain/snapshots", httperror.HandlerFunc(h.HandleListSnapshots))
	mux.Handle("POST /api/brain/snapshots", httperror.HandlerFunc(h.HandleCreateSnapshot))
	mux.Handle("POST /api/brain/snapshots/{id}/restore", httperror.HandlerFunc(h.HandleRestoreSnapshot))

	// Take a snapshot.
	create := do(t, mux, http.MethodPost, "/api/brain/snapshots")
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d (%s)", create.Code, create.Body.String())
	}
	var snap snapshotDTO
	if err := json.Unmarshal(create.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if snap.ID == "" {
		t.Fatal("created snapshot has empty id")
	}

	// List it.
	list := do(t, mux, http.MethodGet, "/api/brain/snapshots")
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d", list.Code)
	}
	var snaps []snapshotDTO
	if err := json.Unmarshal(list.Body.Bytes(), &snaps); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if !hasSnap(snaps, snap.ID) {
		t.Fatalf("list %v missing created %q", snaps, snap.ID)
	}

	// Restore it (204).
	restore := do(t, mux, http.MethodPost, "/api/brain/snapshots/"+snap.ID+"/restore")
	if restore.Code != http.StatusNoContent {
		t.Fatalf("restore status = %d (%s)", restore.Code, restore.Body.String())
	}

	// A bogus id is a 404, not a 500.
	missing := do(t, mux, http.MethodPost, "/api/brain/snapshots/20000101T000000Z/restore")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("restore-missing status = %d, want 404", missing.Code)
	}
}

func do(t *testing.T, mux *http.ServeMux, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(method, path, nil))
	return rr
}

func hasSnap(ss []snapshotDTO, id string) bool {
	for _, s := range ss {
		if s.ID == id {
			return true
		}
	}
	return false
}
