package mcphttp

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/allbin/agentkit/devurls"
	akmcp "github.com/allbin/agentkit/mcphttp"
	"github.com/mdjarv/agentique/backend/internal/assistant"
)

// toolNames lists what a built handler actually offers, which is how the
// optional tools are tested: registration is the switch.
func toolNames(t *testing.T, h http.Handler) []string {
	t.Helper()
	handler, ok := h.(*akmcp.Handler)
	if !ok {
		t.Fatalf("NewHandler returned %T, want *mcphttp.Handler", h)
	}
	names := make([]string, 0, len(handler.Tools()))
	for _, tool := range handler.Tools() {
		names = append(names, tool.Name)
	}
	return names
}

func itoa(n int) string { return strconv.Itoa(n) }

// pickFreePort grabs an ephemeral port that's free at the moment of the call.
func pickFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

func resultText(r akmcp.Result) string {
	if len(r.Content) == 0 {
		return ""
	}
	return r.Content[0].Text
}

// TestDevURLToolsRoundTrip exercises the agentkit/devurls swap end-to-end:
// Acquire returns a lease, ListDevUrls reflects the holder, Release frees it,
// and KillDevUrlPort handles a free slot gracefully (full kill path is covered
// by the live smoke-test step on a running server).
func TestDevURLToolsRoundTrip(t *testing.T) {
	ctx := context.Background()
	port := pickFreePort(t)
	store := devurls.NewStore([]devurls.Slot{
		{Slot: "dev1", Port: port, PublicHost: "dev1.test.example"},
	})

	res := acquireDevImpl(ctx, store, "sess-1")
	if res.IsError {
		t.Fatalf("acquireDevImpl reported error: %s", resultText(res))
	}
	if !strings.Contains(resultText(res), `Acquired dev URL slot "dev1"`) {
		t.Fatalf("acquire result missing slot name: %s", resultText(res))
	}

	// Bind the port to simulate the session having started its dev server
	// — otherwise List reports the lease as stale.
	bound, err := net.Listen("tcp", "127.0.0.1:"+itoa(port))
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	listRes := listDevURLsImpl(ctx, store)
	_ = bound.Close()
	if !strings.Contains(resultText(listRes), "held by sess-1 (port bound)") {
		t.Fatalf("list result missing held marker: %s", resultText(listRes))
	}

	relRes := releaseDevImpl(store, "sess-1")
	if !strings.Contains(resultText(relRes), "Released slot(s)") {
		t.Fatalf("release result unexpected: %s", resultText(relRes))
	}
	if leases := store.List(); len(leases) != 0 {
		t.Fatalf("expected no leases after release, got %d", len(leases))
	}

	killRes := killDevPortImpl(ctx, store, "dev1")
	if killRes.IsError {
		t.Fatalf("killDevPortImpl reported error: %s", resultText(killRes))
	}
	if !strings.Contains(resultText(killRes), "already free") {
		t.Fatalf("kill on free port should say already free: %s", resultText(killRes))
	}
}

// TestDevURLAcquireAllBusy verifies the agentkit ErrAllBusy sentinel flows
// through acquireDevImpl unchanged.
func TestDevURLAcquireAllBusy(t *testing.T) {
	ctx := context.Background()
	port := pickFreePort(t)
	store := devurls.NewStore([]devurls.Slot{
		{Slot: "dev1", Port: port, PublicHost: "dev1.test.example"},
	})

	if res := acquireDevImpl(ctx, store, "sess-A"); res.IsError {
		t.Fatalf("first acquire failed: %s", resultText(res))
	}

	res := acquireDevImpl(ctx, store, "sess-B")
	if !res.IsError {
		t.Fatalf("expected error result on exhausted pool, got: %s", resultText(res))
	}
	if !strings.Contains(resultText(res), "All dev URL slots are currently in use") {
		t.Fatalf("expected all-busy text, got: %s", resultText(res))
	}

	if _, err := store.Acquire(ctx, "sess-C"); !errors.Is(err, devurls.ErrAllBusy) {
		t.Fatalf("expected ErrAllBusy from store.Acquire, got %v", err)
	}
}

