package server

import (
	"context"
	"errors"
	"fmt"
	"strings"

	claudecli "github.com/allbin/claudecli-go"

	"github.com/mdjarv/agentique/backend/internal/assistant"
	"github.com/mdjarv/agentique/backend/internal/msggen"
	"github.com/mdjarv/agentique/backend/internal/providers"
)

// The compaction step's one model call: a Haiku one-shot that folds a day of the
// journal into a sentence (docs/assistant.md, the M5 contract).
//
// The same seam as the triager, deliberately spelled the same way — one method,
// raw text out, every rule about what the text MEANS in the core. What differs is
// the prompt and what it costs to get it wrong: a triage verdict is judged again
// in fifteen minutes, where a day's summary is what is left after that day's
// entries are deleted. So the failures here are errors rather than guesses, and
// the core's rule is that a day whose summary did not arrive is a day that is not
// folded.
//
// It runs through [session.BlockingRunner], so agentique still never execs a
// provider CLI itself.

// maxSummarizePrompt is the last-resort REFUSAL, on the triager's argument and
// with the same shape: a cut, not a refusal, is what silently breaks a prompt
// whose instructions are at the end of it.
//
// The core already bounds the part that grows — a day is rendered under
// `maxCompactWindowBytes` with the oldest lines dropped, and the framing and the
// answer format are always whole — so a prompt past this is not a busy day. It is
// something else, and the core treats an error as "this day did not fold", which
// is the fail-closed direction: nothing is deleted.
const maxSummarizePrompt = 64000

// assistantSummarizer is [assistant.Summarizer] over the blocking runner.
//
// model is a FAMILY NAME resolved through the catalog, or "" for the Haiku
// family. No model id is spelled here, on the model-catalog rule — and it shares
// the triager's configured family (`[assistant] triage-model`) rather than adding
// a second knob: both are one-shots over the assistant's own bookkeeping, and a
// machine that wants a cheaper or dearer model for one wants it for the other.
type assistantSummarizer struct {
	runner  msggen.Runner
	catalog *providers.Catalog
	model   string
}

func newAssistantSummarizer(runner msggen.Runner, catalog *providers.Catalog, model string) *assistantSummarizer {
	return &assistantSummarizer{runner: runner, catalog: catalog, model: strings.TrimSpace(model)}
}

// Summarize implements assistant.Summarizer.
//
// The options are the persona service's Haiku set, as the triager's are: one
// turn, no builtin tools, no slash commands, no settings sources. A summariser
// with a shell would be a summariser that can act, and what it is reading is
// agent-written text about repository content.
func (s *assistantSummarizer) Summarize(ctx context.Context, prompt string) (string, error) {
	if s.runner == nil {
		return "", errors.New("no blocking runner, so nothing can summarise a day")
	}
	if len(prompt) > maxSummarizePrompt {
		return "", fmt.Errorf("the day's prompt is %d bytes, past the %d one shot may fold",
			len(prompt), maxSummarizePrompt)
	}

	result, err := msggen.RunWithRetry(ctx, s.runner, prompt, s.options(ctx)...)
	if err != nil {
		return "", fmt.Errorf("summarise a day of the journal: %w", err)
	}
	if result == nil {
		return "", errors.New("the summariser answered nothing")
	}
	return strings.TrimSpace(result.Text), nil
}

// options is the one-shot's flags.
func (s *assistantSummarizer) options(ctx context.Context) []claudecli.Option {
	return []claudecli.Option{
		claudecli.WithModel(s.slug(ctx)),
		claudecli.WithMaxTurns(1),
		claudecli.WithBuiltinTools(""),
		claudecli.WithSkipVersionCheck(),
		claudecli.WithStrictMCPConfig(),
		claudecli.WithDisableSlashCommands(),
		claudecli.WithSettingSources(""),
	}
}

// slug resolves the configured family, falling back to Haiku for the reason the
// triager does: this step must stay cheap, and folding thirty days on Opus is a
// misconfiguration that should cost a log line rather than an allowance.
func (s *assistantSummarizer) slug(ctx context.Context) claudecli.Model {
	if s.model == "" || s.catalog == nil {
		return claudecli.ModelHaiku
	}
	if model, ok := s.catalog.ResolveFamily(ctx, "claude", s.model); ok {
		return claudecli.Model(model.Slug)
	}
	return claudecli.ModelHaiku
}

// Both one-shots are the same seam, and this is the assertion that keeps them
// interchangeable at the wiring site.
var (
	_ assistant.Triager    = (*assistantTriager)(nil)
	_ assistant.Summarizer = (*assistantSummarizer)(nil)
)
