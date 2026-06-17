package brain

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	claudecli "github.com/allbin/claudecli-go"

	"github.com/mdjarv/agentique/backend/internal/memory"
	"github.com/mdjarv/agentique/backend/internal/msggen"
)

// ClaudeExtractor implements memory.Extractor by calling a Claude model through
// the shared blocking-runner path (msggen). It needs no embeddings or external
// API — extraction is a plain chat completion over the configured Claude CLI,
// constrained to a JSON schema so the model output is validated at generation
// time rather than scraped from prose.
//
// The model is a constructor parameter: the CALLER owns model policy, there is
// deliberately no default. Suggested choices — a cheap model (claudecli.ModelHaiku)
// for high-volume Extract during capture/backfill; a strong model
// (claudecli.ModelOpus) for the Reorganize "sleep" pass, where judgment and
// faithfulness matter and the call is infrequent. Keeping the choice out of this
// type lets the memory core lift to agentkit without carrying a model decision.
type ClaudeExtractor struct {
	runner msggen.Runner
	model  claudecli.Model
}

var _ memory.Extractor = (*ClaudeExtractor)(nil)

// NewClaudeExtractor returns an Extractor that calls the given model via runner.
// See ClaudeExtractor for model-selection guidance; the caller must choose a
// model (there is no library default).
func NewClaudeExtractor(runner msggen.Runner, model claudecli.Model) *ClaudeExtractor {
	return &ClaudeExtractor{runner: runner, model: model}
}

// ParseModel maps a model name to a claudecli.Model, rejecting unknown names so a
// typo never silently falls through to a CLI default. Callers (CLI flags, the
// consolidate HTTP body) own model policy and validate user input through here.
func ParseModel(s string) (claudecli.Model, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "haiku":
		return claudecli.ModelHaiku, nil
	case "sonnet":
		return claudecli.ModelSonnet, nil
	case "opus":
		return claudecli.ModelOpus, nil
	default:
		return "", fmt.Errorf("brain: unknown model %q (want haiku|sonnet|opus)", s)
	}
}

// opts builds the per-call CLI options. When schema is non-empty the model is
// constrained to it (claudecli reads the result back from structured_output).
//
// Structured output costs an extra turn: the CLI generates, then emits/validates
// the structured_output on a second turn, so --max-turns 1 fails with
// error_max_turns. We give schema calls headroom; plain calls stay single-turn.
func (e *ClaudeExtractor) opts(schema string) []claudecli.Option {
	maxTurns := 1
	if schema != "" {
		maxTurns = 4
	}
	o := []claudecli.Option{
		claudecli.WithModel(e.model),
		claudecli.WithMaxTurns(maxTurns),
		claudecli.WithBuiltinTools(""),
		claudecli.WithSkipVersionCheck(),
		claudecli.WithStrictMCPConfig(),
		claudecli.WithDisableSlashCommands(),
		claudecli.WithSettingSources(""),
	}
	if schema != "" {
		o = append(o, claudecli.WithJSONSchema(schema))
	}
	return o
}

const extractMaxChars = 12000

// Categories the model may assign. Mirrors normalizeCategory; encoded as a schema
// enum so an out-of-vocabulary category cannot be produced (no silent coercion).
const categoryEnumJSON = `"fact","preference","project","identity","goal","contact","task"`

// extractSchema constrains Extract output to {"memories":[{text,category}]}. The
// root is an object (the shape claudecli's --json-schema expects); maxItems caps
// the batch and maxLength enforces the "concise" rule structurally.
const extractSchema = `{"type":"object","additionalProperties":false,"required":["memories"],` +
	`"properties":{"memories":{"type":"array","maxItems":5,"items":{` +
	`"type":"object","additionalProperties":false,"required":["text","category"],` +
	`"properties":{"text":{"type":"string","minLength":1,"maxLength":160},` +
	`"category":{"type":"string","enum":[` + categoryEnumJSON + `]}}}}}}`

// reorganizeSchema constrains Reorganize output to {"facts":[{id,text,category}]}.
// The schema enforces SHAPE only; the semantic invariants (ids must be real, no
// over-deletion) live in the consolidation core's applyReorg.
const reorganizeSchema = `{"type":"object","additionalProperties":false,"required":["facts"],` +
	`"properties":{"facts":{"type":"array","items":{` +
	`"type":"object","additionalProperties":false,"required":["id","text","category"],` +
	`"properties":{"id":{"type":"string"},"text":{"type":"string","minLength":1,"maxLength":240},` +
	`"category":{"type":"string","enum":[` + categoryEnumJSON + `]}}}}}}`

const extractSystemPrompt = `You extract DURABLE, REUSABLE facts about a software project and the user from a coding-session transcript.

Extract ONLY long-lived facts worth recalling in FUTURE, unrelated sessions:
- project conventions, architecture, where things live
- build/test/tooling commands and gotchas
- the user's durable preferences and identity

IGNORE: transient task state, one-off debugging, anything specific to a single change, and secrets/tokens.

Return ONLY a JSON object {"memories": [...]} with at most 5 items, each {"text": <concise standalone fact under 20 words>, "category": <one of: fact, preference, project, identity, goal>}.
Use an empty memories array if nothing durable is present. No prose, no code fences.`

const reorganizeSystemPrompt = `You are a CONSERVATIVE memory curator. You are given facts as a JSON object {"facts": [{"id","text","category"}]}.

Merge only TRUE duplicates and rewrite genuinely vague entries to be clearer. Related specific facts MAY be abstracted into one general rule.

Rules:
- Keep the "id" of any fact you retain (you may change its text/category).
- Use an EMPTY id ("") for a NEW abstracted fact.
- NEVER invent ids that were not in the input.
- When in doubt, keep the fact unchanged.

Return ONLY a JSON object {"facts": [{"id","text","category"}]}. No prose, no code fences.`

