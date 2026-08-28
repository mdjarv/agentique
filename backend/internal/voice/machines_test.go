package voice

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// CAN THE VOICE ASSISTANT LIST THE MACHINES PAIRED WITH AGENTIQUE?
//
// Short answer, which these tests pin down: no tool asks that question, and the
// assistant only ever learns a machine's name as a property of a session it can
// already see. So it can answer "which machines is something running on" and it
// cannot answer "which machines are paired" — a paired machine sitting idle is
// invisible to a call, and so is every fact about pairing (connected, away,
// last seen).
//
// The distinction matters because the two questions sound identical spoken
// aloud. An operator who asks "what machines do I have" and hears three names
// will read that as the pairing list, when it is really the subset with live
// sessions.
//
// These are TRUTH tests, not aspiration. If someone adds a machines tool they
// should fail, and the failure message says which sentence in docs/voice.md to
// go and rewrite.

// --- What the model is actually offered ---------------------------------------------------

// TestNoToolAsksAboutMachines locks the tool surface. The declarations are
// fixed at connect (see toolDeclarations), so this set IS the assistant's
// vocabulary for the whole call — nothing can be added mid-conversation to
// answer a question the operator turns out to have.
func TestNoToolAsksAboutMachines(t *testing.T) {
	want := []string{
		ToolCreateSession,
		ToolFindSession,
		ToolFocusSession,
		ToolHangUp,
		ToolListProjects,
		ToolListSessions,
		ToolRunPrompt,
		ToolSummarizeSession,
	}

	var got []string
	for _, decl := range toolDeclarations() {
		got = append(got, decl.Name)

		// A machine tool would most likely announce itself in the name; catch a
		// differently-named one by its description too.
		if strings.Contains(strings.ToLower(decl.Name), "machine") {
			t.Errorf("a machine tool %q now exists — this test and docs/voice.md both describe "+
				"a surface that has none", decl.Name)
		}
	}
	sort.Strings(got)

	if len(got) != len(want) {
		t.Fatalf("tool count changed: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tool set changed: got %v, want %v", got, want)
		}
	}
}

// --- What it CAN do: name the machines that have sessions ---------------------------------

