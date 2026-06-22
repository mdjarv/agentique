package brain

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	claudecli "github.com/allbin/claudecli-go"

	"github.com/mdjarv/agentique/backend/internal/memory"
	"github.com/mdjarv/agentique/backend/internal/msggen"
)

// The automatic outcome emitter (brain-outcome-signal.md "Automatic outcome emitter",
// RFC-LD decision #2's open branch). It is the AUTOMATIC twin of the agent-volunteered
// MemoryUsed/MemoryFlag tools: a session-end pass that reads the finished transcript,
// recovers the facts recall INJECTED during the session, and asks an LLM judge whether
// each one was used/corroborated (→ MarkAutoHelped) or contradicted (→ Flag). It exists
// because the explicit tools are rarely called, so `helped` stays 0 across the live corpus
// and the strength-weighted decay + operating-contract graduation are starved of signal.
//
// It mirrors LearnFromTranscript: opt-in behind a model (off by default), best-effort
// (a failed judge is logged, never fatal), and transcript-only (no live session state,
// so it survives a restart between the session and its deletion). The injected fact ids
// are recovered from the persisted <brain> envelopes in the prompt events — the same
// blocks RecallBlock wrote and the agent saw — so the judge audits exactly what was shown.

// OutcomeVerdict is the judge's per-fact ruling. It is intentionally a closed enum so a
// malformed/unknown verdict degrades to "do nothing" rather than a silent miscategorization.
type OutcomeVerdict string

const (
	// OutcomeHelped: the transcript shows clear evidence the agent used/relied on the fact,
	// or the conversation confirmed it correct/useful → MarkAutoHelped.
	OutcomeHelped OutcomeVerdict = "helped"
	// OutcomeContradicted: clear evidence the fact is wrong or out of date → Flag (review band).
	OutcomeContradicted OutcomeVerdict = "contradicted"
	// OutcomeNeutral: no strong evidence either way. The conservative default — most facts.
	OutcomeNeutral OutcomeVerdict = "neutral"
)

// JudgedFact is the minimal projection of a recalled fact handed to the judge: its id (to
// reference in the verdict), its text, and its category. No confidence/strength is sent —
// the judge rules on evidence in the transcript, not on the fact's current trust.
type JudgedFact struct {
	ID       string
	Text     string
	Category string
}

// FactVerdict pairs a judged fact id with the ruling and an optional one-line reason
// (stored on a contradiction as the human-review note).
type FactVerdict struct {
	ID      string
	Verdict OutcomeVerdict
	Reason  string
}

// OutcomeJudge rules, from a finished transcript, on whether each surfaced fact helped or
// was contradicted. Kept as an interface (mirroring memory.Extractor) so the orchestration
// is testable with a deterministic fake and the model choice stays in the glue.
type OutcomeJudge interface {
	JudgeOutcomes(ctx context.Context, transcript string, facts []JudgedFact) ([]FactVerdict, error)
}

// OutcomeReport summarizes one emitter pass for logging. Judged is how many distinct
// surfaced facts were sent to the judge; Helped/Flagged are the outcomes actually applied.
type OutcomeReport struct {
	Judged  int
	Helped  int
	Flagged int
}

// factIDPattern matches the id attribute of a recalled <fact> element as written by
// RecallBlock (`<fact id="UUID">…`). RecallBlock leaves the id quote-raw (it is a clean
// UUID), so a simple non-greedy attribute capture is exact; the surrounding prose is
// escaped and never contains the literal `<fact id="`.
var factIDPattern = regexp.MustCompile(`<fact id="([^"]+)">`)

// brainBlockPattern matches a whole <brain>…</brain> envelope so it can be stripped from
// the judge transcript: the facts are sent to the judge explicitly, so leaving the raw
// envelopes inline would only duplicate them and waste the transcript budget.
var brainBlockPattern = regexp.MustCompile(`(?s)<brain>.*?</brain>\s*`)

