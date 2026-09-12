package assistant

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/memory"
	"github.com/mdjarv/agentique/backend/internal/usage"
)

// baseVerbTiers is the table on a server with no long-term memory.
func baseVerbTiers() map[string]Tier {
	return map[string]Tier{
		VerbOrientation:      TierRead,
		VerbListSessions:     TierRead,
		VerbFindSession:      TierRead,
		VerbSummarizeSession: TierRead,
		VerbListProjects:     TierRead,
		VerbAllowances:       TierRead,
		VerbJournal:          TierRead,

		VerbCreateSession:   TierContained,
		VerbRunPrompt:       TierContained,
		VerbFollowSession:   TierContained,
		VerbUnfollowSession: TierContained,
		VerbNote:            TierContained,
		VerbDigest:          TierContained,

		VerbMergeSession:    TierUncontained,
		VerbRebaseSession:   TierUncontained,
		VerbArchiveSession:  TierUncontained,
		VerbDeleteSession:   TierUncontained,
		VerbReclaimSession:  TierUncontained,
		VerbDissolveChannel: TierUncontained,
		VerbSetSessionModel: TierUncontained,
		VerbSetSessionMode:  TierUncontained,
	}
}

// memoryVerbTiers is what a wired [Memory] adds: one pull and three writes.
//
// The pull is read tier because it writes no fact. The three writes are
// contained — they reach the store and nothing else, and each is journaled.
func memoryVerbTiers() map[string]Tier {
	return map[string]Tier{
		VerbRecall:        TierRead,
		VerbRemember:      TierContained,
		VerbConfirmMemory: TierContained,
		VerbFlagMemory:    TierContained,
	}
}

// assertVerbTable checks the table is exactly want, and that every member's tier
// and handler agree.
func assertVerbTable(t *testing.T, svc *Service, want map[string]Tier) {
	t.Helper()

	got := svc.Verbs()
	if len(got) != len(want) {
		t.Fatalf("table has %d verbs, want %d — a new verb needs its tier decided here", len(got), len(want))
	}

	for _, verb := range got {
		tier, known := want[verb.Name]
		if !known {
			t.Errorf("%q is in the table and not in this test: decide its tier", verb.Name)
			continue
		}
		if verb.Tier != tier {
			t.Errorf("%q is %s, want %s", verb.Name, verb.Tier, tier)
		}
		if verb.Description == "" {
			t.Errorf("%q has no description — it is what a head is told the verb does", verb.Name)
		}

		// Every verb is callable, uncontained ones included: what an
		// uncontained verb's handler does is write a proposal, and the tier is
		// what keeps it from doing anything else.
		if !verb.HasHandler() {
			t.Errorf("%q is %s and has no handler", verb.Name, verb.Tier)
		}
	}
}

// The table is closed, and these are the members. A verb added without a
// decision about its tier is the failure this guards: the tier is the whole
// containment story, so it cannot be defaulted.
func TestVerbTableIsClosedAndEveryVerbHasATier(t *testing.T) {
	svc, _, _ := newTestService(t)
	assertVerbTable(t, svc, baseVerbTiers())
}

// A verb that cannot work must not be offered. The table is what a head is told
// exists, in a section of its instruction saying nothing outside the list is
// real, so four verbs that answer "I have no memory" to every call would teach it
// to stop asking — and would be four tool schemas every turn pays for.
func TestMemoryVerbsExistOnlyWithAMemory(t *testing.T) {
	ctx := context.Background()

	off, _, _ := newTestService(t)
	for name := range memoryVerbTiers() {
		if _, listed := off.Verb(name); listed {
			t.Errorf("%q is in the table with no memory wired", name)
		}
		if _, err := off.Invoke(ctx, name, nil); !errors.Is(err, ErrUnknownVerb) {
			t.Errorf("Invoke(%q) with no memory = %v, want ErrUnknownVerb", name, err)
		}
	}

	on, _, _ := newTestService(t, WithMemory(&fakeMemory{}))
	want := baseVerbTiers()
	for name, tier := range memoryVerbTiers() {
		want[name] = tier
	}
	assertVerbTable(t, on, want)
}