// TestDevURLKillExternalPort exercises the SIGTERM path on a real listener
// that the test owns. Linux-only — gracefully no-ops on platforms where
// FindPortOwner returns nil (the package's /proc-based lookup is Linux-only).
func TestDevURLKillExternalPort(t *testing.T) {
	ctx := context.Background()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port

	// Owner here is the test process itself; killDevPortImpl would SIGTERM
	// us, which is not what we want in a unit test. Instead, verify the
	// owner-lookup path runs cleanly through the new ctx-threading API.
	owner, err := devurls.FindPortOwner(ctx, port)
	if err != nil {
		t.Fatalf("FindPortOwner returned error: %v", err)
	}
	// On Linux owner should be populated; on other platforms it may be nil.
	t.Logf("FindPortOwner: owner=%v", owner)
}

// fakeModelInspector records which session the SessionModel tool asked about.
type fakeModelInspector struct {
	report SessionModelReport
	err    error
	asked  []string
}

func (f *fakeModelInspector) InspectSessionModel(_ context.Context, sessionID string) (SessionModelReport, error) {
	f.asked = append(f.asked, sessionID)
	return f.report, f.err
}

// TestSessionModelDefaultsToCaller covers the common call: an agent asking what
// it is running on, with no argument.
func TestSessionModelDefaultsToCaller(t *testing.T) {
	insp := &fakeModelInspector{report: SessionModelReport{
		SessionID:       "sess-1",
		Provider:        "claude",
		RequestedSlug:   "opus",
		ResolvedModelID: "claude-opus-5",
		ResolvedAt:      "2026-09-09T10:00:00Z",
		Source:          "init_event",
	}}

	res := sessionModelImpl(context.Background(), insp, "sess-1", "")
	if res.IsError {
		t.Fatalf("sessionModelImpl reported error: %s", resultText(res))
	}
	if len(insp.asked) != 1 || insp.asked[0] != "sess-1" {
		t.Fatalf("inspected %v, want [sess-1]", insp.asked)
	}

	var got SessionModelReport
	if err := json.Unmarshal([]byte(resultText(res)), &got); err != nil {
		t.Fatalf("result is not JSON (%v): %s", err, resultText(res))
	}
	if got != insp.report {
		t.Fatalf("report = %+v, want %+v", got, insp.report)
	}
}

// An explicit argument wins, so one session can inspect another.
func TestSessionModelArgumentOverridesCaller(t *testing.T) {
	insp := &fakeModelInspector{report: SessionModelReport{SessionID: "sess-2", Source: "unresolved"}}

	res := sessionModelImpl(context.Background(), insp, "sess-1", "  sess-2  ")
	if res.IsError {
		t.Fatalf("sessionModelImpl reported error: %s", resultText(res))
	}
	if len(insp.asked) != 1 || insp.asked[0] != "sess-2" {
		t.Fatalf("inspected %v, want [sess-2]", insp.asked)
	}
	if !strings.Contains(resultText(res), `"source":"unresolved"`) {
		t.Fatalf("unresolved source missing from result: %s", resultText(res))
	}
}

func TestSessionModelReportsLookupFailure(t *testing.T) {
	insp := &fakeModelInspector{err: errors.New("session not found")}

	res := sessionModelImpl(context.Background(), insp, "sess-1", "")
	if !res.IsError {
		t.Fatalf("expected an error result, got: %s", resultText(res))
	}
	if !strings.Contains(resultText(res), "session not found") {
		t.Fatalf("error text missing the cause: %s", resultText(res))
	}
}

// With neither a caller identity nor an argument there is nothing to inspect,
// and saying so beats inspecting the empty string.
func TestSessionModelWithoutAnySessionRefuses(t *testing.T) {
	insp := &fakeModelInspector{}

	res := sessionModelImpl(context.Background(), insp, "", "")
	if !res.IsError {
		t.Fatalf("expected an error result, got: %s", resultText(res))
	}
	if len(insp.asked) != 0 {
		t.Fatalf("inspector consulted with no session: %v", insp.asked)
	}
}

