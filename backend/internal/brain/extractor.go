package brain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
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
	runner     msggen.Runner
	model      claudecli.Model
	aggressive bool // aggressive Reorganize prompt (collapse granular facts into broad rules)
}

var (
	_ memory.Extractor = (*ClaudeExtractor)(nil)
	_ memory.Promoter  = (*ClaudeExtractor)(nil)
)

// ExtractorOption tunes a ClaudeExtractor at construction. Policy knobs live here
// (in the glue), keeping the memory core's Extractor contract model-agnostic.
type ExtractorOption func(*ClaudeExtractor)

// WithAggressiveReorganize swaps the conservative reorganize prompt for one that
// actively collapses families of granular facts into broader rules — the "shrink a
// bloated scope" mode. Safe because reorganization is preview-gated (the user
// reviews the plan before applying) and still bounded by the over-deletion guard.
func WithAggressiveReorganize() ExtractorOption {
	return func(e *ClaudeExtractor) { e.aggressive = true }
}

// NewClaudeExtractor returns an Extractor that calls the given model via runner.
// See ClaudeExtractor for model-selection guidance; the caller must choose a
// model (there is no library default).
func NewClaudeExtractor(runner msggen.Runner, model claudecli.Model, opts ...ExtractorOption) *ClaudeExtractor {
	e := &ClaudeExtractor{runner: runner, model: model}
	for _, o := range opts {
		o(e)
	}
	return e
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
	`"properties":{"memories":{"type":"array","maxItems":3,"items":{` +
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
- how the project works at a high level: conventions, architecture, where things live
- build/test/tooling commands and genuine gotchas
- the user's durable preferences, working style, and identity

Prefer FEWER, BROADER facts. A good memory is a general rule someone would want to know BEFORE working here — not an implementation detail.

DO NOT record:
- transient task state, one-off debugging, or anything specific to a single change
- low-level trivia easily rediscovered by reading the code (exact timings, field names, specific flag values) UNLESS it is a surprising gotcha
- facts about OTHER projects this session merely references — record only what is about THIS project and this user
- secrets or tokens

Return ONLY a JSON object {"memories": [...]} with AT MOST 3 items, each {"text": <concise standalone fact under 20 words>, "category": <one of: fact, preference, project, identity, goal>}.
Prefer 0-2 high-signal facts over filling the list. Use an empty array if nothing durable is present. No prose, no code fences.`

// promoteSchema constrains Promote output to {"promotions":[{text,category,subsumes}]}.
const promoteSchema = `{"type":"object","additionalProperties":false,"required":["promotions"],` +
	`"properties":{"promotions":{"type":"array","items":{` +
	`"type":"object","additionalProperties":false,"required":["text","category","subsumes"],` +
	`"properties":{"text":{"type":"string","minLength":1,"maxLength":240},` +
	`"category":{"type":"string","enum":[` + categoryEnumJSON + `]},` +
	`"subsumes":{"type":"array","items":{"type":"string"}}}}}}}`

const promoteSystemPrompt = `You curate a SHARED, cross-project "global" memory from per-project facts.

You are given project facts as {"facts": [{"id","scope","text","category"}]}. "scope" identifies the project; facts in different scopes come from different projects.

Promote a fact to global ONLY when it is useful in EVERY project, not just one:
- the SAME fact recurs across two or more different scopes (a shared convention, tool or workflow), OR
- it is inherently about the USER, not a codebase: identity, contact, or a durable personal preference / working style that holds across projects.

Do NOT promote codebase-specific facts: where files live, a project's architecture, build/test commands, project-specific gotchas. When in doubt, do NOT promote — global facts are injected into EVERY session, so noise is costly.

For each promotion: write ONE concise canonical fact (under 20 words) and list in "subsumes" the ids of the project facts it replaces (include every duplicate across scopes; for a single user-level fact, its one id).

Return ONLY a JSON object {"promotions": [{"text","category","subsumes":["id",...]}]}. Empty promotions if nothing qualifies. No prose, no code fences.`

const reorganizeSystemPrompt = `You are a CONSERVATIVE memory curator. You are given facts as a JSON object {"facts": [{"id","text","category"}]}.

Merge only TRUE duplicates and rewrite genuinely vague entries to be clearer. Related specific facts MAY be abstracted into one general rule.

Rules:
- Keep the "id" of any fact you retain (you may change its text/category).
- Use an EMPTY id ("") for a NEW abstracted fact.
- NEVER invent ids that were not in the input.
- When in doubt, keep the fact unchanged.

Return ONLY a JSON object {"facts": [{"id","text","category"}]}. No prose, no code fences.`

// reorganizeAggressiveSystemPrompt is the "shrink a bloated scope" curator. It is
// deliberately less conservative: it collapses families of overlapping, granular
// facts into a few broad rules and drops low-signal trivia, trading some detail for
// a high-signal scope. The id rules are IDENTICAL to the conservative prompt (the
// apply step drops invented ids and the over-deletion guard still applies), so an
// aggressive plan is just as safe to replay — it only proposes deeper merges.
const reorganizeAggressiveSystemPrompt = `You are an AGGRESSIVE memory curator. Your goal is a SMALL, HIGH-SIGNAL set of durable facts. You are given facts as a JSON object {"facts": [{"id","text","category"}]}.

This scope is bloated with overlapping, overly specific entries. Consolidate hard:
- MERGE every group of facts about the same topic into ONE broader fact. Several narrow facts that share a subject should become a single general rule.
- ABSTRACT families of specific examples ("X does A", "X does B", "X does C") into the underlying principle ("X does A/B/C…").
- REWRITE verbose entries to be concise and self-contained.
- DROP low-signal trivia that a future reader could rediscover from the code, UNLESS it is a surprising gotcha. To drop a fact, simply omit it from the output.
- Prefer FEWER, broader facts. A good memory is a general rule someone wants to know BEFORE working here.

Do NOT lose genuinely distinct knowledge — only collapse what overlaps. Keep the meaning; shed the redundancy.

Rules:
- Keep the "id" of any fact you retain (you may change its text/category).
- Use an EMPTY id ("") for a NEW abstracted/merged fact.
- NEVER invent ids that were not in the input.

Return ONLY a JSON object {"facts": [{"id","text","category"}]}. No prose, no code fences.`

// reorganizePrompt selects the curator prompt for the extractor's configured mode.
func (e *ClaudeExtractor) reorganizePrompt() string {
	if e.aggressive {
		return reorganizeAggressiveSystemPrompt
	}
	return reorganizeSystemPrompt
}

// ReorganizeModeAggressive is the request/UI value selecting the aggressive curator.
const ReorganizeModeAggressive = "aggressive"

// AggressiveMinSurvivorRatio is the over-deletion guard floor for an aggressive
// Tidy: a bloated scope may collapse to as little as 20% of its facts in one pass
// (the guard then only catches a near-total wipe). Conservative Tidy keeps 0.5.
const AggressiveMinSurvivorRatio = 0.2

// reorganizeModePolicy maps an HTTP/UI mode string to the extractor options and the
// matching over-deletion guard ratio. Unknown/empty resolves to conservative
// (default prompt, 0 ⇒ the core's 0.5 guard). One place owns the conservative ↔
// aggressive policy so the job runner and any future caller stay consistent.
func reorganizeModePolicy(mode string) (opts []ExtractorOption, minSurvivorRatio float64) {
	if strings.EqualFold(strings.TrimSpace(mode), ReorganizeModeAggressive) {
		return []ExtractorOption{WithAggressiveReorganize()}, AggressiveMinSurvivorRatio
	}
	return nil, 0
}

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
// single generation. This is a hard reliability limit, not just an optimization:
// a structured-output reorganize of a large chunk exhausts the model's turn/output
// budget and the CLI exits non-zero (verified on the live brain — ~57 facts
// succeed, ~85 fail with `claudecli: exit 1`), which aborts the whole pass. 50
// keeps a comfortable margin below that wall for the longer facts in bloated scopes.
//
// Chunking is community-aware (chunkByCommunity): facts carry a Community label
// (memory.factsForReorg) and a whole community lands in one chunk whenever it fits,
// so related facts merge across the scope rather than only within an arbitrary
// slice. Cluster-aware chunking is what makes this smaller batch lossless — related
// facts co-locate instead of scattering. A community larger than the batch is split,
// but that is rare (e.g. reviewbot's largest is ~22). A var, not a const, so tests
// can shrink it further.
var maxReorgBatch = 50

// aggressiveMaxReorgBatch is a smaller batch for the aggressive Tidy. Aggressive
// runs precisely on bloated scopes — longer, denser facts whose structured-output
// generation is the most likely to hit the silent `claudecli: exit 1` wall (see the
// maxReorgBatch note and docs/tech-debt.md). A tighter batch trades a few more model
// calls for materially fewer crash-and-retry cycles on exactly the runs that crash.
// Cluster-aware chunking keeps the smaller batch lossless. A var so tests can shrink it.
var aggressiveMaxReorgBatch = 35

// reorgBatchSize returns the per-call fact cap for the extractor's mode.
func (e *ClaudeExtractor) reorgBatchSize() int {
	if e.aggressive {
		return aggressiveMaxReorgBatch
	}
	return maxReorgBatch
}

// Reorganize asks the model to merge/clean a set of facts, chunking large sets by
// topic community and running the chunks with bounded concurrency. On any parse
// failure a chunk returns its input unchanged (a safe no-op), so a failed chunk
// never causes the consolidation core to delete its facts.
func (e *ClaudeExtractor) Reorganize(ctx context.Context, facts []memory.Fact) ([]memory.Fact, error) {
	chunks := chunkByCommunity(facts, e.reorgBatchSize())
	if len(chunks) <= 1 {
		return e.reorganizeBatch(ctx, facts)
	}

	results := make([][]memory.Fact, len(chunks))
	errsByIdx := make([]error, len(chunks))
	memory.RunBounded(ctx, len(chunks), maxParallelReorg, func(idx int) {
		results[idx], errsByIdx[idx] = e.reorganizeBatch(ctx, chunks[idx])
	})
	out := make([]memory.Fact, 0, len(facts))
	for idx := range chunks {
		if errsByIdx[idx] != nil {
			if ctx.Err() != nil {
				return nil, errsByIdx[idx] // cancellation aborts the whole pass
			}
			// One chunk failing (after retries) must not sink the others — a large
			// scope where 8/9 chunks tidy and one transiently crashed is a useful
			// partial pass; keep the failed chunk's facts unchanged and let a re-run
			// pick it up. The over-deletion guard still applies to the merged result.
			slog.Warn("brain: reorganize chunk failed after retries; keeping it unchanged",
				append([]any{"facts", len(chunks[idx])}, cliErrorFields(errsByIdx[idx])...)...)
			out = append(out, chunks[idx]...)
			continue
		}
		out = append(out, results[idx]...)
	}
	return out, nil
}

// chunkByCommunity packs facts into batches of at most max, keeping every topic
// community whole within a single batch whenever it fits. Communities are visited
// in ascending id order and accumulated greedily; small communities share a batch,
// and a community that alone exceeds max is split into max-sized pieces (the only
// case where related facts can still straddle a chunk). Deterministic given the
// deterministic community labels, so the resulting plan is reproducible.
func chunkByCommunity(facts []memory.Fact, max int) [][]memory.Fact {
	if len(facts) == 0 {
		return nil
	}
	if max < 1 {
		max = 1
	}
	order := make([]int, 0)
	groups := make(map[int][]memory.Fact)
	for _, f := range facts {
		if _, ok := groups[f.Community]; !ok {
			order = append(order, f.Community)
		}
		groups[f.Community] = append(groups[f.Community], f)
	}
	sort.Ints(order)

	var chunks [][]memory.Fact
	var cur []memory.Fact
	flush := func() {
		if len(cur) > 0 {
			chunks = append(chunks, cur)
			cur = nil
		}
	}
	for _, c := range order {
		g := groups[c]
		if len(g) > max {
			// A single community too big to co-locate: emit it in max-sized pieces.
			flush()
			for i := 0; i < len(g); i += max {
				end := i + max
				if end > len(g) {
					end = len(g)
				}
				chunks = append(chunks, g[i:end])
			}
			continue
		}
		if len(cur)+len(g) > max {
			flush()
		}
		cur = append(cur, g...)
	}
	flush()
	return chunks
}

// maxParallelReorg caps concurrent Reorganize chunk calls. See memory.maxParallelBatches.
var maxParallelReorg = 4

// reorgMaxAttempts is how many times a single reorganize chunk is tried before it
// gives up — covers the intermittent `claudecli: exit 1` crash on large structured
// generations, which msggen.RunWithRetry doesn't treat as retriable. Bumped to 4
// (from 3): on the live reviewbot aggressive Tidy, chunks that crashed often
// succeeded on a later try, so one more cheap, idempotent attempt converges more
// scopes without a re-run.
const reorgMaxAttempts = 4

// cliErrorFields extracts structured diagnostics from a claudecli failure for
// logging. The silent `claudecli: exit 1` reorganize crash yields no classified
// error and an empty stderr, but claudecli buffers the last raw stdout JSONL lines
// in Error.LastEvents — capturing them here is the only handle we have to root-cause
// it (docs/tech-debt.md). Returns slog key/value pairs; "error" is always present.
func cliErrorFields(err error) []any {
	fields := []any{"error", err}
	var cliErr *claudecli.Error
	if errors.As(err, &cliErr) {
		fields = append(fields, "exitCode", cliErr.ExitCode)
		if cliErr.Stderr != "" {
			fields = append(fields, "stderr", cliErr.Stderr)
		}
		if len(cliErr.LastEvents) > 0 {
			fields = append(fields, "lastEvents", cliErr.LastEvents)
		}
	}
	return fields
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
	prompt := e.reorganizePrompt() + "\n\nFACTS:\n" + string(inJSON) + "\n\nReturn ONLY the JSON object."
	// A large structured-output generation occasionally crashes the CLI subprocess
	// (`claudecli: exit 1`, no events) — a transient failure msggen.RunWithRetry
	// doesn't classify as retriable. Reorganize is idempotent, so retry the whole
	// chunk a few times before giving up; this turns an intermittent crash from a
	// pass-aborting error into a no-op at worst.
	var res *claudecli.BlockingResult
	for attempt := 0; attempt < reorgMaxAttempts; attempt++ {
		if res, err = msggen.RunWithRetry(ctx, e.runner, prompt, e.opts(reorganizeSchema)...); err == nil {
			break
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		slog.Warn("brain: reorganize chunk call failed; retrying",
			append([]any{"attempt", attempt + 1, "of", reorgMaxAttempts, "facts", len(facts)}, cliErrorFields(err)...)...)
	}
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

// Promote selects cross-cutting facts to lift to the global scope. It returns the
// global facts to create, each naming the project-fact ids it subsumes. A promotion
// with no real ids is dropped (it must generalize at least one fact). On any parse
// failure it returns nothing — promotion is best-effort and must not abort a pass.
func (e *ClaudeExtractor) Promote(ctx context.Context, candidates []memory.ScopedFact) ([]memory.Promotion, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	type cj struct {
		ID       string `json:"id"`
		Scope    string `json:"scope"`
		Text     string `json:"text"`
		Category string `json:"category"`
	}
	in := make([]cj, len(candidates))
	for i, c := range candidates {
		in[i] = cj{ID: c.ID, Scope: string(c.Scope), Text: c.Text, Category: string(c.Category)}
	}
	inJSON, err := json.Marshal(struct {
		Facts []cj `json:"facts"`
	}{Facts: in})
	if err != nil {
		return nil, nil
	}
	prompt := promoteSystemPrompt + "\n\nFACTS:\n" + string(inJSON) + "\n\nReturn ONLY the JSON object."
	res, err := msggen.RunWithRetry(ctx, e.runner, prompt, e.opts(promoteSchema)...)
	if err != nil {
		return nil, err
	}
	var wrap struct {
		Promotions []struct {
			Text     string   `json:"text"`
			Category string   `json:"category"`
			Subsumes []string `json:"subsumes"`
		} `json:"promotions"`
	}
	if !decodeWrapped(res, "promotions", &wrap) {
		return nil, nil
	}
	out := make([]memory.Promotion, 0, len(wrap.Promotions))
	for _, p := range wrap.Promotions {
		t := strings.TrimSpace(p.Text)
		if t == "" || len(p.Subsumes) == 0 {
			continue
		}
		out = append(out, memory.Promotion{Text: t, Category: normalizeCategory(p.Category), Subsumes: p.Subsumes})
	}
	return out, nil
}

// refineSchema constrains Refine output to {"text": "..."} — a single rewritten fact.
const refineSchema = `{"type":"object","additionalProperties":false,"required":["text"],` +
	`"properties":{"text":{"type":"string","minLength":1,"maxLength":240}}}`

const refineSystemPrompt = `You refine a SINGLE durable memory fact for a cross-project "global" memory.

You are given the CURRENT fact, optionally the SOURCE facts it was merged from, and an INSTRUCTION from the user. Rewrite the fact to satisfy the instruction while staying faithful to the current fact and sources. Keep it ONE concise, self-contained statement (under 25 words). Do NOT invent information not supported by the current fact or the sources.

Return ONLY a JSON object {"text":"<the rewritten fact>"}. No prose, no code fences.`

// Refine rewrites a single memory fact per a user instruction, optionally informed by
// the project facts it was merged from, and returns the new text. It writes nothing —
// the caller shows the draft and the user decides whether to save it. Unlike Promote
// (best-effort, swallows errors), this is an interactive request, so a failure is
// returned for the UI to surface.
func (e *ClaudeExtractor) Refine(ctx context.Context, current string, sources []memory.SubsumedSource, instruction string) (string, error) {
	type src struct {
		Scope string `json:"scope"`
		Text  string `json:"text"`
	}
	in := struct {
		Current     string `json:"current"`
		Sources     []src  `json:"sources,omitempty"`
		Instruction string `json:"instruction"`
	}{Current: current, Instruction: instruction}
	for _, s := range sources {
		in.Sources = append(in.Sources, src{Scope: string(s.Scope), Text: s.Text})
	}
	payload, err := json.Marshal(in)
	if err != nil {
		return "", err
	}
	prompt := refineSystemPrompt + "\n\nINPUT:\n" + string(payload) + "\n\nReturn ONLY the JSON object."
	res, err := msggen.RunWithRetry(ctx, e.runner, prompt, e.opts(refineSchema)...)
	if err != nil {
		return "", err
	}
	raw := []byte(res.Text)
	if len(res.StructuredOutput) > 0 {
		raw = res.StructuredOutput
	}
	var out struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &out) != nil || strings.TrimSpace(out.Text) == "" {
		return "", fmt.Errorf("brain: refine produced no usable text")
	}
	return strings.TrimSpace(out.Text), nil
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
