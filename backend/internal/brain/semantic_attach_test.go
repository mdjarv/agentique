package brain

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mdjarv/agentique/backend/internal/memory"
	"github.com/mdjarv/agentique/backend/internal/memory/vectortest"
)

const attachTestCollection = "attach_test"

// newAttachSvc builds a Service configured against the fakes, on a clock fast enough for a test.
func newAttachSvc(t *testing.T, c *vectortest.Chroma, e *vectortest.Embedder) *Service {
	t.Helper()
	svc, err := New(context.Background(), Config{
		Dir:        t.TempDir(),
		ChromaURL:  c.URL,
		EmbedURL:   e.URL,
		EmbedModel: "fake",
		Collection: attachTestCollection,
	})
	if err != nil {
		t.Fatal(err)
	}
	svc.timing = semanticTiming{
		probeInterval: 15 * time.Millisecond,
		retryMin:      5 * time.Millisecond,
		retryMax:      20 * time.Millisecond,
		detachAfter:   2,
		probeTimeout:  time.Second,
	}
	return svc
}

// runAttachLoop runs RunSemantic until the test ends. Registered after the fakes, so the loop
// stops before their servers close.
func runAttachLoop(t *testing.T, svc *Service) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.RunSemantic(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func addFact(t *testing.T, svc *Service, scope memory.Scope, text string) memory.Record {
	t.Helper()
	r, err := svc.Add(context.Background(), scope, text, memory.CategoryFact, memory.SourceConsolidated)
	if err != nil {
		t.Fatalf("add %q: %v", text, err)
	}
	return r
}

// New must not dial: a backend that is up at construction is still not attached until the loop
// (or Connect) attaches it.
func TestNewDoesNotAttach(t *testing.T) {
	t.Parallel()
	c, e := vectortest.NewChroma(t), vectortest.NewEmbedder(t)
	svc := newAttachSvc(t, c, e)
	if svc.SemanticEnabled() {
		t.Fatal("New attached a backend; attaching is the loop's job")
	}
	if got := svc.SemanticStatus().State; got != SemanticConnecting {
		t.Fatalf("state after New = %q, want connecting", got)
	}
	if e.Texts() != 0 || c.Docs(attachTestCollection) != nil {
		t.Fatal("New reached the backend")
	}
}

// The incident: Chroma down at boot left the process keyword-only until a restart. Now the
// loop attaches when Chroma comes up, over a complete index, and loses it again honestly.
func TestRunSemanticAttachesWhenBackendComesUpAndDetachesWhenItGoes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c, e := vectortest.NewChroma(t), vectortest.NewEmbedder(t)
	c.SetDown(true)
	svc := newAttachSvc(t, c, e)
	scope := ScopeForProject("attach")

	// A corpus bigger than one index batch, written before anything is attached. Put directly:
	// Add would dedup texts this alike into one reinforced fact.
	const facts = 2*64 + 5
	for i := range facts {
		r := memory.New(scope, fmt.Sprintf("fact number %d about the release pipeline", i), memory.CategoryFact, memory.SourceConsolidated)
		if err := svc.activeStore().Put(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	runAttachLoop(t, svc)

	eventually(t, "unreachable status", func() bool {
		return svc.SemanticStatus().State == SemanticUnreachable
	})
	if st := svc.SemanticStatus(); st.Reason != ReasonChromaUnreachable || st.DownSince.IsZero() {
		t.Fatalf("status = %+v, want chroma-unreachable with a down-since", st)
	}
	if svc.SemanticEnabled() {
		t.Fatal("attached to a Chroma that is down")
	}

	// Chroma comes up. No restart.
	c.SetDown(false)
	eventually(t, "attach", svc.SemanticEnabled)
	if st := svc.SemanticStatus(); st.State != SemanticOn || !st.DownSince.IsZero() || st.Reason != "" {
		t.Fatalf("status after attach = %+v", st)
	}
	if got := len(c.Docs(attachTestCollection)); got != facts {
		t.Fatalf("empty collection caught up to %d vectors, want %d", got, facts)
	}
	if e.MaxBatch() > 64 {
		t.Fatalf("an embed request carried %d inputs, over the index batch", e.MaxBatch())
	}

	// A write now is indexed on its way in, and recall is hybrid.
	fresh := addFact(t, svc, scope, "semantic recall picks up a backend without a restart")
	if doc := c.Docs(attachTestCollection)[fresh.ID]; doc != fresh.Text {
		t.Fatalf("new fact not indexed: %q", doc)
	}
	res, err := svc.Recall(ctx, recallScopes(scope), "backend without a restart", 5)
	if err != nil || len(res.Recalled) == 0 {
		t.Fatalf("recall while attached: %v, %d results", err, len(res.Recalled))
	}

	// Chroma goes away. Status tells the truth, writes still land, recall degrades to keyword.
	c.SetDown(true)
	eventually(t, "detach", func() bool { return !svc.SemanticEnabled() })
	if st := svc.SemanticStatus(); st.State != SemanticUnreachable || st.DownSince.IsZero() {
		t.Fatalf("status after detach = %+v", st)
	}
	lost := addFact(t, svc, scope, "a fact written while the vector index was gone")
	res, err = svc.Recall(ctx, recallScopes(scope), "written while the vector index was gone", 5)
	if err != nil || len(res.Recalled) == 0 {
		t.Fatalf("keyword recall while detached: %v, %d results", err, len(res.Recalled))
	}

	// And back: the write it missed is caught up on re-attach.
	c.SetDown(false)
	eventually(t, "re-attach", svc.SemanticEnabled)
	if doc := c.Docs(attachTestCollection)[lost.ID]; doc != lost.Text {
		t.Fatalf("fact written while detached not indexed on re-attach: %q", doc)
	}
}

// A dead embedder is a dead backend: the old boot probe asked Chroma alone.
func TestRunSemanticRefusesADeadEmbedder(t *testing.T) {
	t.Parallel()
	c, e := vectortest.NewChroma(t), vectortest.NewEmbedder(t)
	e.SetDown(true)
	svc := newAttachSvc(t, c, e)
	runAttachLoop(t, svc)

	eventually(t, "unreachable status", func() bool {
		return svc.SemanticStatus().State == SemanticUnreachable
	})
	if got := svc.SemanticStatus().Reason; got != ReasonEmbedderUnreachable {
		t.Fatalf("reason = %q, want embedder-unreachable", got)
	}
	e.SetDown(false)
	eventually(t, "attach", svc.SemanticEnabled)
}

// An index write that fails during a blip too short to detach is healed on the next healthy
// probe, rather than leaving that fact unsearchable until a manual reindex.
func TestRunSemanticHealsAWriteLostToABlip(t *testing.T) {
	t.Parallel()
	c, e := vectortest.NewChroma(t), vectortest.NewEmbedder(t)
	svc := newAttachSvc(t, c, e)
	svc.timing.detachAfter = 1 << 30 // never detach: this is about the blip
	runAttachLoop(t, svc)
	eventually(t, "attach", svc.SemanticEnabled)

	c.SetDown(true)
	blip := addFact(t, svc, ScopeForProject("heal"), "a fact whose vector write failed")
	c.SetDown(false)
	eventually(t, "heal", func() bool {
		return c.Docs(attachTestCollection)[blip.ID] == blip.Text
	})
	if !svc.SemanticEnabled() {
		t.Fatal("a blip under the detach threshold detached")
	}
}

// Readers take one snapshot of the backend while the loop swaps it underneath them. Run under
// -race: recall, writes, graph edges and status all read what attach and detach replace.
func TestSemanticSwapIsRaceFree(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c, e := vectortest.NewChroma(t), vectortest.NewEmbedder(t)
	svc := newAttachSvc(t, c, e)
	svc.timing.detachAfter = 1
	scope := ScopeForProject("race")
	for i := range 8 {
		addFact(t, svc, scope, fmt.Sprintf("seed fact %d about concurrent recall", i))
	}
	runAttachLoop(t, svc)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for w := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := svc.Recall(ctx, recallScopes(scope), "concurrent recall", 5); err != nil {
					t.Errorf("recall: %v", err)
					return
				}
				if _, err := svc.Add(ctx, scope, fmt.Sprintf("worker %d fact %d", w, i), memory.CategoryFact, memory.SourceAgent); err != nil {
					t.Errorf("add: %v", err)
					return
				}
				recs, _ := svc.List(ctx, scope)
				_, _ = svc.SemanticEdges(ctx, durableRecords(recs))
				_ = svc.SemanticStatus()
				_ = svc.SemanticEnabled()
			}
		}()
	}
	for range 6 {
		c.SetDown(false)
		eventually(t, "attach", svc.SemanticEnabled)
		c.SetDown(true)
		eventually(t, "detach", func() bool { return !svc.SemanticEnabled() })
	}
	close(stop)
	wg.Wait()
}

