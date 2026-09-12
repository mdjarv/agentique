package assistant

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mdjarv/agentique/backend/internal/memory"
)

// The preamble carries the pinned set and the INDEX, and nothing else from the
// store. That is the whole of the M2 policy change: the design it replaces
// guessed at relevance and injected the guess into every turn, and the guess was
// the noise. Anything a head has not asked for arrives as a label and a count.
func TestPreambleCarriesTheIndexAndThePinnedSetAndNoOtherBodies(t *testing.T) {
	ctx := context.Background()
	mem := &fakeMemory{
		pinned: []Fact{{
			ID: "pin-1", Text: "never push without asking",
			Category: memory.CategoryPreference, Source: memory.SourceHuman,
		}},
		index: []IndexLine{
			{Kind: IndexArea, Label: "reconnect drops", Size: 12,
				Scopes: []string{"the project riff", "everywhere"}},
			{Kind: IndexScope, Label: "the project riff", Size: 40},
		},
		// Reachable only through recall. If this sentence is in the preamble, the
		// pull has become a push again.
		found: []Fact{{ID: "f9", Text: "the worklets are emitted, never inlined"}},
	}
	head := &fakeHead{reply: "ok"}
	svc, _, _ := newTestService(t, WithHeadManager(head), WithMemory(mem))

	if _, err := svc.Say(ctx, SurfaceThread, "what do you know?"); err != nil {
		t.Fatalf("Say() = %v", err)
	}

	preamble := head.lastPreamble()
	for _, want := range []string{
		"What you remember",      // the section exists
		"never push without ask", // the pinned body, which is what pinned means
		"pin-1",                  // and its id, which is what a confirm takes
		"preference",             // and its category
		"reconnect drops",        // the area label
		"12 fact",                // its size
		"the project riff",       // the scope label
		"40 fact",                // its count
		"behind `recall`",        // and the sentence that says so
		"before you answer",      // when to reach for it
	} {
		if !strings.Contains(preamble, want) {
			t.Errorf("preamble is missing %q", want)
		}
	}
	if strings.Contains(preamble, "worklets are emitted") {
		t.Error("a fact reachable only through recall rode the preamble: the pull is a push again")
	}
	for _, verb := range []string{VerbRecall, VerbRemember, VerbConfirmMemory, VerbFlagMemory} {
		if !strings.Contains(preamble, verb) {
			t.Errorf("the preamble does not name %q", verb)
		}
	}
}

// No memory wired, no section. A head told it remembers things and then refused
// at every call is worse off than one that was never told.
func TestPreambleHasNoMemorySectionWithoutAMemory(t *testing.T) {
	ctx := context.Background()
	head := &fakeHead{reply: "ok"}
	svc, _, _ := newTestService(t, WithHeadManager(head))

	if _, err := svc.Say(ctx, SurfaceThread, "hello"); err != nil {
		t.Fatalf("Say() = %v", err)
	}
	if strings.Contains(head.lastPreamble(), "What you remember") {
		t.Error("the preamble promises a memory this server does not have")
	}
}

// An empty memory still gets the section: the verbs are in the table, and the
// head has to be told that writing is how anything ever gets in there.
func TestPreambleNamesAnEmptyMemory(t *testing.T) {
	ctx := context.Background()
	head := &fakeHead{reply: "ok"}
	svc, _, _ := newTestService(t, WithHeadManager(head), WithMemory(&fakeMemory{}))

	if _, err := svc.Say(ctx, SurfaceThread, "hello"); err != nil {
		t.Fatalf("Say() = %v", err)
	}
	preamble := head.lastPreamble()
	if !strings.Contains(preamble, "What you remember") {
		t.Fatal("an empty memory is still a memory and still needs its section")
	}
	if !strings.Contains(preamble, "nothing in it yet") {
		t.Errorf("preamble = %q, want it to say the memory is empty", preamble)
	}
}

// Captures come from the conversation and from notable journal entries, never
// from transcripts — so writing a notable entry is what stages one, rather than
// each writer remembering to.
func TestANotableEntryBecomesACapture(t *testing.T) {
	ctx := context.Background()
	mem := &fakeMemory{}
	svc, _, _ := newTestService(t, WithMemory(mem))

	if _, err := svc.Invoke(ctx, VerbNote,
		map[string]any{"text": "they want the deck to lead with what needs them"}); err != nil {
		t.Fatalf("Invoke(note) = %v", err)
	}

	captures := mem.captures()
	if len(captures) != 1 {
		t.Fatalf("staged %d captures, want 1", len(captures))
	}
	if captures[0].Text != "they want the deck to lead with what needs them" {
		t.Errorf("captured %q, want the note's own words", captures[0].Text)
	}
	// The assistant's own sentence, so plain capture tier.
	if captures[0].Source != memory.SourceCapture {
		t.Errorf("source = %q, want %q for the assistant's own words",
			captures[0].Source, memory.SourceCapture)
	}
}

