package httperror

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// An unclassified error's text is internal detail — paths, SQL, filenames.
// It belongs in the log, not in the response body.
func TestClassifyDoesNotLeakUnclassifiedErrorText(t *testing.T) {
	err := fmt.Errorf("open /home/operator/.local/share/agentique/agentique.db: permission denied")
	he := Classify(err)

	if he.Status != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", he.Status)
	}
	if he.Message != genericInternalMessage {
		t.Errorf("message = %q, want %q", he.Message, genericInternalMessage)
	}
	if !errors.Is(he.Cause, err) {
		t.Error("the original error must stay reachable as Cause for the log line")
	}
}

func TestRespondErrorBodyForAnUnclassifiedError(t *testing.T) {
	w := httptest.NewRecorder()
	RespondError(w, fmt.Errorf("dial tcp 10.0.0.7:5432: connect: connection refused"))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"10.0.0.7", "5432", "dial tcp"} {
		if strings.Contains(body.Error, leak) {
			t.Errorf("response body leaks %q: %s", leak, body.Error)
		}
	}
}

// Typed errors are deliberate, caller-facing messages and must survive intact.
func TestClassifyKeepsTypedMessages(t *testing.T) {
	for _, he := range []*Error{
		BadRequest("path must be absolute"),
		NotFound("session not found"),
		Conflict("slug is already in use"),
		Internal("list projects", errors.New("boom")),
	} {
		got := Classify(he)
		if got.Message != he.Message || got.Status != he.Status {
			t.Errorf("Classify(%v) = %d/%q, want %d/%q", he, got.Status, got.Message, he.Status, he.Message)
		}
	}
	// A typed error wrapped further down still classifies to itself.
	wrapped := fmt.Errorf("while loading: %w", NotFound("memory not found"))
	if got := Classify(wrapped); got.Status != http.StatusNotFound {
		t.Errorf("wrapped typed error status = %d, want 404", got.Status)
	}
}

func TestClassifyMapsNoRows(t *testing.T) {
	if got := Classify(fmt.Errorf("get: %w", sql.ErrNoRows)); got.Status != http.StatusNotFound {
		t.Errorf("status = %d, want 404", got.Status)
	}
}
