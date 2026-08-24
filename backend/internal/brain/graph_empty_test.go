package brain

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/httperror"
)

// Every array field of the graph report must marshal as [] and never null: the
// frontend types them all as arrays and reads .length unguarded, so one nil
// slice takes the whole Brain tab down. An empty brain is the case that finds
// it — a fresh install, and every test fixture.
func TestGraphReportArraysAreNeverNullOnAnEmptyBrain(t *testing.T) {
	svc, err := New(context.Background(), Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("brain.New: %v", err)
	}
	h := &Handler{Service: svc}

	rec := httptest.NewRecorder()
	httperror.HandlerFunc(h.HandleGraph).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/brain/graph", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); strings.Contains(body, ":null") {
		t.Errorf("graph payload has a null array on an empty brain: %s", body)
	}
}
