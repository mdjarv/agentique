package server

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/assistant"
	"github.com/mdjarv/agentique/backend/internal/brain"
	"github.com/mdjarv/agentique/backend/internal/memory"
	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/mdjarv/agentique/backend/internal/testutil"
)

// The two switches are independent, and the verb table is what a head is told
// exists — so "the brain is on" has to be observable in the table and nothing
// else. A head offered `recall` on a server with no store would refuse at every
// call and learn to stop asking.
func TestTheHeadsVerbsCarryMemoryOnlyWhenBothFlagsAreOn(t *testing.T) {
	memoryVerbs := []string{
		assistant.VerbRecall,
		assistant.VerbRemember,
		assistant.VerbConfirmMemory,
		assistant.VerbFlagMemory,
	}

	both := serveForMemory(t, true)
	names := verbNames(both)
	for _, verb := range memoryVerbs {
		if !slices.Contains(names, verb) {
			t.Errorf("%q is not in the head's table with the assistant and the brain both on", verb)
		}
	}

	assistantOnly := serveForMemory(t, false)
	names = verbNames(assistantOnly)
	for _, verb := range memoryVerbs {
		if slices.Contains(names, verb) {
			t.Errorf("%q is in the head's table with no brain behind it", verb)
		}
	}
	// The rest of the table is unaffected: memory is a collaborator, not a gate
	// on the assistant itself.
	if !slices.Contains(names, assistant.VerbRunPrompt) {
		t.Error("the assistant lost its own verbs when the brain was switched off")
	}
}

// serveForMemory stands a server up with the assistant on and the brain in a
// given position, and answers with its assistant service.
func serveForMemory(t *testing.T, withBrain bool) *assistant.Service {
	t.Helper()

	db, queries := testutil.SetupDB(t)
	cfg := Config{DB: db, ExperimentalAssistant: true}
	if withBrain {
		cfg.BrainEnabled = true
		cfg.BrainDir = filepath.Join(t.TempDir(), "brain")
	}
	srv, err := New(queries, cfg)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	t.Cleanup(srv.Shutdown)
	if srv.assistantSvc == nil {
		t.Fatal("the assistant service was not built with the switch on")
	}
	return srv.assistantSvc
}

func verbNames(svc *assistant.Service) []string {
	verbs := svc.Verbs()
	out := make([]string, 0, len(verbs))
	for _, verb := range verbs {
		out = append(out, verb.Name)
	}
	return out
}

// newMemoryOverBrain builds the adapter over a real brain, because what is being
// tested is the mapping between two vocabularies and a stub on either side would
// test the stub.
func newMemoryOverBrain(t *testing.T) (*assistantMemory, *store.Queries) {
	t.Helper()

	_, queries := testutil.SetupDB(t)
	b, err := brain.New(context.Background(), brain.Config{Dir: filepath.Join(t.TempDir(), "brain")})
	if err != nil {
		t.Fatalf("brain.New() = %v", err)
	}
	return newAssistantMemory(b, queries), queries
}

// The index is labels and counts, and the counts are of facts `recall` can
// actually return — so a capture, which is never recalled, is never counted.
// An index whose numbers do not survive being asked about is worse than none.
func TestIndexCountsDurableFactsAndNamesTheProject(t *testing.T) {
	ctx := context.Background()
	mem, queries := newMemoryOverBrain(t)
	project := testutil.SeedProject(t, queries, "riff", t.TempDir())

	if _, err := mem.Remember(ctx, "they deploy on Fridays anyway", memory.CategoryPreference,
		assistant.ProvenanceOperator, project.ID); err != nil {
		t.Fatalf("Remember() = %v", err)
	}
	if _, err := mem.Remember(ctx, "the primary database is sqlite", memory.CategoryFact,
		assistant.ProvenanceAssistant, ""); err != nil {
		t.Fatalf("Remember() = %v", err)
	}
	if err := mem.Capture(ctx, project.ID, "an agent said the reconnect test was already red",
		memory.SourceReported); err != nil {
		t.Fatalf("Capture() = %v", err)
	}

	lines, err := mem.Index(ctx)
	if err != nil {
		t.Fatalf("Index() = %v", err)
	}

	sizes := map[string]int{}
	for _, line := range lines {
		if line.Kind == assistant.IndexScope {
			sizes[line.Label] = line.Size
		}
	}
	if got := sizes["the project riff"]; got != 1 {
		t.Errorf("the project's line counts %d, want 1 — the capture must not be counted", got)
	}
	if got := sizes["everywhere"]; got != 1 {
		t.Errorf("the global line counts %d, want 1", got)
	}
	// A scope reaches the assistant already readable: a head shown
	// `project:8f2c…` can do nothing with it, and never passes a scope back.
	for label := range sizes {
		if strings.HasPrefix(label, "project:") {
			t.Errorf("scope %q reached the assistant as a raw scope string", label)
		}
	}
}