// Calibration embeds the whole corpus, so a flapping backend must not repeat it per attach.
func TestCalibrationRunsOnFirstAttachOnly(t *testing.T) {
	t.Parallel()
	c, e := vectortest.NewChroma(t), vectortest.NewEmbedder(t)
	svc := newAttachSvc(t, c, e)
	svc.semCfg.calibrate = true
	for i := range 40 {
		addFact(t, svc, ScopeForProject(fmt.Sprintf("cal-%d", i%4)), fmt.Sprintf("calibration corpus fact %d on topic %d", i, i%4))
	}
	svc.timing.detachAfter = 1
	runAttachLoop(t, svc)
	eventually(t, "attach", svc.SemanticEnabled)
	svc.connectMu.Lock()
	first := svc.calibrated
	tried := svc.calibrationTried
	svc.connectMu.Unlock()
	if !tried {
		t.Fatal("calibration did not run on the first attach")
	}

	c.SetDown(true)
	eventually(t, "detach", func() bool { return !svc.SemanticEnabled() })
	c.SetDown(false)
	eventually(t, "re-attach", svc.SemanticEnabled)
	svc.connectMu.Lock()
	again := svc.calibrated
	svc.connectMu.Unlock()
	if again != first {
		t.Fatal("re-attach calibrated again")
	}
}