// An untrusted notable entry is agent-written text about repository content
// nobody here authored. It is still staged — that is the point of a capture — and
// it is staged with its provenance, so consolidation can weigh it differently
// from something the operator said.
func TestAnUntrustedNotableEntryIsCapturedAsReported(t *testing.T) {
	ctx := context.Background()
	mem := &fakeMemory{}
	svc, _, _ := newTestService(t, WithMemory(mem))

	if _, err := svc.appendJournal(ctx, journalWrite{
		Kind:      JournalReport,
		SessionID: "s1",
		ProjectID: "p1",
		Summary:   "the reconnect test was already failing on master",
		Untrusted: true,
		Notable:   true,
	}); err != nil {
		t.Fatalf("appendJournal() = %v", err)
	}

	captures := mem.captures()
	if len(captures) != 1 {
		t.Fatalf("staged %d captures, want 1", len(captures))
	}
	if captures[0].Source != memory.SourceReported {
		t.Errorf("source = %q, want %q for agent-written text",
			captures[0].Source, memory.SourceReported)
	}
	if captures[0].ProjectID != "p1" {
		t.Errorf("project = %q, want the entry's own project scope", captures[0].ProjectID)
	}
}

// Notable is the mark that says "consolidation should look at this", so an entry
// without it stages nothing. Most of the journal is not notable — every turn end
// on every session writes one — and capturing all of it would be the transcript
// extraction this design took out, one layer up.
func TestAnOrdinaryEntryIsNotCaptured(t *testing.T) {
	ctx := context.Background()
	mem := &fakeMemory{}
	svc, _, _ := newTestService(t, WithMemory(mem))

	if _, err := svc.appendJournal(ctx, journalWrite{
		Kind:      JournalSessionFinished,
		SessionID: "s1",
		Summary:   "the tests pass",
		Untrusted: true,
	}); err != nil {
		t.Fatalf("appendJournal() = %v", err)
	}
	if got := mem.captures(); len(got) != 0 {
		t.Errorf("staged %v, want nothing from an entry nobody marked notable", got)
	}
}

// The journal entry that RECORDS a memory write is the one notable entry that is
// not itself captured. The fact is already in the store; staging a sentence
// saying a fact was stored would hand consolidation a meta-phrased second copy to
// judge against the first.
func TestAMemoryWritesOwnJournalEntryIsNotCaptured(t *testing.T) {
	ctx := context.Background()
	mem := &fakeMemory{}
	svc, _, _ := newTestService(t, WithMemory(mem))

	if _, err := svc.Invoke(ctx, VerbRemember, map[string]any{
		"text": "they read the rail before the deck", "category": "preference",
		"provenance": "operator",
	}); err != nil {
		t.Fatalf("Invoke(remember) = %v", err)
	}

	entries, err := svc.Journal(ctx, "", 10)
	if err != nil {
		t.Fatalf("Journal() = %v", err)
	}
	if len(entries) != 1 || !entries[0].Notable {
		t.Fatalf("journal = %+v, want one notable note", entries)
	}
	if got := mem.captures(); len(got) != 0 {
		t.Errorf("staged %v, want nothing: the fact is already in the store", got)
	}
	if len(mem.writes()) != 1 {
		t.Errorf("wrote %d facts, want the one remember", len(mem.writes()))
	}
}