// The pull is the only way a fact reaches a turn, so the verb has to reach the
// store and hand back the ids a confirm or a flag will need.
func TestRecallPullsFactsWithTheirIds(t *testing.T) {
	mem := &fakeMemory{found: []Fact{
		{ID: "f1", Text: "they deploy on Fridays anyway", Category: memory.CategoryPreference,
			Source: memory.SourceHuman},
	}}
	svc, _, _ := newTestService(t, WithMemory(mem))

	payload, err := svc.Invoke(context.Background(), VerbRecall, map[string]any{"query": "deploy policy"})
	if err != nil {
		t.Fatalf("Invoke() = %v", err)
	}
	facts, _ := payload["facts"].([]map[string]any)
	if len(facts) != 1 || facts[0]["id"] != "f1" {
		t.Fatalf("facts = %v, want the one fact with its id", payload["facts"])
	}
	if mem.queries[0] != "deploy policy" {
		t.Errorf("query = %q, want the head's own words", mem.queries[0])
	}
	// A recalled fact whose source is "reported" is agent-written text about a
	// repository, so the answer has to say what quoting means here.
	if note, _ := payload["note"].(string); !strings.Contains(note, "reported") {
		t.Errorf("note = %q, want it to name the untrusted provenance", note)
	}
}

func TestRecallOnAnEmptyMemorySaysSo(t *testing.T) {
	svc, _, _ := newTestService(t, WithMemory(&fakeMemory{}))

	payload, err := svc.Invoke(context.Background(), VerbRecall, map[string]any{"query": "anything"})
	if err != nil {
		t.Fatalf("Invoke() = %v", err)
	}
	if _, refused := payload["error"]; refused {
		t.Fatalf("payload = %v, want an answer rather than a refusal", payload)
	}
	if note, _ := payload["note"].(string); !strings.Contains(note, "Nothing on record") {
		t.Errorf("note = %q, want it to say there is nothing rather than invent something", note)
	}
}

// The provenance enum is the whole trust story of a written fact, and it maps
// onto the store's own sources in one place.
func TestRememberMapsProvenance(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct {
		provenance Provenance
		want       memory.Source
	}{
		{ProvenanceOperator, memory.SourceHuman},
		{ProvenanceAssistant, memory.SourceAgent},
	} {
		mem := &fakeMemory{}
		svc, _, _ := newTestService(t, WithMemory(mem))

		payload, err := svc.Invoke(ctx, VerbRemember, map[string]any{
			"text":       "they want the rail quieter",
			"category":   string(memory.CategoryPreference),
			"provenance": string(tc.provenance),
		})
		if err != nil {
			t.Fatalf("Invoke() = %v", err)
		}
		if payload["remembered"] != true {
			t.Fatalf("payload = %v, want the fact kept", payload)
		}

		writes := mem.writes()
		if len(writes) != 1 {
			t.Fatalf("wrote %d facts, want 1", len(writes))
		}
		if writes[0].Provenance != tc.provenance {
			t.Errorf("provenance = %q, want %q", writes[0].Provenance, tc.provenance)
		}
		if got := writes[0].Provenance.Source(); got != tc.want {
			t.Errorf("%q maps to %q, want %q", tc.provenance, got, tc.want)
		}
		if writes[0].Category != memory.CategoryPreference {
			t.Errorf("category = %q, want the one it was given", writes[0].Category)
		}
		if writes[0].ProjectID != "" {
			t.Errorf("project = %q, want the global scope when no project was named", writes[0].ProjectID)
		}
	}
}