// Pinned is the other half of what the preamble carries, and it is the only
// place a fact body rides a preamble at all.
func TestPinnedCarriesTheOperatorsStandingFacts(t *testing.T) {
	ctx := context.Background()
	mem, _ := newMemoryOverBrain(t)

	// identity is pinned on the way in, which is what makes the category worth
	// refusing to guess.
	if _, err := mem.Remember(ctx, "their name is Mathias", memory.CategoryIdentity,
		assistant.ProvenanceOperator, ""); err != nil {
		t.Fatalf("Remember() = %v", err)
	}
	if _, err := mem.Remember(ctx, "the build tool is just", memory.CategoryProject,
		assistant.ProvenanceOperator, ""); err != nil {
		t.Fatalf("Remember() = %v", err)
	}

	facts, err := mem.Pinned(ctx)
	if err != nil {
		t.Fatalf("Pinned() = %v", err)
	}
	if len(facts) != 1 || !strings.Contains(facts[0].Text, "Mathias") {
		t.Fatalf("pinned = %+v, want only the identity fact", facts)
	}
	if facts[0].ID == "" {
		t.Error("a pinned fact reached the head with no id: nothing could confirm or flag it")
	}
	if facts[0].Source != memory.SourceHuman {
		t.Errorf("source = %q, want %q for something the operator said",
			facts[0].Source, memory.SourceHuman)
	}
	if facts[0].Confidence == "" {
		t.Error("a fact reached the head with no confidence tier")
	}
}

// Search is the pull, and a staged capture is not part of what it may return.
// That is the whole safety property of SourceReported: agent-written text about
// repository content is kept for consolidation to judge and never handed back as
// though the operator had said it.
func TestSearchNeverReturnsACapture(t *testing.T) {
	ctx := context.Background()
	mem, _ := newMemoryOverBrain(t)

	// Staged first, so it is genuinely a new capture rather than a reinforcement
	// of the durable fact below.
	if err := mem.Capture(ctx, "", "the reconnect socket drops on every rebuild of the worklet",
		memory.SourceReported); err != nil {
		t.Fatalf("Capture() = %v", err)
	}
	if _, err := mem.Remember(ctx, "the reconnect socket work lives on the retry branch",
		memory.CategoryProject, assistant.ProvenanceOperator, ""); err != nil {
		t.Fatalf("Remember() = %v", err)
	}

	// A query written at the CAPTURE, so the assertion is not vacuous: the fact it
	// comes back with is the durable one, and the closer match is the one held
	// back.
	facts, err := mem.Search(ctx, "reconnect socket drops rebuild worklet", 8)
	if err != nil {
		t.Fatalf("Search() = %v", err)
	}
	if len(facts) == 0 {
		t.Fatal("Search() answered nothing at all: this test would pass for the wrong reason")
	}
	for _, fact := range facts {
		if fact.Source.Staged() {
			t.Errorf("recall returned a staged fact: %+v", fact)
		}
		if strings.Contains(fact.Text, "drops on every rebuild") {
			t.Error("a reported capture came back from recall as though it were a fact")
		}
	}
}

// Confirm and flag are the conversational outcome signal: the accept half makes a
// fact ground truth, the contradict half weakens it into the review queue and
// deletes nothing.
func TestConfirmAndFlagMoveTheRecord(t *testing.T) {
	ctx := context.Background()
	mem, _ := newMemoryOverBrain(t)

	fact, err := mem.Remember(ctx, "they prefer tabs", memory.CategoryPreference,
		assistant.ProvenanceAssistant, "")
	if err != nil {
		t.Fatalf("Remember() = %v", err)
	}
	if fact.Source != memory.SourceAgent {
		t.Fatalf("source = %q, want the assistant's own conclusion", fact.Source)
	}

	if err := mem.Confirm(ctx, fact.ID); err != nil {
		t.Fatalf("Confirm() = %v", err)
	}
	confirmed, err := mem.brain.Get(ctx, fact.ID)
	if err != nil {
		t.Fatalf("Get() = %v", err)
	}
	if confirmed.Source != memory.SourceHuman {
		t.Errorf("source after confirm = %q, want %q — a confirmation is the operator's own",
			confirmed.Source, memory.SourceHuman)
	}

	other, err := mem.Remember(ctx, "the tests run on every push", memory.CategoryFact,
		assistant.ProvenanceAssistant, "")
	if err != nil {
		t.Fatalf("Remember() = %v", err)
	}
	if err := mem.Flag(ctx, other.ID, "they moved to a nightly run"); err != nil {
		t.Fatalf("Flag() = %v", err)
	}
	flagged, err := mem.brain.Get(ctx, other.ID)
	if err != nil {
		t.Fatalf("Get() = %v — a flag must not delete the fact", err)
	}
	if flagged.ReviewNote == "" {
		t.Error("a flagged fact carries no review note, so nobody can act on it")
	}
}