// The tool is optional: a handler built without an inspector must not offer it.
func TestSessionModelToolRegistrationIsOptional(t *testing.T) {
	tokens := NewTokenStore()
	dev := devurls.NewStore(nil)

	if got := toolNames(t, NewHandler(tokens, dev, nil, nil, nil, nil)); slices.Contains(got, ToolSessionModel) {
		t.Errorf("SessionModel registered without an inspector: %v", got)
	}
	if got := toolNames(t, NewHandler(tokens, dev, nil, nil, nil, &fakeModelInspector{})); !slices.Contains(got, ToolSessionModel) {
		t.Errorf("SessionModel missing with an inspector: %v", got)
	}
}

// --- The assistant's two callers ---

// fakeAssistant records what reached the core and answers whatever it is told
// to.
type fakeAssistant struct {
	verbs []assistant.Verb
	// reported is every (sid, kind, headline) that got through to Report.
	reported []string
	// invoked is every verb name that got through to ToolHandler.
	invoked []string
}

func (f *fakeAssistant) Report(sessionID, kind, headline string) (string, error) {
	f.reported = append(f.reported, sessionID+"/"+kind+"/"+headline)
	return "kept.", nil
}

func (f *fakeAssistant) Verbs() []assistant.Verb { return f.verbs }

func (f *fakeAssistant) ToolHandler(_ context.Context, name string, _ map[string]any) map[string]any {
	f.invoked = append(f.invoked, name)
	return map[string]any{"ok": true}
}

// callTool invokes one registered tool as sid would.
func callTool(t *testing.T, h http.Handler, sid, name string, args any) akmcp.Result {
	t.Helper()
	handler, ok := h.(*akmcp.Handler)
	if !ok {
		t.Fatalf("NewHandler returned %T, want *mcphttp.Handler", h)
	}
	for _, tool := range handler.Tools() {
		if tool.Name != name {
			continue
		}
		raw, err := json.Marshal(args)
		if err != nil {
			t.Fatalf("marshal args: %v", err)
		}
		return tool.Handler(context.Background(), sid, raw)
	}
	t.Fatalf("no tool named %q; have %v", name, toolNames(t, h))
	return akmcp.Result{}
}

// A nil assistant omits both halves of its surface, which is the correct shape
// on a server where the assistant is switched off: off means unbuilt.
func TestAssistantToolsAreOmittedWhenTheAssistantIsOff(t *testing.T) {
	tokens := NewTokenStore()
	dev := devurls.NewStore(nil)

	got := toolNames(t, NewHandler(tokens, dev, nil, nil, nil, nil))
	if slices.Contains(got, ToolAssistantReport) {
		t.Errorf("AssistantReport is registered with no assistant wired: %v", got)
	}
	if slices.Contains(got, assistant.VerbListSessions) {
		t.Errorf("the verb table is registered with no assistant wired: %v", got)
	}
}

// A reporter with no head is the voice-without-the-assistant shape, and it is
// the one combination that has to work: the dispatcher appends the instruction
// naming AssistantReport to every prompt a call sends, whether or not
// [experimental] assistant is on, so the tool has to be there while the verb
// table is not.
func TestAssistantReportIsRegisteredWithoutTheHead(t *testing.T) {
	tokens := NewTokenStore()
	dev := devurls.NewStore(nil)
	fake := &fakeAssistant{verbs: []assistant.Verb{{
		Name: assistant.VerbListSessions, Tier: assistant.TierRead, Description: "List their sessions.",
	}}}

	got := toolNames(t, NewHandler(tokens, dev, nil, nil, fake, nil))
	if !slices.Contains(got, ToolAssistantReport) {
		t.Errorf("AssistantReport missing with a reporter wired: %v", got)
	}
	if slices.Contains(got, assistant.VerbListSessions) {
		t.Errorf("the verb table is registered with no head wired: %v", got)
	}
}

