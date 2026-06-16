package brain

import (
	"context"
	"encoding/json"
	"strings"

	claudecli "github.com/allbin/claudecli-go"

	"github.com/mdjarv/agentique/backend/internal/memory"
	"github.com/mdjarv/agentique/backend/internal/msggen"
)

// HaikuExtractor implements memory.Extractor by calling a cheap Claude model
// (Haiku) through the same blocking-runner path the rest of agentique uses
// (msggen / persona). It needs no embeddings or external API — extraction is a
// plain chat completion over the configured Claude CLI.
type HaikuExtractor struct {
	runner msggen.Runner
}

var _ memory.Extractor = (*HaikuExtractor)(nil)

// NewHaikuExtractor returns an Extractor over the given blocking runner.
func NewHaikuExtractor(runner msggen.Runner) *HaikuExtractor {
	return &HaikuExtractor{runner: runner}
}

func haikuExtractorOpts() []claudecli.Option {
	return []claudecli.Option{
		claudecli.WithModel(claudecli.ModelHaiku),
		claudecli.WithMaxTurns(1),
		claudecli.WithBuiltinTools(""),
		claudecli.WithSkipVersionCheck(),
		claudecli.WithStrictMCPConfig(),
		claudecli.WithDisableSlashCommands(),
		claudecli.WithSettingSources(""),
	}
}

const extractMaxChars = 12000

const extractSystemPrompt = `You extract DURABLE, REUSABLE facts about a software project and the user from a coding-session transcript.

Extract ONLY long-lived facts worth recalling in FUTURE, unrelated sessions:
- project conventions, architecture, where things live
- build/test/tooling commands and gotchas
- the user's durable preferences and identity

IGNORE: transient task state, one-off debugging, anything specific to a single change, and secrets/tokens.

Return ONLY a JSON array of at most 5 objects, each {"text": <concise standalone fact under 20 words>, "category": <one of: fact, preference, project, identity, goal>}.
Return [] if nothing durable is present. No prose, no code fences.`

const reorganizeSystemPrompt = `You are a CONSERVATIVE memory curator. You are given facts as a JSON array of {"id","text","category"}.

Merge only TRUE duplicates and rewrite genuinely vague entries to be clearer. Related specific facts MAY be abstracted into one general rule.

Rules:
- Keep the "id" of any fact you retain (you may change its text/category).
- Use an EMPTY id ("") for a NEW abstracted fact.
- NEVER invent ids that were not in the input.
- When in doubt, keep the fact unchanged.

Return ONLY a JSON array of {"id","text","category"}. No prose, no code fences.`

// Extract distills the given episodes (a transcript or transcript chunk) into
// durable candidate facts.
func (e *HaikuExtractor) Extract(ctx context.Context, episodes []string) ([]memory.Candidate, error) {
	transcript := strings.TrimSpace(strings.Join(episodes, "\n"))
	if transcript == "" {
		return nil, nil
	}
	if len(transcript) > extractMaxChars {
		transcript = transcript[:extractMaxChars]
	}
	prompt := extractSystemPrompt + "\n\nTRANSCRIPT:\n" + transcript + "\n\nReturn ONLY the JSON array."
	res, err := msggen.RunWithRetry(ctx, e.runner, prompt, haikuExtractorOpts()...)
	if err != nil {
		return nil, err
	}
	var items []struct {
		Text     string `json:"text"`
		Category string `json:"category"`
	}
	// A malformed model response yields no candidates rather than an error —
	// extraction is best-effort and must not abort a batch.
	if err := json.Unmarshal(parseJSONArray(res.Text), &items); err != nil {
		return nil, nil
	}
	out := make([]memory.Candidate, 0, len(items))
	for _, it := range items {
		t := strings.TrimSpace(it.Text)
		if t == "" {
			continue
		}
		out = append(out, memory.Candidate{Text: t, Category: normalizeCategory(it.Category)})
	}
	return out, nil
}

// Reorganize asks the model to merge/clean a set of facts. On any parse failure
// it returns the input unchanged (a safe no-op for the consolidation pass).
func (e *HaikuExtractor) Reorganize(ctx context.Context, facts []memory.Fact) ([]memory.Fact, error) {
	if len(facts) == 0 {
		return facts, nil
	}
	type fj struct {
		ID       string `json:"id"`
		Text     string `json:"text"`
		Category string `json:"category"`
	}
	in := make([]fj, len(facts))
	for i, f := range facts {
		in[i] = fj{ID: f.ID, Text: f.Text, Category: string(f.Category)}
	}
	inJSON, err := json.Marshal(in)
	if err != nil {
		return facts, nil
	}
	prompt := reorganizeSystemPrompt + "\n\nFACTS:\n" + string(inJSON) + "\n\nReturn ONLY the JSON array."
	res, err := msggen.RunWithRetry(ctx, e.runner, prompt, haikuExtractorOpts()...)
	if err != nil {
		return nil, err
	}
	var items []fj
	if err := json.Unmarshal(parseJSONArray(res.Text), &items); err != nil {
		return facts, nil
	}
	// An empty result almost certainly means a parse miss or a model failure, not
	// an instruction to delete every fact — treat it as a no-op. (A real
	// reorganization always returns at least the facts it keeps.)
	if len(items) == 0 {
		return facts, nil
	}
	out := make([]memory.Fact, 0, len(items))
	for _, it := range items {
		t := strings.TrimSpace(it.Text)
		if t == "" {
			continue
		}
		out = append(out, memory.Fact{ID: strings.TrimSpace(it.ID), Text: t, Category: normalizeCategory(it.Category)})
	}
	return out, nil
}

// parseJSONArray extracts the first top-level JSON array from a model response,
// tolerating <think> blocks, ```json fences and surrounding prose.
func parseJSONArray(s string) []byte {
	// strip reasoning-model <think>…</think> blocks
	for {
		start := strings.Index(s, "<think>")
		if start < 0 {
			break
		}
		end := strings.Index(s, "</think>")
		if end < 0 || end < start {
			s = s[:start]
			break
		}
		s = s[:start] + s[end+len("</think>"):]
	}
	s = strings.ReplaceAll(s, "```json", "")
	s = strings.ReplaceAll(s, "```", "")
	i := strings.Index(s, "[")
	j := strings.LastIndex(s, "]")
	if i < 0 || j < i {
		return []byte("[]")
	}
	return []byte(s[i : j+1])
}

func normalizeCategory(c string) memory.Category {
	switch strings.ToLower(strings.TrimSpace(c)) {
	case "identity":
		return memory.CategoryIdentity
	case "preference":
		return memory.CategoryPreference
	case "contact":
		return memory.CategoryContact
	case "project":
		return memory.CategoryProject
	case "goal":
		return memory.CategoryGoal
	case "task":
		return memory.CategoryTask
	default:
		return memory.CategoryFact
	}
}
