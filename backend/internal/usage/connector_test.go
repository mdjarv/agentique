package usage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/allbin/agentkit/runtime"
)

// fakeAccount stands in for a connector. The real one dials an app-server;
// what matters here is the contract, not the transport.
type fakeAccount struct {
	limits *runtime.RateLimits
	err    error
	calls  int
	block  chan struct{}
}

func (f *fakeAccount) AccountRateLimits(ctx context.Context) (*runtime.RateLimits, error) {
	f.calls++
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return f.limits, f.err
}

var testNow = time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)

func collect(src runtime.AccountInspectable, prev *Agent) (Agent, bool) {
	return collectConnector(context.Background(), "codex", "Codex", src, prev, testNow)
}

func TestConnectorNormalizesWindows(t *testing.T) {
	src := &fakeAccount{limits: &runtime.RateLimits{
		PlanLabel: "plus",
		Windows: []runtime.RateLimitWindow{
			{Label: "5h window", Percent: 0.31, ResetsAt: testNow.Add(2 * time.Hour)},
			{Label: "Weekly (7-day)", Percent: 0.07},
		},
	}}

	agent, ok := collect(src, nil)
	if !ok || !agent.Ready {
		t.Fatalf("a clean answer is a ready record: %+v", agent)
	}
	if agent.TierLabel != "plus" {
		t.Errorf("tierLabel = %q — the plan is rendered as a caption, never parsed", agent.TierLabel)
	}
	if len(agent.Limits) != 2 {
		t.Fatalf("want 2 windows, got %d", len(agent.Limits))
	}
	if agent.Limits[0].Percent != 0.31 {
		t.Errorf("percent must pass through as a fraction, got %v", agent.Limits[0].Percent)
	}
	if agent.Limits[0].ResetsAt != "2026-08-27T14:00:00Z" {
		t.Errorf("resetsAt = %q", agent.Limits[0].ResetsAt)
	}
	// A window with no reset time is normal, not an error — and must not
	// invent one.
	if agent.Limits[1].ResetsAt != "" {
		t.Errorf("a zero reset time must stay empty, got %q", agent.Limits[1].ResetsAt)
	}
}

// Unknown and unused are different answers. A window with no number must not
// reach the wire, where it would draw as a full, untouched allowance.
func TestConnectorDropsUnknownWindows(t *testing.T) {
	src := &fakeAccount{limits: &runtime.RateLimits{
		Windows: []runtime.RateLimitWindow{
			{Label: "5h window", Percent: -1},
			{Label: "Weekly (7-day)", Percent: 0.5},
		},
	}}
	agent, ok := collect(src, nil)
	if !ok {
		t.Fatal("one usable window is still a record")
	}
	if len(agent.Limits) != 1 || agent.Limits[0].Label != "Weekly (7-day)" {
		t.Fatalf("the unknown window must be dropped: %+v", agent.Limits)
	}
}

// A machine that cannot ask says nothing. A CLI that is not installed is a
// normal state, not a row reporting that it is missing.
func TestConnectorStaysSilentWhenItCannotAsk(t *testing.T) {
	for _, err := range []error{
		runtime.ErrNotSupported,
		errors.New("exec: codex: not found"),
		context.DeadlineExceeded,
	} {
		if _, ok := collect(&fakeAccount{err: err}, nil); ok {
			t.Errorf("%v must produce no record at all", err)
		}
	}
}

// Being signed out is the one failure worth a row on a machine that has never
// had numbers: the operator can fix it, and the command is not guessable.
func TestConnectorNamesTheSignInCommand(t *testing.T) {
	agent, ok := collect(&fakeAccount{err: runtime.ErrNotSignedIn}, nil)
	if !ok {
		t.Fatal("signed-out is worth saying")
	}
	if agent.Ready {
		t.Error("signed out is not ready")
	}
	if agent.AuthHelpText == "" {
		t.Error("only the CLI can sign in — name the command")
	}
}

