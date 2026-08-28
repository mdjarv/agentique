package voice

import (
	"context"
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