// The verb table is on the head's endpoint and the session endpoint does not
// LIST it — the half a per-call id check cannot cover, because a handler answers
// tools/list from everything registered on it, whoever asks.
func TestTheVerbTableIsOnlyOnTheHeadsEndpoint(t *testing.T) {
	dev := devurls.NewStore(nil)
	fake := &fakeAssistant{verbs: []assistant.Verb{{
		Name: assistant.VerbListSessions, Tier: assistant.TierRead, Description: "List their sessions.",
	}}}

	sessions := toolNames(t, NewHandler(NewTokenStore(), dev, nil, nil, fake, nil))
	if slices.Contains(sessions, assistant.VerbListSessions) {
		t.Errorf("a coding session is shown the assistant's verbs: %v", sessions)
	}
	if !slices.Contains(sessions, ToolAssistantReport) {
		t.Errorf("AssistantReport missing from the session endpoint: %v", sessions)
	}

	head := toolNames(t, NewAssistantHandler(NewTokenStore(), fake))
	if !slices.Contains(head, assistant.VerbListSessions) {
		t.Errorf("the head's endpoint does not carry the table: %v", head)
	}
	if slices.Contains(head, ToolAssistantReport) || slices.Contains(head, ToolSetSessionName) {
		t.Errorf("the head's endpoint carries a session's tools: %v", head)
	}
}

// Two endpoints, and the injected id is still the test on each: a token is a
// credential, where the id is what a tool would act AS, so the separation holds
// from both sides even for a caller that reached the other endpoint.
func TestAssistantToolsSeparateASessionFromTheHead(t *testing.T) {
	tokens := NewTokenStore()
	dev := devurls.NewStore(nil)
	fake := &fakeAssistant{verbs: []assistant.Verb{{
		Name:        assistant.VerbListSessions,
		Tier:        assistant.TierRead,
		Description: "List their sessions.",
		Input: []assistant.Param{{
			Name: "filter", Type: assistant.ParamString, Description: "Which ones.",
		}},
	}}}
	h := NewHandler(tokens, dev, nil, nil, fake, nil)
	headEndpoint := NewAssistantHandler(NewTokenStore(), fake)

	const sessionSID = "11111111-2222-3333-4444-555555555555"
	headSID := assistant.HeadIDPrefix + "66666666-7777-8888-9999-000000000000"

	// A session may report, and may not reach into the verb table.
	if got := resultText(callTool(t, h, sessionSID, ToolAssistantReport,
		map[string]string{"kind": "surprise", "headline": "the auth tests were already failing"})); got != "kept." {
		t.Errorf("a session's report answered %q, want the core's answer", got)
	}
	if len(fake.reported) != 1 {
		t.Fatalf("the core saw %d reports, want 1", len(fake.reported))
	}

	refused := callTool(t, headEndpoint, sessionSID, assistant.VerbListSessions, map[string]any{})
	if !refused.IsError {
		t.Error("a session calling one of the assistant's verbs was allowed through")
	}
	if len(fake.invoked) != 0 {
		t.Errorf("a session's verb call reached the core: %v", fake.invoked)
	}

	// And the head may call a verb, but may not report as a session it is only
	// borrowing the identity of.
	if got := callTool(t, headEndpoint, headSID, assistant.VerbListSessions,
		map[string]any{"filter": "all"}); got.IsError {
		t.Errorf("the head was refused its own verb: %q", resultText(got))
	}
	if len(fake.invoked) != 1 || fake.invoked[0] != assistant.VerbListSessions {
		t.Errorf("the core saw %v, want one list_sessions", fake.invoked)
	}

	headReport := callTool(t, h, headSID, ToolAssistantReport,
		map[string]string{"kind": "surprise", "headline": "something"})
	if !headReport.IsError {
		t.Error("the head was allowed to report as a session")
	}
	if len(fake.reported) != 1 {
		t.Errorf("the head's report reached the core: %v", fake.reported)
	}
}

// A session-only tool refuses the head for the same reason: it acts on the
// calling session, and the head is not one.
func TestSessionToolsRefuseTheHead(t *testing.T) {
	tokens := NewTokenStore()
	dev := devurls.NewStore(nil)
	h := NewHandler(tokens, dev, nil, nil, &fakeAssistant{}, nil)

	headSID := assistant.HeadIDPrefix + "66666666-7777-8888-9999-000000000000"
	got := callTool(t, h, headSID, ToolSetSessionName, map[string]string{"name": "whatever"})
	if !got.IsError {
		t.Error("the head was allowed to rename the session whose identity it borrowed")
	}
	if !strings.Contains(resultText(got), "not one") {
		t.Errorf("the refusal does not say why: %q", resultText(got))
	}
}