// machinesIn collects the distinct machine names a list_sessions answer would
// let the assistant say out loud. This is the only channel by which a machine
// name reaches the speech model at all.
func machinesIn(t *testing.T, answer map[string]any) []string {
	t.Helper()
	rows, ok := answer["sessions"].([]map[string]any)
	if !ok {
		t.Fatalf("no sessions in the answer: %v", answer)
	}
	seen := map[string]bool{}
	var out []string
	for _, row := range rows {
		name, _ := row["machine"].(string)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// connectedWorld is the browser's snapshot for two PAIRED machines. This is how
// a remote machine's sessions reach a call: not from this server's database,
// but pushed by the browser, which holds a socket to each paired machine.
// Simulating "connected machines" in this package means exactly this — there is
// no other door.
func connectedWorld() []wireSessionRow {
	return []wireSessionRow{
		{
			SessionID:      "sess-laptop",
			Name:           "Top Bar Redesign",
			ProjectName:    "Agentique",
			MachineID:      "m-laptop",
			MachineName:    "thinkpad",
			State:          "running",
			LastActivityAt: "2026-08-28T12:00:00Z",
		},
		{
			SessionID:      "sess-mini",
			Name:           "Nightly Import",
			ProjectName:    "Alltix",
			MachineID:      "m-mini",
			MachineName:    "mac-mini",
			State:          "idle",
			Attention:      AttentionUnread,
			LastActivityAt: "2026-08-28T11:00:00Z",
		},
	}
}

// localDirectory stands in for this server's own database — the one machine a
// call has authority over.
func localDirectory() *fakeDirectory {
	return &fakeDirectory{rows: []SessionRow{{
		ID:           "sess-local",
		Name:         "Träffbild ML",
		ProjectName:  "Träffbild",
		MachineID:    "m-desktop",
		MachineName:  "workstation",
		State:        "idle",
		LastActivity: "2026-08-28T13:00:00Z",
	}}}
}

// TestListSessionsNamesEveryMachineThatHasASession is the capability that does
// exist. Three machines, one local and two paired, and the assistant can name
// all three — because each of their sessions carries the name along.
func TestListSessionsNamesEveryMachineThatHasASession(t *testing.T) {
	c := newToolCall(localDirectory(), &fakeDispatcher{}, "")
	c.setWorld(connectedWorld())

	answer := c.toolListSessions(context.Background(), map[string]any{"filter": FilterAll})

	got := machinesIn(t, answer)
	want := []string{"mac-mini", "thinkpad", "workstation"}
	if len(got) != len(want) {
		t.Fatalf("machines the assistant could name: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("machines the assistant could name: got %v, want %v", got, want)
		}
	}
}

// TestAPairedMachineWithNoSessionsIsInvisible is the gap. "printer-room" is
// paired and connected; it simply has nothing running. Nothing in any tool
// answer mentions it, so the assistant cannot include it in a list of machines
// and cannot be asked to start work there.
func TestAPairedMachineWithNoSessionsIsInvisible(t *testing.T) {
	c := newToolCall(localDirectory(), &fakeDispatcher{}, "")
	// The snapshot carries SESSIONS, not machines (see wireSessionRow): a paired
	// machine with none contributes no rows, so there is nothing to omit.
	c.setWorld(connectedWorld())

	for _, filter := range []string{FilterAll, FilterRecent, FilterRunning, FilterNeedsAttention} {
		answer := c.toolListSessions(context.Background(), map[string]any{"filter": filter})
		if strings.Contains(strings.ToLower(rendered(answer)), "printer-room") {
			t.Errorf("filter %q surfaced an idle paired machine — the capability changed", filter)
		}
	}

	// And the negative said positively: with the two paired machines' sessions
	// removed, the assistant is back to knowing about one machine, even though
	// both are still paired and connected.
	c.setWorld(nil)
	answer := c.toolListSessions(context.Background(), map[string]any{"filter": FilterAll})
	if got := machinesIn(t, answer); len(got) != 1 || got[0] != "workstation" {
		t.Fatalf("with no remote sessions the call should know only its own machine, got %v", got)
	}
}

// TestFindSessionAlsoCarriesTheMachine checks the other route a machine name
// can take to the model, so the capability claim does not rest on one tool.
func TestFindSessionAlsoCarriesTheMachine(t *testing.T) {
	c := newToolCall(localDirectory(), &fakeDispatcher{}, "")
	c.setWorld(connectedWorld())

	answer := c.toolFindSession(context.Background(), map[string]any{"query": "nightly import"})
	if !strings.Contains(rendered(answer), "mac-mini") {
		t.Errorf("find_session dropped the machine name, which is how the assistant tells two "+
			"similarly-named sessions apart: %v", answer)
	}
}

// TestUnnamedMachineDegradesToWords covers the reporting path for a row whose
// machine has no name — the assistant must still be able to form the sentence
// explaining why work cannot start there.
func TestUnnamedMachineDegradesToWords(t *testing.T) {
	if got := machineWords(SessionRow{MachineID: "m-x"}); got != "another machine" {
		t.Errorf("machineWords with no name = %q, want %q", got, "another machine")
	}
	if got := machineWords(SessionRow{MachineName: "thinkpad"}); got != "thinkpad" {
		t.Errorf("machineWords = %q, want %q", got, "thinkpad")
	}
}

// rendered flattens a tool answer to one searchable string. The payloads nest
// (sessions is a slice of maps), and every assertion here is "does this name
// reach the model at all", not "in which field".
func rendered(answer map[string]any) string {
	var b strings.Builder
	var write func(v any)
	write = func(v any) {
		switch t := v.(type) {
		case string:
			b.WriteString(t)
			b.WriteByte('\n')
		case []map[string]any:
			for _, m := range t {
				write(m)
			}
		case []any:
			for _, e := range t {
				write(e)
			}
		case map[string]any:
			for _, e := range t {
				write(e)
			}
		}
	}
	write(answer)
	return b.String()
}

// --- Three more ways a machine that exists produces no evidence of itself -----------------

// TestAskingForAnIdleMachineByNameDeadEnds is the other half of the invisibility:
// the operator does not have to wait to be told the list, they can name the
// machine. find_session is where that lands, and it answers about SESSIONS.
//
// The refusal is true and misleading at once. Nothing in it distinguishes "that
// machine has nothing running" from "there is no such machine", which are
// different answers to the question that was actually asked.
func TestAskingForAnIdleMachineByNameDeadEnds(t *testing.T) {
	c := newToolCall(localDirectory(), &fakeDispatcher{}, "")
	c.setWorld(connectedWorld())

	answer := c.toolFindSession(context.Background(), map[string]any{"query": "printer-room"})
	if candidates, ok := answer["candidates"].([]map[string]any); ok && len(candidates) != 0 {
		t.Fatalf("found %d candidates for a machine with no sessions: %v", len(candidates), candidates)
	}
	note, _ := answer["note"].(string)
	if !strings.Contains(note, "Nothing matches") {
		t.Errorf("note = %q, want the no-match wording", note)
	}
	if strings.Contains(strings.ToLower(note), "machine has no") {
		t.Error("the no-match note now separates an idle machine from an unknown one — " +
			"a real improvement, and this test is what needs rewriting")
	}
}

// TestAnUnlabelledMachineLosesItsIdToo goes one step past machineWords. A row
// whose machine has no catalog label still carries a MachineID, and rowPayload
// emits MachineName only — so by the time the model sees the row it is
// indistinguishable from a local one, and there is not even an id to read out.
func TestAnUnlabelledMachineLosesItsIdToo(t *testing.T) {
	c := newToolCall(nil, &fakeDispatcher{}, "")
	c.setWorld([]wireSessionRow{{
		SessionID:      "sess-anon",
		Name:           "Top Bar Redesign",
		ProjectName:    "Agentique",
		MachineID:      "m-laptop",
		State:          "idle",
		LastActivityAt: "2026-08-28T12:00:00Z",
	}})

	answer := c.toolListSessions(context.Background(), map[string]any{"filter": FilterAll})
	rows, ok := answer["sessions"].([]map[string]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("sessions = %v, want one row", answer["sessions"])
	}
	if _, present := rows[0]["machine"]; present {
		t.Errorf("machine = %v, want the key absent when there is no label", rows[0]["machine"])
	}
	if strings.Contains(rendered(answer), "m-laptop") {
		t.Error("the machine id now reaches the model; an id is not a name, but it is more " +
			"than nothing and this test should say so instead")
	}
}

// TestTheSpokenCapCanHideAMachineEntirely is the failure mode that survives even
// when every machine does have sessions.
//
// list_sessions truncates to maxSpokenRows across all machines at once, ordered
// by attention and then recency, so the quietest machine drops out first. What
// comes back says `omitted`, counted in SESSIONS — which is the wrong unit for
// the question "what machines do I have", and the only unit on the wire.
func TestTheSpokenCapCanHideAMachineEntirely(t *testing.T) {
	rows := make([]wireSessionRow, 0, maxSpokenRows+1)
	for i := range maxSpokenRows + 1 {
		rows = append(rows, wireSessionRow{
			SessionID:      fmt.Sprintf("sess-busy-%d", i),
			Name:           "Reconnect Drops",
			ProjectName:    "Agentique",
			MachineID:      "m-laptop",
			MachineName:    "thinkpad",
			State:          "idle",
			LastActivityAt: fmt.Sprintf("2026-08-28T12:00:%02dZ", i),
		})
	}
	// One session on a second machine, and the least recent thing in the world.
	rows = append(rows, wireSessionRow{
		SessionID:      "sess-quiet",
		Name:           "Nightly Import",
		ProjectName:    "Alltix",
		MachineID:      "m-mini",
		MachineName:    "mac-mini",
		State:          "idle",
		LastActivityAt: "2020-01-01T00:00:00Z",
	})

	c := newToolCall(nil, &fakeDispatcher{}, "")
	c.setWorld(rows)

	answer := c.toolListSessions(context.Background(), map[string]any{"filter": FilterAll})
	for _, name := range machinesIn(t, answer) {
		if name == "mac-mini" {
			t.Fatal("the cap no longer drops the quietest machine — update this test")
		}
	}
	if got, _ := answer["omitted"].(int); got != 2 {
		t.Errorf("omitted = %v, want 2 sessions", answer["omitted"])
	}
	// Two sessions omitted, one machine omitted. Nothing on the wire says the
	// second number exists.
}

// TestListProjectsCarriesNoMachine closes the last route. list_projects is the
// only other tool that could name a machine, and it is deliberately local-only:
// a project on another machine cannot host a session created from here, so it is
// not listed and its machine is never named.
func TestListProjectsCarriesNoMachine(t *testing.T) {
	c := newToolCall(directoryWithTwo(), &fakeDispatcher{}, "")

	answer := c.toolListProjects(context.Background(), nil)
	rows, ok := answer["projects"].([]map[string]any)
	if !ok || len(rows) == 0 {
		t.Fatalf("projects = %v, want rows", answer["projects"])
	}
	for _, row := range rows {
		if _, present := row["machine"]; present {
			t.Errorf("a project payload names a machine: %v", row)
		}
	}
}