// extractInjectedFactIDs recovers, in first-seen order and de-duplicated, the ids of every
// fact recall injected during the session, by parsing the <brain> envelopes RecallBlock
// persisted into the prompt events. Delta recall already injects each fact at most once per
// session, but de-duping here keeps the pass idempotent against any future change to that.
func extractInjectedFactIDs(events []TranscriptEvent) []string {
	seen := make(map[string]struct{})
	var ids []string
	for _, ev := range events {
		text := promptText(ev)
		if text == "" {
			continue
		}
		for _, m := range factIDPattern.FindAllStringSubmatch(text, -1) {
			id := m[1]
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	return ids
}

// promptText returns the user-authored content of a prompt-bearing event (where recall is
// injected), or "" for any other event. Recall is only ever prepended to the main turn
// prompt (Session.Query), but user_message is scanned too for robustness.
func promptText(ev TranscriptEvent) string {
	switch ev.Type {
	case "prompt":
		var d struct {
			Prompt string `json:"prompt"`
		}
		if json.Unmarshal([]byte(ev.Data), &d) == nil {
			return d.Prompt
		}
	case "user_message":
		var d struct {
			Content string `json:"content"`
		}
		if json.Unmarshal([]byte(ev.Data), &d) == nil {
			return d.Content
		}
	}
	return ""
}

// ApplyOutcomesFromTranscript is the emitter's orchestration: recover the facts recall
// surfaced this session, fetch the live records (scope-checked, skipping any since deleted
// or edited away), ask the judge to rule on each, and apply the rulings — MarkAutoHelped
// for a clear positive, Flag for a clear contradiction. Conservative by construction: the
// judge defaults to neutral and a neutral ruling changes nothing. Best-effort: a per-fact
// apply error is logged, not fatal. Returns the applied counts.
//
// scope is the session's project scope; only facts in that scope or global are eligible (a
// surfaced fact is always one of those, but the filter is a defensive guard so the emitter
// can never re-rate another project's memory). A judge verdict referencing an id that was
// not surfaced is ignored (anti-hallucination), as is a second verdict for the same id.
func (s *Service) ApplyOutcomesFromTranscript(ctx context.Context, scope memory.Scope, events []TranscriptEvent, judge OutcomeJudge) (OutcomeReport, error) {
	if judge == nil {
		return OutcomeReport{}, nil
	}
	ids := extractInjectedFactIDs(events)
	if len(ids) == 0 {
		return OutcomeReport{}, nil
	}

	eligible := make(map[memory.Scope]struct{})
	for _, sc := range recallScopes(scope) {
		eligible[sc] = struct{}{}
	}

	facts := make([]JudgedFact, 0, len(ids))
	known := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		r, err := s.store.Get(ctx, id)
		if err != nil {
			continue // surfaced earlier but since deleted/consolidated away — nothing to rate
		}
		if _, ok := eligible[r.Scope]; !ok {
			continue // defensive: never touch a fact outside the session's project/global
		}
		facts = append(facts, JudgedFact{ID: r.ID, Text: r.Text, Category: string(r.Category)})
		known[r.ID] = struct{}{}
	}
	if len(facts) == 0 {
		return OutcomeReport{}, nil
	}

	transcript := outcomeTranscript(events)
	if transcript == "" {
		return OutcomeReport{}, nil
	}

	verdicts, err := judge.JudgeOutcomes(ctx, transcript, facts)
	if err != nil {
		return OutcomeReport{}, err
	}

	rep := OutcomeReport{Judged: len(facts)}
	applied := make(map[string]struct{}, len(verdicts))
	for _, v := range verdicts {
		if _, ok := known[v.ID]; !ok {
			continue // judge referenced a fact that wasn't surfaced — ignore
		}
		if _, done := applied[v.ID]; done {
			continue // one outcome per fact per session
		}
		switch v.Verdict {
		case OutcomeHelped:
			if _, err := s.MarkAutoHelped(ctx, v.ID); err != nil {
				slog.Warn("brain: auto-outcome mark-helped failed", "id", v.ID, "error", err)
				continue
			}
			applied[v.ID] = struct{}{}
			rep.Helped++
		case OutcomeContradicted:
			if _, err := s.Flag(ctx, v.ID, autoFlagReason(v.Reason)); err != nil {
				slog.Warn("brain: auto-outcome flag failed", "id", v.ID, "error", err)
				continue
			}
			applied[v.ID] = struct{}{}
			rep.Flagged++
		default:
			// neutral / unknown verdict → no change (the conservative default)
		}
	}
	return rep, nil
}

// autoFlagReason marks an auto-detected contradiction so the human-review queue shows it
// came from the session-end judge, not an agent's MemoryFlag — and falls back to a generic
// note when the judge gave none. The "auto:" prefix is the provenance marker.
func autoFlagReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "auto: contradicted by session transcript (automatic outcome emitter)"
	}
	return "auto: " + reason
}