// Extract distills the given episodes (a transcript or transcript chunk) into
// durable candidate facts.
func (e *ClaudeExtractor) Extract(ctx context.Context, episodes []string) ([]memory.Candidate, error) {
	transcript := strings.TrimSpace(strings.Join(episodes, "\n"))
	if transcript == "" {
		return nil, nil
	}
	if len(transcript) > extractMaxChars {
		transcript = transcript[:extractMaxChars]
	}
	prompt := extractSystemPrompt + "\n\nTRANSCRIPT:\n" + transcript + "\n\nReturn ONLY the JSON object."
	res, err := msggen.RunWithRetry(ctx, e.runner, prompt, e.opts(extractSchema)...)
	if err != nil {
		return nil, err
	}
	type item struct {
		Text     string `json:"text"`
		Category string `json:"category"`
	}
	var wrap struct {
		Memories []item `json:"memories"`
	}
	// A malformed response yields no candidates rather than an error — extraction
	// is best-effort and must not abort a batch.
	decodeWrapped(res, "memories", &wrap)
	items := wrap.Memories
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

// maxReorgBatch bounds how many facts go to the model in one Reorganize call.
// Large scopes are chunked so the model never has to re-emit the whole set in a
// single generation (which risks hitting the output limit and silently dropping
// facts). A var, not a const, so tests can shrink it. Trade-off: duplicates that
// land in different chunks won't merge in one pass — but each pass that merges
// changes the set, so re-running Tidy converges. Clustering related facts into
// the same chunk is a future optimization.
var maxReorgBatch = 100

// Reorganize asks the model to merge/clean a set of facts, chunking large sets.
// On any parse failure a chunk returns its input unchanged (a safe no-op), so a
// failed chunk never causes the consolidation core to delete its facts.
func (e *ClaudeExtractor) Reorganize(ctx context.Context, facts []memory.Fact) ([]memory.Fact, error) {
	if len(facts) <= maxReorgBatch {
		return e.reorganizeBatch(ctx, facts)
	}
	out := make([]memory.Fact, 0, len(facts))
	for i := 0; i < len(facts); i += maxReorgBatch {
		end := i + maxReorgBatch
		if end > len(facts) {
			end = len(facts)
		}
		got, err := e.reorganizeBatch(ctx, facts[i:end])
		if err != nil {
			return nil, err
		}
		out = append(out, got...)
	}
	return out, nil
}

func (e *ClaudeExtractor) reorganizeBatch(ctx context.Context, facts []memory.Fact) ([]memory.Fact, error) {
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
	// Present the input in the same {"facts":[...]} shape we ask back.
	inJSON, err := json.Marshal(struct {
		Facts []fj `json:"facts"`
	}{Facts: in})
	if err != nil {
		return facts, nil
	}
	prompt := reorganizeSystemPrompt + "\n\nFACTS:\n" + string(inJSON) + "\n\nReturn ONLY the JSON object."
	res, err := msggen.RunWithRetry(ctx, e.runner, prompt, e.opts(reorganizeSchema)...)
	if err != nil {
		return nil, err
	}
	var wrap struct {
		Facts []fj `json:"facts"`
	}
	// An empty/unparseable result almost certainly means a parse miss or a model
	// failure, not an instruction to delete every fact — treat it as a no-op. (A
	// real reorganization always returns at least the facts it keeps.)
	if !decodeWrapped(res, "facts", &wrap) || len(wrap.Facts) == 0 {
		return facts, nil
	}
	out := make([]memory.Fact, 0, len(wrap.Facts))
	for _, it := range wrap.Facts {
		t := strings.TrimSpace(it.Text)
		if t == "" {
			continue
		}
		out = append(out, memory.Fact{ID: strings.TrimSpace(it.ID), Text: t, Category: normalizeCategory(it.Category)})
	}
	if len(out) == 0 {
		return facts, nil
	}
	return out, nil
}

// decodeWrapped unmarshals a {"<field>":[...]} payload into dst, preferring the
// schema-validated structured_output and falling back to scraping the text
// response. It tolerates a model that drops the object wrapper and returns a bare
// array, because parseJSONArray over the same bytes extracts the inner array in
// both cases. Returns true when it populated dst with a non-nil slice.
func decodeWrapped(res *claudecli.BlockingResult, field string, dst any) bool {
	if res == nil {
		return false
	}
	raw := []byte(res.Text)
	if len(res.StructuredOutput) > 0 {
		raw = res.StructuredOutput
	}
	if json.Unmarshal(raw, dst) == nil && wrappedNonEmpty(dst, field) {
		return true
	}
	// Fallback: pull a bare top-level array out of prose/fences and decode it into
	// the wrapper's single array field by wrapping it back up.
	arr := parseJSONArray(string(raw))
	rewrapped := append(append([]byte(`{"`+field+`":`), arr...), '}')
	return json.Unmarshal(rewrapped, dst) == nil
}

// wrappedNonEmpty reports whether the wrapper's named array field decoded to a
// non-nil slice — distinguishing "model returned []" / wrong shape from a real
// payload so the caller can try the array fallback.
func wrappedNonEmpty(dst any, field string) bool {
	b, err := json.Marshal(dst)
	if err != nil {
		return false
	}
	var probe map[string]json.RawMessage
	if json.Unmarshal(b, &probe) != nil {
		return false
	}
	v, ok := probe[field]
	return ok && len(v) > 0 && string(v) != "null"
}

// parseJSONArray extracts the first top-level JSON array from a model response,
// tolerating <think> blocks, ```json fences and surrounding prose. Retained as
// the fallback path when structured output is unavailable.
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
