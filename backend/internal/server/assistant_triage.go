package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	claudecli "github.com/allbin/claudecli-go"

	"github.com/mdjarv/agentique/backend/internal/assistant"
	"github.com/mdjarv/agentique/backend/internal/msggen"
	"github.com/mdjarv/agentique/backend/internal/providers"
)

// The heartbeat's triage step: one Haiku one-shot, on the auto-namer's
// precedent.
//
// This is where the assistant's cheapest judgement lives. The gate before it is
// a row count, so most ticks cost nothing at all; when something has happened,
// this reads the enabled policies and the journal and answers one line. It is
// deliberately NOT the head: a head turn is a subprocess with the whole verb
// table and a ten-minute budget, and paying that every fifteen minutes to be
// told "nothing" is the thing that would make autonomy not worth having.
//
// It runs through [session.BlockingRunner] — the same seam the auto-namer and
// the summariser use — so agentique still never execs a provider CLI itself.

// maxTriagePrompt is the last-resort REFUSAL, and deliberately not a cut.
//
// The core budgets the one part of the prompt that grows without bound — the
// journal window, bounded at sixty entries, at a size per line and at a size
// for the block — and always emits the intro and the closed answer format
// whole. So a prompt that arrives past this is one whose STANDING INSTRUCTIONS
// have grown past what a single shot can judge, which is the case the M0 design
// already answers: "if policies grow past what one shot can judge the answer is
// a second shot per policy, never the head".
//
// Cutting it was the fault this replaces. The answer format is the LAST thing
// in the prompt, so clamping the finished string took the closed verdict
// contract away and left the window that made it too long; the one-shot then
// answered prose, and prose parses as `none` forever, silently. An error is
// what the tick already treats as "no verdict", and it says so in the log.
const maxTriagePrompt = 64000

// assistantHeartbeatInterval reads [assistant] heartbeat-interval.
//
// Three answers and each is deliberate: empty is the default, "0" (or any
// non-positive duration) is OFF because disabling autonomy has to be spellable,
// and anything unparsable is a warning and the default. Nothing here refuses to
// boot — a mistyped interval must not cost somebody their server, and the
// warning names the value it could not read.
func assistantHeartbeatInterval(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return assistant.DefaultHeartbeatInterval
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		slog.Warn("assistant: [assistant] heartbeat-interval is not a duration; using the default",
			"value", raw, "default", assistant.DefaultHeartbeatInterval, "error", err)
		return assistant.DefaultHeartbeatInterval
	}
	if d <= 0 {
		// Not a mistake: "0" is how the heartbeat is turned off.
		return 0
	}
	if d < minHeartbeatInterval {
		slog.Warn("assistant: [assistant] heartbeat-interval is shorter than the floor; using the floor",
			"value", raw, "floor", minHeartbeatInterval)
		return minHeartbeatInterval
	}
	return d
}

// minHeartbeatInterval is the shortest tick the server will run.
//
// The gate is cheap but it is not free: it is a query, a policy read and — once
// anything has happened — a Haiku call per tick. A minute is already far more
// often than a person's attention moves, and a config saying "1s" is a mistake
// rather than a preference.
const minHeartbeatInterval = time.Minute

// assistantDigestAt reads [assistant] digest-at, an empty or unreadable value
// meaning no timed digest. The digest control and the assistant's own verb are
// unaffected either way: this is only the clock.
func assistantDigestAt(raw string) assistant.DigestTime {
	at, err := assistant.ParseDigestAt(raw)
	if err != nil {
		slog.Warn("assistant: [assistant] digest-at is not a wall-clock time; no timed digest",
			"value", raw, "error", err)
		return assistant.DigestTime{}
	}
	return at
}

// assistantTriager is [assistant.Triager] over the blocking runner.
//
// model is a FAMILY NAME resolved through the catalog, or "" for the Haiku
// family — no model id is spelled here, on the model-catalog rule: a new
// upstream Haiku must not require an agentique release.
type assistantTriager struct {
	runner  msggen.Runner
	catalog *providers.Catalog
	model   string
}

func newAssistantTriager(runner msggen.Runner, catalog *providers.Catalog, model string) *assistantTriager {
	return &assistantTriager{runner: runner, catalog: catalog, model: strings.TrimSpace(model)}
}

// Triage implements assistant.Triager.
//
// The options are the persona service's Haiku set: one turn, no builtin tools,
// no slash commands, no settings sources. A triage step with a shell would be a
// triage step that can act, and the whole point of it is that it cannot — it
// answers one word, and the core decides what that word means.
func (t *assistantTriager) Triage(ctx context.Context, prompt string) (string, error) {
	if t.runner == nil {
		return "", errors.New("no blocking runner, so nothing can triage")
	}
	if len(prompt) > maxTriagePrompt {
		return "", fmt.Errorf("the triage prompt is %d bytes, past the %d one shot may judge: "+
			"the standing instructions are too long to triage together", len(prompt), maxTriagePrompt)
	}

	result, err := msggen.RunWithRetry(ctx, t.runner, prompt, t.options(ctx)...)
	if err != nil {
		return "", fmt.Errorf("triage: %w", err)
	}
	if result == nil {
		return "", errors.New("triage answered nothing")
	}
	return strings.TrimSpace(result.Text), nil
}

// options is the one-shot's flags.
func (t *assistantTriager) options(ctx context.Context) []claudecli.Option {
	return []claudecli.Option{
		claudecli.WithModel(t.slug(ctx)),
		claudecli.WithMaxTurns(1),
		claudecli.WithBuiltinTools(""),
		claudecli.WithSkipVersionCheck(),
		claudecli.WithStrictMCPConfig(),
		claudecli.WithDisableSlashCommands(),
		claudecli.WithSettingSources(""),
	}
}

// slug resolves the configured family to something the CLI accepts.
//
// An unset or unresolvable family falls back to the Haiku the contract names as
// the default, rather than to whatever a session would get: this is the step
// that must stay cheap, and a triage running on Opus every fifteen minutes is a
// misconfiguration that should cost a log line, not an allowance.
func (t *assistantTriager) slug(ctx context.Context) claudecli.Model {
	if t.model == "" || t.catalog == nil {
		return claudecli.ModelHaiku
	}
	if model, ok := t.catalog.ResolveFamily(ctx, "claude", t.model); ok {
		return claudecli.Model(model.Slug)
	}
	return claudecli.ModelHaiku
}