// outcomeTranscriptMaxChars bounds the transcript handed to the judge. Larger than the
// extractor's budget — judging "was this used?" needs more of the conversation than
// distilling a few facts — but still capped so a long session can't blow the model's
// context/cost. When over budget, the head and tail are kept (the head holds the task and
// first recalls; the tail holds the resolution where "it helped" usually shows up).
const outcomeTranscriptMaxChars = 24000

// outcomeTranscript reconstructs the session transcript for the judge with the <brain>
// recall envelopes stripped (the surfaced facts are sent to the judge explicitly), then
// clamps it to outcomeTranscriptMaxChars keeping head + tail.
func outcomeTranscript(events []TranscriptEvent) string {
	chunks := BuildTranscript(events, 0)
	if len(chunks) == 0 {
		return ""
	}
	t := brainBlockPattern.ReplaceAllString(chunks[0], "")
	t = strings.TrimSpace(t)
	if len(t) <= outcomeTranscriptMaxChars {
		return t
	}
	head := outcomeTranscriptMaxChars * 3 / 5
	tail := outcomeTranscriptMaxChars - head
	return t[:head] + "\n\n…(transcript truncated)…\n\n" + t[len(t)-tail:]
}

// ClaudeOutcomeJudge implements OutcomeJudge via a Claude model over the shared
// blocking-runner path, constrained to a JSON schema — the same machinery as
// ClaudeExtractor. The model is a constructor parameter (the caller owns model policy;
// no library default), matching the rest of the brain glue.
type ClaudeOutcomeJudge struct {
	runner msggen.Runner
	model  claudecli.Model
}

var _ OutcomeJudge = (*ClaudeOutcomeJudge)(nil)

// NewClaudeOutcomeJudge returns an OutcomeJudge that calls the given model via runner.
func NewClaudeOutcomeJudge(runner msggen.Runner, model claudecli.Model) *ClaudeOutcomeJudge {
	return &ClaudeOutcomeJudge{runner: runner, model: model}
}

// outcomeSchema constrains the judge to {"verdicts":[{"id","verdict","reason?"}]}. The
// verdict is a closed enum so an out-of-vocabulary ruling cannot be produced; reason is
// optional and length-bounded (it becomes a review note on a contradiction).
const outcomeSchema = `{"type":"object","additionalProperties":false,"required":["verdicts"],` +
	`"properties":{"verdicts":{"type":"array","items":{` +
	`"type":"object","additionalProperties":false,"required":["id","verdict"],` +
	`"properties":{"id":{"type":"string"},` +
	`"verdict":{"type":"string","enum":["helped","contradicted","neutral"]},` +
	`"reason":{"type":"string","maxLength":200}}}}}}`