// With a backend configured and detached, every pass that persists similarity-derived
// structure refuses; a dry run and an explicit lexical pass do not.
func TestConsolidationRefusesWhileDetached(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c, e := vectortest.NewChroma(t), vectortest.NewEmbedder(t)
	c.SetDown(true)
	svc := newAttachSvc(t, c, e)
	scope := ScopeForProject("gate")
	addFact(t, svc, scope, "the build uses just")
	addFact(t, svc, scope, "the tests run with the race detector")

	if _, err := svc.Consolidate(ctx, scope, nil, memory.DecayPolicy{}, false, ConsolidateOpts{}); !errors.Is(err, ErrSemanticUnavailable) {
		t.Fatalf("Consolidate while detached = %v, want ErrSemanticUnavailable", err)
	}
	if _, err := svc.ApplyPlan(ctx, scope, memory.Plan{Scope: scope}, memory.DecayPolicy{}, false); !errors.Is(err, ErrSemanticUnavailable) {
		t.Fatalf("ApplyPlan while detached = %v, want ErrSemanticUnavailable", err)
	}
	if _, err := svc.ApplyGlobal(ctx, memory.GlobalPlan{}, false); !errors.Is(err, ErrSemanticUnavailable) {
		t.Fatalf("ApplyGlobal while detached = %v, want ErrSemanticUnavailable", err)
	}
	if _, err := svc.AssignAreas(ctx); !errors.Is(err, ErrSemanticUnavailable) {
		t.Fatalf("AssignAreas while detached = %v, want ErrSemanticUnavailable", err)
	}
	if _, err := svc.Consolidate(ctx, scope, nil, memory.DecayPolicy{}, true, ConsolidateOpts{}); err != nil {
		t.Fatalf("a dry run writes nothing and must not refuse: %v", err)
	}
	if _, err := svc.Consolidate(ctx, scope, nil, memory.DecayPolicy{}, false, ConsolidateOpts{AllowLexical: true}); err != nil {
		t.Fatalf("an explicitly lexical pass must run: %v", err)
	}
	if _, err := svc.PreviewAreas(ctx); err != nil {
		t.Fatalf("a preview persists nothing and must not refuse: %v", err)
	}

	// Unconfigured is keyword by choice, and consolidates as it always did.
	plain := newSvc(t)
	addFact(t, plain, scope, "the build uses just")
	if _, err := plain.Consolidate(ctx, scope, nil, memory.DecayPolicy{}, false, ConsolidateOpts{}); err != nil {
		t.Fatalf("keyword-mode consolidate: %v", err)
	}
}