// A failed refresh never blanks numbers that were true a minute ago.
func TestConnectorKeepsLastGoodOnFailure(t *testing.T) {
	previous := &Agent{
		ID:     "codex",
		Name:   "Codex",
		Ready:  true,
		Limits: []Limit{{Label: "5h window", Percent: 0.4}},
	}
	for _, tc := range []struct {
		err  error
		want string
	}{
		// ErrNotSupported is absent on purpose: it is structural, so it drops
		// the record rather than keeping stale numbers alive for a provider
		// that can no longer answer. TestConnectorForgetsOnStructuralFailure
		// covers that.
		{runtime.ErrNotSignedIn, "Not signed in."},
		{context.DeadlineExceeded, "Timed out reading limits."},
		{errors.New("boom"), "Could not read limits."},
	} {
		agent, ok := collect(&fakeAccount{err: tc.err}, previous)
		if !ok {
			t.Fatalf("%v: a record with history stays", tc.err)
		}
		if len(agent.Limits) != 1 || agent.Limits[0].Percent != 0.4 {
			t.Errorf("%v: the numbers must survive, got %+v", tc.err, agent.Limits)
		}
		if agent.Ready {
			t.Errorf("%v: stale numbers are not ready", tc.err)
		}
		if agent.UsageStatusText != tc.want {
			t.Errorf("%v: status = %q, want %q", tc.err, agent.UsageStatusText, tc.want)
		}
	}
}

// A connector that can no longer report at all takes its record with it. An
// uninstalled CLI must not leave a meter behind that never stops being wrong.
func TestConnectorForgetsOnStructuralFailure(t *testing.T) {
	previous := &Agent{
		ID:     "codex",
		Ready:  true,
		Limits: []Limit{{Label: "5h window", Percent: 0.4}},
	}
	if _, ok := collect(&fakeAccount{err: runtime.ErrNotSupported}, previous); ok {
		t.Fatal("ErrNotSupported is structural — the record goes with it")
	}
}

// A clean answer reporting nothing is neither a failure nor zero.
func TestConnectorEmptyAnswer(t *testing.T) {
	src := &fakeAccount{limits: &runtime.RateLimits{}}
	if _, ok := collect(src, nil); ok {
		t.Error("nothing reported and nothing known is not a row")
	}

	previous := &Agent{ID: "codex", Limits: []Limit{{Label: "5h window", Percent: 0.2}}}
	agent, ok := collect(src, previous)
	if !ok || len(agent.Limits) != 1 {
		t.Fatalf("previous numbers survive an empty answer: %+v", agent)
	}
	if agent.AuthHelpText != "" {
		t.Error("an empty answer is not an auth problem and must not blame one")
	}
}

// The probe is bounded on its own, so one vendor hanging cannot hold up the
// poll or the others in it.
func TestConnectorProbeIsBounded(t *testing.T) {
	src := &fakeAccount{block: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		collectConnector(ctx, "codex", "Codex", src, nil, testNow)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a cancelled probe must return rather than hang the poll")
	}
}

// Collectors run in parallel and are folded independently: a silent one is
// forgotten by id, and a live one is unaffected by it.
func TestProbeForgetsASilentAgentByID(t *testing.T) {
	src := &fakeAccount{limits: &runtime.RateLimits{
		Windows: []runtime.RateLimitWindow{{Label: "5h window", Percent: 0.2}},
	}}
	c := New(Options{
		Accounts: map[string]runtime.AccountInspectable{"codex": src},
		Claude:   ClaudeOptions{CredentialsPath: t.TempDir() + "/absent.json"},
		Now:      func() time.Time { return testNow },
	})

	c.probe(context.Background())
	if _, ok := c.agents["codex"]; !ok {
		t.Fatal("a reporting connector becomes a record")
	}

	// Now it cannot answer at all — the record must go, not linger stale.
	src.limits = nil
	src.err = runtime.ErrNotSupported
	c.probe(context.Background())
	if _, ok := c.agents["codex"]; ok {
		t.Fatal("a connector that says nothing must be forgotten by id")
	}
}
