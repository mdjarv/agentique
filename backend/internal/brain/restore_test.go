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

// archivedFixture seeds an archived fact directly through the store (the test is package
// brain, so the unexported store is reachable) and returns its id.
func archivedFixture(t *testing.T, svc *Service, text string) string {
	t.Helper()
	rec := memory.New(memory.ScopeGlobal, text, memory.CategoryFact, memory.SourceAgent)
	rec.Lifecycle = memory.LifecycleArchived
	rec.LastUsedAt = time.Time{} // cold: no recent use
	if err := svc.store.Put(context.Background(), rec); err != nil {
		t.Fatalf("seed archived fact: %v", err)
	}
	return rec.ID
}

func TestService_Restore(t *testing.T) {
	svc := newSvc(t)
	ctx := context.Background()

	id := archivedFixture(t, svc, "an archived fact")
	got, err := svc.Restore(ctx, id)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if got.Lifecycle != memory.LifecycleActive {
		t.Errorf("Lifecycle = %q, want active", got.Lifecycle)
	}
	if got.LastUsedAt.IsZero() {
		t.Errorf("LastUsedAt should be bumped on restore, got zero")
	}
	// Persisted, not just returned.
	reloaded, err := svc.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if reloaded.Lifecycle != memory.LifecycleActive {
		t.Errorf("persisted Lifecycle = %q, want active", reloaded.Lifecycle)
	}
}

func TestService_Restore_NoOpOnActive(t *testing.T) {
	svc := newSvc(t)
	ctx := context.Background()

	stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	rec := memory.New(memory.ScopeGlobal, "a live fact", memory.CategoryFact, memory.SourceAgent)
	rec.Lifecycle = memory.LifecycleActive
	rec.LastUsedAt = stamp
	if err := svc.store.Put(ctx, rec); err != nil {
		t.Fatal(err)
	}

	got, err := svc.Restore(ctx, rec.ID)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if got.Lifecycle != memory.LifecycleActive {
		t.Errorf("Lifecycle = %q, want active (unchanged)", got.Lifecycle)
	}
	if !got.LastUsedAt.Equal(stamp) {
		t.Errorf("LastUsedAt = %v, want unchanged %v (no-op on an active fact)", got.LastUsedAt, stamp)
	}
}

func TestHandleRestore_RoundTrip(t *testing.T) {
	svc := newSvc(t)
	h := &Handler{Service: svc}
	id := archivedFixture(t, svc, "round-trip archived fact")

	mux := http.NewServeMux()
	mux.Handle("POST /api/brain/memories/{id}/restore", httperror.HandlerFunc(h.HandleRestore))

	req := httptest.NewRequest(http.MethodPost, "/api/brain/memories/"+id+"/restore", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rr.Code, rr.Body.String())
	}
	var dto memoryDTO
	if err := json.Unmarshal(rr.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dto.Lifecycle != "active" {
		t.Errorf("response lifecycle = %q, want active", dto.Lifecycle)
	}
	if dto.ID != id {
		t.Errorf("response id = %q, want %q", dto.ID, id)
	}
}