// The capture door refuses a durable source. Its whole contract is "staged, never
// injected", and a human or agent source arriving here would write an injectable
// fact through a door that promises it cannot.
func TestCaptureRefusesADurableSource(t *testing.T) {
	ctx := context.Background()
	mem, _ := newMemoryOverBrain(t)

	if err := mem.Capture(ctx, "", "something", memory.SourceHuman); err == nil {
		t.Error("Capture() accepted a durable source")
	}
}

// The index has two halves and one budget, and the half that always exists must
// survive it. Areas are unbounded — a clustering pass invents as many as the corpus
// supports — so an index truncated as one list left the head with topic labels and
// nothing naming a project scope it could ask about.
func TestIndexBudgetKeepsTheScopeLines(t *testing.T) {
	areas := make([]assistant.IndexLine, 0, 200)
	for i := 0; i < 200; i++ {
		areas = append(areas, assistant.IndexLine{Kind: assistant.IndexArea, Label: "area", Size: 200 - i})
	}
	scoped := []assistant.IndexLine{
		{Kind: assistant.IndexScope, Label: "everywhere", Size: 9},
		{Kind: assistant.IndexScope, Label: "the project riff", Size: 4},
	}

	lines := budgetIndexLines(areas, scoped)
	if len(lines) != maxAssistantIndexLines {
		t.Fatalf("index is %d lines, want the cap of %d", len(lines), maxAssistantIndexLines)
	}
	kept := 0
	for _, line := range lines {
		if line.Kind == assistant.IndexScope {
			kept++
		}
	}
	if kept != len(scoped) {
		t.Errorf("%d of %d scope lines survived 200 areas", kept, len(scoped))
	}
	// The areas that survive are the biggest, on the order they arrive in.
	if lines[0].Size != 200 {
		t.Errorf("first area has size %d, want the largest", lines[0].Size)
	}

	// More scopes than the whole budget: they lose their smallest, and no area rides
	// a budget that is already spent.
	many := make([]assistant.IndexLine, 0, 60)
	for i := 0; i < 60; i++ {
		many = append(many, assistant.IndexLine{Kind: assistant.IndexScope, Label: "p", Size: 60 - i})
	}
	lines = budgetIndexLines(areas, many)
	if len(lines) != maxAssistantIndexLines {
		t.Fatalf("index is %d lines, want the cap of %d", len(lines), maxAssistantIndexLines)
	}
	for _, line := range lines {
		if line.Kind != assistant.IndexScope {
			t.Fatal("an area took room the scope lines had already spent")
		}
	}
}

// Pinned facts ride every fresh head WITH their bodies, and the head can add to that
// set itself: `remember(category: "identity")` pins on the way in. So the read is
// capped, and what it keeps is what was touched most recently.
func TestPinnedIsCappedForThePreamble(t *testing.T) {
	ctx := context.Background()
	mem, _ := newMemoryOverBrain(t)

	for i := 0; i < maxAssistantPinnedFacts+5; i++ {
		// Deliberately unalike: the store reinforces a near-duplicate instead of
		// writing a second fact, so forty variations on one sentence are one fact.
		text := fmt.Sprintf("vorbex%d kelmar%d thrunable%d quillon%d", i, i, i, i)
		if _, err := mem.Remember(ctx, text, memory.CategoryIdentity,
			assistant.ProvenanceOperator, ""); err != nil {
			t.Fatalf("Remember() = %v", err)
		}
	}

	facts, err := mem.Pinned(ctx)
	if err != nil {
		t.Fatalf("Pinned() = %v", err)
	}
	if len(facts) != maxAssistantPinnedFacts {
		t.Fatalf("pinned set is %d facts, want the cap of %d", len(facts), maxAssistantPinnedFacts)
	}
}
