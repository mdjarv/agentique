package assistant

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/usage"
)

// The table is closed, and these are the members. A verb added without a
// decision about its tier is the failure this guards: the tier is the whole
// containment story, so it cannot be defaulted.
func TestVerbTableIsClosedAndEveryVerbHasATier(t *testing.T) {
	svc, _, _ := newTestService(t)

	want := map[string]Tier{
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

		VerbMergeSession:    TierUncontained,
		VerbRebaseSession:   TierUncontained,
		VerbArchiveSession:  TierUncontained,
		VerbDeleteSession:   TierUncontained,
		VerbReclaimSession:  TierUncontained,
		VerbDissolveChannel: TierUncontained,
		VerbSetSessionModel: TierUncontained,
		VerbSetSessionMode:  TierUncontained,
	}

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

		switch verb.Tier {
		case TierRead, TierContained:
			if !verb.HasHandler() {
				t.Errorf("%q is %s and has no handler", verb.Name, verb.Tier)
			}
		case TierUncontained:
			if verb.HasHandler() {
				t.Errorf("%q is uncontained and has a handler — these are never performed", verb.Name)
			}
		}
	}
}

// Asking for an uncontained verb is a proposal, not an action and not a crash.
func TestUncontainedVerbsNeedAProposal(t *testing.T) {
	svc, _, _ := newTestService(t)

	for _, verb := range svc.Verbs() {
		if verb.Tier != TierUncontained {
			continue
		}
		_, err := svc.Invoke(context.Background(), verb.Name, nil)
		if !errors.Is(err, ErrProposalRequired) {
			t.Errorf("Invoke(%q) = %v, want ErrProposalRequired", verb.Name, err)
		}
		var typed *ProposalRequiredError
		if !errors.As(err, &typed) || typed.Verb != verb.Name {
			t.Errorf("Invoke(%q) did not name the verb in a typed error: %v", verb.Name, err)
		}
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

	raw, err := svc.Invoke(context.Background(), VerbMergeSession, nil)
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