// Neither category nor provenance is defaulted. Both are silent failures if
// guessed: an identity fact is pinned on the way in, and "the operator said it"
// is the one claim in this store that outranks everything else.
func TestRememberRefusesAnInventedCategoryOrProvenance(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		args map[string]any
		want string
	}{
		{"category", map[string]any{"text": "x y z", "category": "vibes", "provenance": "operator"}, "bad-category"},
		{"provenance", map[string]any{"text": "x y z", "category": "fact", "provenance": "probably"}, "bad-provenance"},
		{"no provenance", map[string]any{"text": "x y z", "category": "fact"}, "bad-provenance"},
	} {
		mem := &fakeMemory{}
		svc, _, _ := newTestService(t, WithMemory(mem))

		payload, err := svc.Invoke(ctx, VerbRemember, tc.args)
		if err != nil {
			t.Fatalf("%s: Invoke() = %v", tc.name, err)
		}
		if reason, _ := payload[reasonKey].(string); reason != tc.want {
			t.Errorf("%s: reason = %q, want %q", tc.name, reason, tc.want)
		}
		if len(mem.writes()) != 0 {
			t.Errorf("%s: a refused remember wrote a fact anyway", tc.name)
		}
	}
}

// A named project is resolved through the directory's own list, and never
// guessed: global means "true everywhere", so filing a project's fact there is
// the one mistake that cannot be seen from the answer.
func TestRememberResolvesAProjectByName(t *testing.T) {
	ctx := context.Background()
	dir := &fakeDirectory{projects: []ProjectRow{
		{ID: "p1", Name: "riff", Slug: "riff"},
		{ID: "p2", Name: "agentique", Slug: "agentique"},
	}}
	mem := &fakeMemory{}
	svc, _, _ := newTestService(t, WithDirectory(dir), WithMemory(mem))

	if _, err := svc.Invoke(ctx, VerbRemember, map[string]any{
		"text": "the worklets live in public/", "category": "project",
		"provenance": "operator", "project": "riff",
	}); err != nil {
		t.Fatalf("Invoke() = %v", err)
	}
	writes := mem.writes()
	if len(writes) != 1 || writes[0].ProjectID != "p1" {
		t.Fatalf("writes = %+v, want the fact filed under p1", writes)
	}

	payload, err := svc.Invoke(ctx, VerbRemember, map[string]any{
		"text": "something", "category": "fact", "provenance": "operator", "project": "nothing here",
	})
	if err != nil {
		t.Fatalf("Invoke() = %v", err)
	}
	if reason, _ := payload[reasonKey].(string); reason != "scope-project-unrecognised" {
		t.Errorf("reason = %q, want scope-project-unrecognised", reason)
	}
	if len(mem.writes()) != 1 {
		t.Error("an unrecognised project fell back to the global scope instead of refusing")
	}
}

