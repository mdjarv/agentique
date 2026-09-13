package server

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	claudecli "github.com/allbin/claudecli-go"

	"github.com/mdjarv/agentique/backend/internal/assistant"
	"github.com/mdjarv/agentique/backend/internal/session"
)

// recordingRunner is the blocking runner without a CLI behind it.
type recordingRunner struct {
	answer string
	err    error

	prompts []string
	opts    []claudecli.Option
}

func (r *recordingRunner) RunBlocking(_ context.Context, prompt string, opts ...claudecli.Option) (*claudecli.BlockingResult, error) {
	r.prompts = append(r.prompts, prompt)
	r.opts = opts
	if r.err != nil {
		return nil, r.err
	}
	return &claudecli.BlockingResult{Text: r.answer}, nil
}

// The triager answers with the model's raw text and nothing else: the parsing
// and the fail-closed rule belong to the core, so a second implementation
// cannot invent a fourth verdict.
func TestTheTriagerAnswersRawText(t *testing.T) {
	runner := &recordingRunner{answer: "  none\n"}
	triager := newAssistantTriager(runner, nil, "")

	answer, err := triager.Triage(context.Background(), "judge this")
	if err != nil {
		t.Fatalf("Triage() = %v", err)
	}
	if answer != "none" {
		t.Errorf("answer = %q, want the trimmed text", answer)
	}
	if len(runner.prompts) != 1 || runner.prompts[0] != "judge this" {
		t.Errorf("prompts = %v", runner.prompts)
	}
	if len(runner.opts) == 0 {
		t.Error("the one-shot ran with no options: it must be one turn with no builtin tools")
	}
}

// An oversized prompt is REFUSED, never cut.
//
// The core budgets the window and always emits the closed answer format, which
// is the LAST thing in the prompt — so clamping the finished string took the
// verdict contract away and left the window that made it too long, and a
// one-shot that cannot see the format answers prose, which the parser reads as
// `none` forever. An error is what the tick already treats as "no verdict", and
// it says so in the log.
func TestTheTriagerRefusesAnOversizedPrompt(t *testing.T) {
	runner := &recordingRunner{answer: "none"}
	triager := newAssistantTriager(runner, nil, "")

	_, err := triager.Triage(context.Background(), strings.Repeat("x", maxTriagePrompt+1))
	if err == nil {
		t.Fatal("a prompt past the cap was sent anyway")
	}
	if !strings.Contains(err.Error(), "standing instructions") {
		t.Errorf("error = %q, want it to name what has grown too long", err)
	}
	if len(runner.prompts) != 0 {
		t.Error("the refusal still paid for a model call")
	}

	// And one at the cap goes whole, tail included: nothing between the core's
	// budget and this guard may quietly reshape a prompt.
	tail := "act: <one sentence>"
	whole := strings.Repeat("y", maxTriagePrompt-len(tail)) + tail
	if _, err := triager.Triage(context.Background(), whole); err != nil {
		t.Fatalf("Triage() = %v", err)
	}
	if len(runner.prompts) != 1 || runner.prompts[0] != whole {
		t.Error("a prompt within the cap did not reach the model unchanged")
	}
}

// No runner is an error rather than a panic, and an error is an error rather
// than an empty verdict: the core decides what silence means.
func TestTheTriagerWithoutARunnerFails(t *testing.T) {
	if _, err := newAssistantTriager(nil, nil, "").Triage(context.Background(), "x"); err == nil {
		t.Error("a triager with no runner answered")
	}
	failing := newAssistantTriager(&recordingRunner{err: errors.New("no CLI")}, nil, "")
	if _, err := failing.Triage(context.Background(), "x"); err == nil {
		t.Error("a failing runner answered")
	}
}

// The interval has three answers and each one matters: the default, off, and a
// warning plus the default for anything unreadable. Nothing refuses to boot.
func TestHeartbeatIntervalResolution(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want time.Duration
	}{
		{"", assistant.DefaultHeartbeatInterval},
		{"   ", assistant.DefaultHeartbeatInterval},
		{"30m", 30 * time.Minute},
		{"0", 0},
		{"-5m", 0},
		{"1s", minHeartbeatInterval},
		{"every so often", assistant.DefaultHeartbeatInterval},
	} {
		if got := assistantHeartbeatInterval(tt.in); got != tt.want {
			t.Errorf("assistantHeartbeatInterval(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

// An unreadable digest time is no timed digest, never a guessed hour.
func TestDigestAtResolution(t *testing.T) {
	if got := assistantDigestAt("08:30"); got != (assistant.DigestTime{Hour: 8, Minute: 30, Set: true}) {
		t.Errorf("assistantDigestAt = %+v", got)
	}
	for _, bad := range []string{"", "half eight", "25:00"} {
		if got := assistantDigestAt(bad); got.Set {
			t.Errorf("assistantDigestAt(%q) = %+v, want no timed digest", bad, got)
		}
	}
}

// The two packages spell the origin themselves — internal/assistant does not
// import the session pipeline — so something has to hold them together, and this
// is the one place that imports both.
func TestTheOriginVocabulariesAgree(t *testing.T) {
	if assistant.OriginAssistant != session.OriginAssistant {
		t.Fatalf("assistant says %q and session says %q",
			assistant.OriginAssistant, session.OriginAssistant)
	}
}