const outcomeSystemPrompt = `You audit whether an AI coding agent's RECALLED MEMORY actually helped it during a session.

You are given the session TRANSCRIPT and a JSON list of FACTS that were surfaced to the agent from its long-term memory. For EACH fact, return exactly one verdict:
- "helped": the transcript shows CLEAR evidence the agent used, relied on, or acted in accordance with this fact, OR the conversation explicitly confirmed it was correct/useful.
- "contradicted": the transcript shows CLEAR evidence this fact is WRONG or out of date — the user corrected it, or what actually happened diverges from it.
- "neutral": anything else. This is the DEFAULT.

Be CONSERVATIVE. Only return "helped" or "contradicted" when the evidence in the transcript is explicit and unambiguous. A fact merely being shown is NOT "helped" — there must be evidence it shaped what the agent did or said. When in doubt, return "neutral". A wrong "helped" inflates trust in an unproven fact and a wrong "contradicted" demotes a good one, so prefer false "neutral"s over false positives.

For a "contradicted" verdict, set "reason" to a short (<20 words) note on how it was contradicted.

Return ONLY a JSON object {"verdicts":[{"id","verdict","reason"}]} with one entry per fact id you were given. Use "neutral" for every fact you are unsure about. No prose, no code fences.`

// JudgeOutcomes asks the model to rule on each surfaced fact. A malformed/empty response
// yields no verdicts (every fact stays neutral) rather than an error — judging is
// best-effort and must not abort the session-end pass.
func (j *ClaudeOutcomeJudge) JudgeOutcomes(ctx context.Context, transcript string, facts []JudgedFact) ([]FactVerdict, error) {
	transcript = strings.TrimSpace(transcript)
	if transcript == "" || len(facts) == 0 {
		return nil, nil
	}

	type factDTO struct {
		ID       string `json:"id"`
		Text     string `json:"text"`
		Category string `json:"category"`
	}
	dtos := make([]factDTO, len(facts))
	for i, f := range facts {
		dtos[i] = factDTO{ID: f.ID, Text: f.Text, Category: f.Category}
	}
	factsJSON, err := json.Marshal(map[string][]factDTO{"facts": dtos})
	if err != nil {
		return nil, fmt.Errorf("brain: marshal facts for outcome judge: %w", err)
	}

	prompt := outcomeSystemPrompt +
		"\n\nFACTS:\n" + string(factsJSON) +
		"\n\nTRANSCRIPT:\n" + transcript +
		"\n\nReturn ONLY the JSON object."

	res, err := msggen.RunWithRetry(ctx, j.runner, prompt, j.opts()...)
	if err != nil {
		return nil, err
	}

	var wrap struct {
		Verdicts []struct {
			ID      string `json:"id"`
			Verdict string `json:"verdict"`
			Reason  string `json:"reason"`
		} `json:"verdicts"`
	}
	decodeWrapped(res, "verdicts", &wrap)
	out := make([]FactVerdict, 0, len(wrap.Verdicts))
	for _, v := range wrap.Verdicts {
		id := strings.TrimSpace(v.ID)
		if id == "" {
			continue
		}
		out = append(out, FactVerdict{
			ID:      id,
			Verdict: normalizeVerdict(v.Verdict),
			Reason:  strings.TrimSpace(v.Reason),
		})
	}
	return out, nil
}

// normalizeVerdict maps a raw verdict string to the closed enum, defaulting anything
// unrecognized to neutral (no change) — the safe direction for a noisy model output.
func normalizeVerdict(s string) OutcomeVerdict {
	switch OutcomeVerdict(strings.ToLower(strings.TrimSpace(s))) {
	case OutcomeHelped:
		return OutcomeHelped
	case OutcomeContradicted:
		return OutcomeContradicted
	default:
		return OutcomeNeutral
	}
}

// opts builds the per-call CLI options for the judge — structured output (so the schema is
// validated at generation), no builtin tools, single logical task with turn headroom for
// the structured-output second turn (see ClaudeExtractor.opts for the same reasoning).
func (j *ClaudeOutcomeJudge) opts() []claudecli.Option {
	return []claudecli.Option{
		claudecli.WithModel(j.model),
		claudecli.WithMaxTurns(4),
		claudecli.WithBuiltinTools(""),
		claudecli.WithSkipVersionCheck(),
		claudecli.WithStrictMCPConfig(),
		claudecli.WithDisableSlashCommands(),
		claudecli.WithSettingSources(""),
		claudecli.WithJSONSchema(outcomeSchema),
	}
}