// Confirm and flag are the conversational outcome signal, and both are journaled
// so the conversation carries a record of what moved.
func TestConfirmAndFlagReachTheStoreAndTheJournal(t *testing.T) {
	ctx := context.Background()
	mem := &fakeMemory{}
	svc, _, _ := newTestService(t, WithMemory(mem))

	if _, err := svc.Invoke(ctx, VerbConfirmMemory, map[string]any{"id": "f1"}); err != nil {
		t.Fatalf("Invoke(confirm) = %v", err)
	}
	if len(mem.confirmed) != 1 || mem.confirmed[0] != "f1" {
		t.Errorf("confirmed = %v, want the one id", mem.confirmed)
	}

	if _, err := svc.Invoke(ctx, VerbFlagMemory,
		map[string]any{"id": "f2", "reason": "they moved off sqlite"}); err != nil {
		t.Fatalf("Invoke(flag) = %v", err)
	}
	if len(mem.flagged) != 1 || mem.flagged[0][1] != "they moved off sqlite" {
		t.Errorf("flagged = %v, want the id and the reason", mem.flagged)
	}

	entries, err := svc.Journal(ctx, "", 10)
	if err != nil {
		t.Fatalf("Journal() = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("journal has %d entries, want one per write", len(entries))
	}
	for _, entry := range entries {
		if entry.Kind != JournalNote || !entry.Notable {
			t.Errorf("entry = %+v, want a notable note", entry)
		}
	}
}

// A flag with no reason is a review-queue row nobody can act on.
func TestFlagMemoryNeedsAReason(t *testing.T) {
	mem := &fakeMemory{}
	svc, _, _ := newTestService(t, WithMemory(mem))

	payload, err := svc.Invoke(context.Background(), VerbFlagMemory, map[string]any{"id": "f1"})
	if err != nil {
		t.Fatalf("Invoke() = %v", err)
	}
	if reason, _ := payload[reasonKey].(string); reason != "no-flag-reason" {
		t.Errorf("reason = %q, want no-flag-reason", reason)
	}
	if len(mem.flagged) != 0 {
		t.Error("a reasonless flag reached the store")
	}
}

// With no [Actions] wired, an uncontained verb refuses in WORDS and writes
// nothing. A proposal is a claim that the facts were checked, and there is
// nothing here to check them with.
func TestUncontainedVerbsRefuseWithoutActions(t *testing.T) {
	svc, queries, _ := newTestService(t)

	for _, verb := range svc.Verbs() {
		if verb.Tier != TierUncontained {
			continue
		}
		payload, err := svc.Invoke(context.Background(), verb.Name, map[string]any{
			"rationale": "because", "session_id": "s1", "channel_id": "c1",
		})
		if err != nil {
			t.Fatalf("Invoke(%q) = %v, want a refusal payload", verb.Name, err)
		}
		if _, refused := payload["error"]; !refused {
			t.Errorf("Invoke(%q) = %v, want a refusal", verb.Name, payload)
		}
	}

	rows, err := queries.ListAssistantProposals(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListAssistantProposals() = %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("%d proposals were written with nothing to check them", len(rows))
	}
}

// The refusal a head reads carries no internal token, and the token is what
// the log gets. Both halves matter: "an empty session and no log line" is the
// same picture for five different causes.
func TestToolHandlerStripsTheReason(t *testing.T) {
	svc, _, _ := newTestService(t)

	payload := svc.ToolHandler(context.Background(), VerbMergeSession, nil)
	if _, leaked := payload[reasonKey]; leaked {
		t.Error("the refusal reason reached the model")
	}
	if _, refused := payload["error"]; !refused {
		t.Fatalf("payload = %v, want a refusal", payload)
	}

	raw, err := svc.Invoke(context.Background(), "no_such_verb", nil)
	if raw != nil || err == nil {
		t.Fatal("Invoke must return the error rather than a refusal payload")
	}
}

func TestUnknownVerbIsRefusedNotPerformed(t *testing.T) {
	svc, _, _ := newTestService(t)

	if _, err := svc.Invoke(context.Background(), "rm_rf", nil); !errors.Is(err, ErrUnknownVerb) {
		t.Errorf("Invoke() = %v, want ErrUnknownVerb", err)
	}
	payload := svc.ToolHandler(context.Background(), "rm_rf", nil)
	if _, refused := payload["error"]; !refused {
		t.Errorf("payload = %v, want a refusal", payload)
	}
}

// Every read degrades to an answer rather than an error when nothing is wired,
// which is the rule the directory has always had.
func TestReadVerbsRefuseInWordsWithNothingWired(t *testing.T) {
	svc, _, _ := newTestService(t)

	for _, name := range []string{VerbOrientation, VerbListSessions, VerbFindSession,
		VerbSummarizeSession, VerbListProjects, VerbAllowances} {
		payload, err := svc.Invoke(context.Background(), name, map[string]any{"query": "x", "session_id": "s"})
		if err != nil {
			t.Errorf("Invoke(%q) = %v, want a refusal payload", name, err)
			continue
		}
		if _, refused := payload["error"]; !refused {
			t.Errorf("Invoke(%q) = %v, want a refusal", name, payload)
		}
	}
}

func TestListSessionsNormalisesAnInventedFilter(t *testing.T) {
	dir := &fakeDirectory{sessions: []SessionRow{{ID: "s1", Name: "Reconnect Drops", ProjectName: "riff"}}}
	svc, _, _ := newTestService(t, WithDirectory(dir))

	payload, err := svc.Invoke(context.Background(), VerbListSessions, map[string]any{"filter": "whatever"})
	if err != nil {
		t.Fatalf("Invoke() = %v", err)
	}
	if payload["filter"] != FilterRecent {
		t.Errorf("filter = %v, want %q — an unrecognised filter must not answer with nothing",
			payload["filter"], FilterRecent)
	}
	rows, _ := payload["sessions"].([]map[string]any)
	if len(rows) != 1 || rows[0]["name"] != "Reconnect Drops in riff" {
		t.Errorf("sessions = %v, want the row named with its project", payload["sessions"])
	}
}

// It ranks and never picks: two plausible candidates come back as two.
func TestFindSessionNeverPicks(t *testing.T) {
	dir := &fakeDirectory{sessions: []SessionRow{
		{ID: "s1", Name: "Voice Work", ProjectName: "riff"},
		{ID: "s2", Name: "Voice Work", ProjectName: "agentique"},
	}}
	svc, _, _ := newTestService(t, WithDirectory(dir))

	payload, err := svc.Invoke(context.Background(), VerbFindSession, map[string]any{"query": "voice work"})
	if err != nil {
		t.Fatalf("Invoke() = %v", err)
	}
	if payload["top_is_clear"] != false {
		t.Error("two identically named sessions must not produce a clear winner")
	}
	if rows, _ := payload["candidates"].([]map[string]any); len(rows) != 2 {
		t.Errorf("candidates = %v, want both", payload["candidates"])
	}
}

// An unknown percentage is not zero, and reporting it as one would be a number
// nobody can check.
func TestAllowancesDropsUnknownWindows(t *testing.T) {
	svc, _, _ := newTestService(t, WithAllowances(fakeAllowances{doc: usage.Document{
		Agents: []usage.Agent{{Name: "Claude", Limits: []usage.Limit{
			{Label: "Week", Percent: 0.42, Severity: "normal", ResetsAt: "2026-02-01T00:00:00Z"},
			{Label: "Opus week", Percent: usage.Unknown},
		}}},
	}}))

	payload, err := svc.Invoke(context.Background(), VerbAllowances, nil)
	if err != nil {
		t.Fatalf("Invoke() = %v", err)
	}
	windows, _ := payload["windows"].([]map[string]any)
	if len(windows) != 1 || windows[0]["window"] != "Week" {
		t.Fatalf("windows = %v, want only the readable one", payload["windows"])
	}
	// Costs are irrelevant here and stay out of every surface, so the one place
	// cost appears in this answer is the instruction not to mention it.
	if note, _ := payload["note"].(string); !strings.Contains(note, "Never mention cost") {
		t.Errorf("note = %q, want it to rule cost out", note)
	}
}

// Dispatch is local-only: the report registry is local, so a remote run would
// report into nothing.
func TestRunPromptRefusesARemoteSession(t *testing.T) {
	dir := &fakeDirectory{sessions: []SessionRow{
		{ID: "remote-1", Name: "Voice Work", ProjectName: "riff", MachineID: "laptop", MachineName: "laptop"},
	}}
	disp := &fakeDispatcher{}
	svc, _, _ := newTestService(t, WithDirectory(dir), WithDispatcher(disp))

	payload, err := svc.Invoke(context.Background(), VerbRunPrompt,
		map[string]any{"session_id": "remote-1", "prompt": "do the thing"})
	if err != nil {
		t.Fatalf("Invoke() = %v", err)
	}
	if reason, _ := payload[reasonKey].(string); reason != "dispatch-not-local" {
		t.Errorf("reason = %q, want dispatch-not-local", reason)
	}
	if say, _ := payload["error"].(string); !strings.Contains(say, "laptop") {
		t.Errorf("refusal = %q, want it to name the machine", say)
	}
	if len(disp.sent()) != 0 {
		t.Error("a refused dispatch must not have sent anything")
	}
}

// A dispatch follows the session, journals itself, and carries the reporting
// instruction — the assistant follows everything it starts.
func TestRunPromptFollowsJournalsAndReports(t *testing.T) {
	ctx := context.Background()
	dir := &fakeDirectory{}
	disp := &fakeDispatcher{delivery: DeliveryQueued}
	svc, queries, _ := newTestService(t, WithDirectory(dir), WithDispatcher(disp))

	session := seedSession(t, queries)
	dir.sessions = []SessionRow{{ID: session.ID, Name: "Reconnect Drops", ProjectName: "riff"}}

	payload, err := svc.Invoke(ctx, VerbRunPrompt,
		map[string]any{"session_id": session.ID, "prompt": "add a retry around the reconnect"})
	if err != nil {
		t.Fatalf("Invoke() = %v", err)
	}
	if payload["delivery"] != string(DeliveryQueued) {
		t.Errorf("delivery = %v, want the server's own reading %q", payload["delivery"], DeliveryQueued)
	}
	if len(disp.reporting) != 1 || !disp.reporting[0] {
		t.Error("a dispatched run must carry the reporting instruction: the assistant follows what it starts")
	}

	if !svc.Following(ctx, session.ID) {
		t.Error("a dispatched session must be followed, or its first report has nowhere to go")
	}

	entries, err := svc.Journal(ctx, "", 10)
	if err != nil {
		t.Fatalf("Journal() = %v", err)
	}
	if len(entries) != 1 || entries[0].Kind != JournalDispatched {
		t.Fatalf("journal = %+v, want one dispatched entry", entries)
	}
	if entries[0].SessionID != session.ID {
		t.Errorf("entry names session %q, want %q", entries[0].SessionID, session.ID)
	}
}

func TestNoteIsNotableAndInTheJournal(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(t)

	payload, err := svc.Invoke(ctx, VerbNote, map[string]any{"text": "they want the rail quieter"})
	if err != nil {
		t.Fatalf("Invoke() = %v", err)
	}
	if payload["noted"] != true {
		t.Fatalf("payload = %v, want the note kept", payload)
	}

	entries, err := svc.Journal(ctx, "", 10)
	if err != nil {
		t.Fatalf("Journal() = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("journal has %d entries, want 1", len(entries))
	}
	if entries[0].Kind != JournalNote || !entries[0].Notable {
		t.Errorf("entry = %+v, want a notable note", entries[0])
	}
	if entries[0].Untrusted {
		t.Error("a note is the assistant's own sentence, not agent-written text")
	}
}

func TestNoteRefusesNothing(t *testing.T) {
	svc, _, _ := newTestService(t)
	payload, err := svc.Invoke(context.Background(), VerbNote, map[string]any{"text": "  "})
	if err != nil {
		t.Fatalf("Invoke() = %v", err)
	}
	if _, refused := payload["error"]; !refused {
		t.Errorf("payload = %v, want a refusal", payload)
	}
}

// Creating names one project or asks. It never picks between two.
func TestCreateSessionAsksWhenTheProjectIsAmbiguous(t *testing.T) {
	dir := &fakeDirectory{projects: []ProjectRow{
		{ID: "p1", Name: "riff", Slug: "riff"},
		{ID: "p2", Name: "riff tools", Slug: "riff-tools"},
	}}
	svc, _, _ := newTestService(t, WithDirectory(dir))

	payload, err := svc.Invoke(context.Background(), VerbCreateSession, map[string]any{"project": "riff"})
	if err != nil {
		t.Fatalf("Invoke() = %v", err)
	}
	if reason, _ := payload[reasonKey].(string); reason != "project-ambiguous" {
		t.Errorf("reason = %q, want project-ambiguous", reason)
	}
	if dir.lastProject != "" {
		t.Error("nothing must be created while the project is a question")
	}
}

// A model nobody has is a question, and the answer names the ones that exist.
func TestCreateSessionRefusesAnUnknownModel(t *testing.T) {
	dir := &fakeDirectory{
		projects:  []ProjectRow{{ID: "p1", Name: "riff", Slug: "riff"}},
		createErr: &UnknownModelError{Spoken: "fable", Families: []string{"Opus", "Sonnet"}},
	}
	svc, _, _ := newTestService(t, WithDirectory(dir))

	payload, err := svc.Invoke(context.Background(), VerbCreateSession,
		map[string]any{"project": "riff", "model": "fable"})
	if err != nil {
		t.Fatalf("Invoke() = %v", err)
	}
	if reason, _ := payload[reasonKey].(string); reason != "unknown-model" {
		t.Fatalf("reason = %q, want unknown-model", reason)
	}
	if say, _ := payload["error"].(string); !strings.Contains(say, "Sonnet") {
		t.Errorf("refusal = %q, want the families that do exist", say)
	}
}
