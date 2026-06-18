package brain

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	claudecli "github.com/allbin/claudecli-go"

	"github.com/mdjarv/agentique/backend/internal/httperror"
	"github.com/mdjarv/agentique/backend/internal/memory"
)

// blockingRunner blocks until the context is cancelled, then returns ctx.Err() —
// the shape of a wedged or long-rate-limited model call.
type blockingRunner struct{}

func (blockingRunner) RunBlocking(ctx context.Context, _ string, _ ...claudecli.Option) (*claudecli.BlockingResult, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// A wedged refine must not hang the handler indefinitely: the bounded context
// cancels the call and HandleRefine reports 504 instead of blocking forever.
func TestHandleRefineTimesOut(t *testing.T) {
	prev := refineTimeout
	refineTimeout = 20 * time.Millisecond
	t.Cleanup(func() { refineTimeout = prev })

	s := newSvc(t)
	rec, err := s.Add(context.Background(), memory.ScopeGlobal, "a fact to refine", memory.CategoryFact, memory.SourceAgent)
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{Service: s, Runner: blockingRunner{}}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/brain/memories/"+rec.ID+"/refine",
		jsonBody(`{"instruction":"make it shorter","model":"haiku"}`))
	req.SetPathValue("id", rec.ID)

	done := make(chan error, 1)
	go func() { done <- h.HandleRefine(w, req) }()

	select {
	case err = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("HandleRefine did not return — the timeout failed to unblock it")
	}

	herr, ok := err.(*httperror.Error)
	if !ok {
		t.Fatalf("want *httperror.Error, got %T: %v", err, err)
	}
	if herr.Status != http.StatusGatewayTimeout {
		t.Fatalf("want 504 on refine timeout, got %d", herr.Status)
	}
}