// detachingExtractor detaches the attached backend the first time a scope is reorganized: the
// backend lost in the middle of a scheduled pass.
type detachingExtractor struct {
	svc  *Service
	once sync.Once
}

func (d *detachingExtractor) Extract(context.Context, []string) ([]memory.Candidate, error) {
	return nil, nil
}

func (d *detachingExtractor) Reorganize(_ context.Context, facts []memory.Fact) ([]memory.Fact, error) {
	d.once.Do(func() {
		if b := d.svc.backend(); b != nil {
			d.svc.detach(b, errors.New("lost mid-pass"))
		}
	})
	return facts, nil
}

// A scheduled pass skips while detached and runs on the next attach rather than the next
// interval; one whose backend is lost mid-pass stops at the next scope instead of carrying on
// lexically.
func TestScheduledConsolidationWaitsForAttach(t *testing.T) {
	t.Parallel()
	c, e := vectortest.NewChroma(t), vectortest.NewEmbedder(t)
	c.SetDown(true)
	svc := newAttachSvc(t, c, e)
	for _, p := range []string{"one", "two"} {
		addFact(t, svc, ScopeForProject(p), "a fact in project "+p)
		addFact(t, svc, ScopeForProject(p), "another fact in project "+p)
	}
	a := NewAutomation(svc, nil, nil, time.Hour, "", 0, 0)
	a.initialDelay = time.Millisecond
	a.Start()
	t.Cleanup(a.Stop)

	// Skipped: nothing ran, so no pre-churn snapshot was taken.
	time.Sleep(30 * time.Millisecond)
	if snaps, _ := svc.ListSnapshots(); len(snaps) != 0 {
		t.Fatalf("a pass ran while the backend was detached (%d snapshots)", len(snaps))
	}

	// The attach runs the owed pass without waiting out the hour.
	runAttachLoop(t, svc)
	c.SetDown(false)
	eventually(t, "the owed pass", func() bool {
		snaps, _ := svc.ListSnapshots()
		return len(snaps) > 0
	})

}

// A pass whose backend is lost mid-pass stops at the next scope instead of carrying on without
// embeddings, and reports the rest as owed to the next attach.
func TestScheduledConsolidationStopsWhenBackendIsLostMidPass(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c, e := vectortest.NewChroma(t), vectortest.NewEmbedder(t)
	svc := newAttachSvc(t, c, e)
	scopes := []memory.Scope{ScopeForProject("one"), ScopeForProject("two")}
	for _, scope := range scopes {
		addFact(t, svc, scope, "a fact in "+string(scope))
		addFact(t, svc, scope, "another fact in "+string(scope))
	}
	// Attached once and synchronously, with no loop to bring it back.
	if err := svc.Connect(ctx); err != nil {
		t.Fatal(err)
	}

	a := &Automation{svc: svc, done: make(chan struct{})}
	owed := a.runPass(ctx, &detachingExtractor{svc: svc})
	if owed == nil {
		t.Fatal("a pass whose backend was lost mid-pass reported that it ran")
	}
	if svc.SemanticEnabled() {
		t.Fatal("the extractor did not detach the backend")
	}
	fps := svc.loadFingerprints()
	ran := 0
	for _, scope := range scopes {
		if _, ok := fps[string(scope)]; ok {
			ran++
		}
	}
	// The scope being reorganized when the backend went finishes on the vectors it already
	// holds; the one after it is refused.
	if ran != 1 {
		t.Fatalf("%d scopes consolidated, want exactly the one in flight", ran)
	}
	for _, r := range mustList(t, svc) {
		if r.Area != "" {
			t.Fatalf("areas were refreshed after the backend was lost: %q on %q", r.Area, r.Text)
		}
	}
}

func mustList(t *testing.T, svc *Service) []memory.Record {
	t.Helper()
	recs, err := svc.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return recs
}