// A capture that fails costs the index entry and never the journal. The journal
// is the record of what happened; memory is an index over it.
func TestACaptureFailureDoesNotFailTheJournalWrite(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(t, WithMemory(&failingCapture{}))

	if _, err := svc.Invoke(ctx, VerbNote, map[string]any{"text": "keep this"}); err != nil {
		t.Fatalf("Invoke(note) = %v", err)
	}
	entries, err := svc.Journal(ctx, "", 10)
	if err != nil {
		t.Fatalf("Journal() = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("journal has %d entries, want the note the capture could not stage", len(entries))
	}
}

// The two closed sets are the store's own, spelled once. A value here that the
// store does not know is a fact written under a category nothing will ever rank
// by, and a provenance it cannot map.
func TestTheClosedSetsAreTheStoresOwn(t *testing.T) {
	for _, c := range categories {
		if got, ok := parseCategory(string(c)); !ok || got != c {
			t.Errorf("parseCategory(%q) = %q, %v", c, got, ok)
		}
	}
	if _, ok := parseCategory("vibes"); ok {
		t.Error("parseCategory accepted a category the store does not have")
	}
	if got := ProvenanceOperator.Source(); got != memory.SourceHuman {
		t.Errorf("operator maps to %q, want %q", got, memory.SourceHuman)
	}
	if got := ProvenanceAssistant.Source(); got != memory.SourceAgent {
		t.Errorf("assistant maps to %q, want %q", got, memory.SourceAgent)
	}
	// Capture tier is what "staged, never injected" means, and both capture
	// sources have to answer it — a comparison naming only one is how agent
	// written text gets injected as ground truth.
	if !memory.SourceReported.Staged() || !memory.SourceCapture.Staged() {
		t.Error("a capture-tier source does not report itself as staged")
	}
	if memory.SourceHuman.Staged() || memory.SourceAgent.Staged() ||
		memory.SourceConsolidated.Staged() {
		t.Error("a durable source reports itself as staged")
	}
}

// failingCapture is a memory whose staging always fails.
type failingCapture struct{ fakeMemory }

func (*failingCapture) Capture(context.Context, string, string, memory.Source) error {
	return context.DeadlineExceeded
}

// A note about a session is a note about that session's repository, and the capture
// it stages has to land in that repository's scope. The global scope means "true
// everywhere", which is the one filing mistake `remember` refuses to make and the one
// nobody can see afterwards from the fact itself.
//
// The project is resolved FROM the session the head named, so there is no second
// argument that can disagree with the first.
func TestANoteAboutASessionIsCapturedInItsProject(t *testing.T) {
	ctx := context.Background()
	mem := &fakeMemory{}
	dir := &fakeDirectory{sessions: []SessionRow{
		{ID: "s1", Name: "Reconnect Drops", ProjectName: "riff", ProjectID: "p1"},
	}}
	svc, _, _ := newTestService(t, WithMemory(mem), WithDirectory(dir))

	if _, err := svc.Invoke(ctx, VerbNote, map[string]any{
		"text":       "the reconnect only drops on the hands-free route",
		"session_id": "s1",
	}); err != nil {
		t.Fatalf("Invoke(note) = %v", err)
	}

	captures := mem.captures()
	if len(captures) != 1 {
		t.Fatalf("staged %d captures, want 1", len(captures))
	}
	if captures[0].ProjectID != "p1" {
		t.Errorf("project = %q, want the session's own project — the global scope is "+
			"\"true everywhere\"", captures[0].ProjectID)
	}
}

// A note about nothing in particular stays global, and so does one naming a session
// this machine does not hold: an unplaceable note is a note with no project.
func TestANoteWithNoPlaceStaysGlobal(t *testing.T) {
	ctx := context.Background()
	mem := &fakeMemory{}
	dir := &fakeDirectory{sessions: []SessionRow{
		{ID: "remote", Name: "Invoice Importer", ProjectName: "billing", MachineID: "laptop"},
	}}
	svc, _, _ := newTestService(t, WithMemory(mem), WithDirectory(dir))

	if _, err := svc.Invoke(ctx, VerbNote, map[string]any{
		"text": "they prefer the deck to lead with what needs them",
	}); err != nil {
		t.Fatalf("Invoke(note) = %v", err)
	}
	if _, err := svc.Invoke(ctx, VerbNote, map[string]any{
		"text":       "the importer is on the laptop",
		"session_id": "remote",
	}); err != nil {
		t.Fatalf("Invoke(note) = %v", err)
	}

	for i, capture := range mem.captures() {
		if capture.ProjectID != "" {
			t.Errorf("capture %d filed under %q, want the global scope", i, capture.ProjectID)
		}
	}
}

// An unreadable memory is not an empty one, and the preamble has to say which.
// Both halves come from the same store read, so one failure is the whole memory
// going dark — and a head told its memory is empty says so to the operator and then
// remembers facts it already holds a second time.
func TestAnUnreadableMemoryIsNotReportedAsEmpty(t *testing.T) {
	ctx := context.Background()
	mem := &fakeMemory{pinnedErr: errors.New("store unavailable"), indexErr: errors.New("store unavailable")}
	svc, _, _ := newTestService(t, WithMemory(mem))

	pinned, index, unread := svc.memoryBriefing(ctx)
	if len(pinned) != 0 || len(index) != 0 {
		t.Fatalf("a failed read must degrade to nothing, got %+v / %+v", pinned, index)
	}
	if !unread {
		t.Fatal("a failed read must report itself as unread")
	}

	instruction := HeadInstruction(HeadBriefing{HasMemory: true, MemoryUnread: unread})
	if strings.Contains(instruction, "There is nothing in it yet") {
		t.Error("the preamble told the head an unreadable store was empty")
	}
	if !strings.Contains(instruction, "could not be read") {
		t.Error("the preamble never said the store could not be read")
	}

	// And an actually-empty store still reads as empty: the third state must not
	// swallow the second.
	empty := HeadInstruction(HeadBriefing{HasMemory: true})
	if !strings.Contains(empty, "There is nothing in it yet") {
		t.Error("an empty store stopped reading as empty")
	}
}

// The briefing runs before the turn's own deadline exists, while the conversation's
// one turn lock is held and the composer is shut, so it carries its own. A store that
// never answers costs the head its index rather than the start.
func TestTheMemoryBriefingIsBounded(t *testing.T) {
	prev := memoryBriefingBudget
	memoryBriefingBudget = 50 * time.Millisecond
	t.Cleanup(func() { memoryBriefingBudget = prev })

	mem := &fakeMemory{blockReads: true}
	svc, _, _ := newTestService(t, WithMemory(mem))

	done := make(chan bool, 1)
	go func() {
		_, _, unread := svc.memoryBriefing(context.Background())
		done <- unread
	}()

	select {
	case unread := <-done:
		if !unread {
			t.Error("a briefing that timed out must report itself as unread")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the briefing held the head's start on a store that never answered")
	}
}
