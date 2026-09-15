package assistant

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// A turn's working: what the head did between the ask and the reply.
//
// The thread used to show "Thinking…" for as long as a turn took and then only
// the reply, so a turn that recalled the wrong fact, asked for a session that
// does not exist, or ran out its ten-minute budget left nothing to read. The
// steps are that record.
//
// They are recorded where the work HAPPENS, not inferred from the CLI's event
// stream. Every verb the head calls passes one door ([Service.ToolHandler]),
// which knows the verb, its arguments, what it answered and whether it was a
// refusal — facts the stream carries only as an opaque MCP tool call. The one
// thing that door cannot see is the model's reasoning, which arrives from the
// runtime ([HeadParams.OnThought]); on Claude it arrives encrypted, so a thought
// is usually a mark that reasoning happened rather than text to read.
//
// A step is a record for the operator and nothing else. It never reaches the
// head's preamble (the tail carries text only), it is never an instruction,
// and what it quotes — an argument, a refusal, a fact — is shown as inert text.

// Step kinds. A closed set on this side; `string` on the wire, so a peer one
// release ahead can send a kind this client renders as a plain row.
const (
	// StepVerb is one verb call, from the moment the head asked to its answer.
	StepVerb = "verb"
	// StepThought is one block of the model's reasoning.
	StepThought = "thought"
)

// Step statuses.
const (
	// StepRunning is a verb that has been called and has not answered.
	StepRunning = "running"
	// StepDone is a verb that answered, or a thought that arrived.
	StepDone = "done"
	// StepRefused is a verb that answered with a refusal: the table's own
	// rules, a missing argument, nothing to act on.
	StepRefused = "refused"
	// StepFailed is a verb whose handler errored. Separate from refused,
	// because a refusal is the system working and a failure is it not.
	StepFailed = "failed"
)

// Bounds on what a turn keeps. Steps are stored on the message they belong to,
// and a history page is fifty messages, so each field is clipped where it is
// recorded rather than trusted to be short.
const (
	// maxTurnSteps is how many steps one turn keeps. The rest are counted, so
	// the thread can say that more happened than it shows.
	maxTurnSteps = 24
	// maxStepDetailRunes clips the one-line argument a verb row shows.
	maxStepDetailRunes = 100
	// maxStepOutcomeRunes clips what a verb answered, a refusal's sentence
	// included.
	maxStepOutcomeRunes = 160
	// maxStepFacts is how many recalled facts one recall row carries.
	maxStepFacts = 6
	// maxStepFactRunes clips one recalled fact's text.
	maxStepFactRunes = 160
	// maxThoughtRunes clips a thought, for the day a provider sends one in the
	// clear.
	maxThoughtRunes = 1000
)

// Step is one thing the head did during a turn.
type Step struct {
	// Seq orders the steps within one turn, from 1. A live push and the stored
	// message name the same step by it.
	Seq int `json:"seq,omitempty"`
	// Kind is [StepVerb] or [StepThought].
	Kind string `json:"kind,omitempty"`
	// Verb is the verb's name, for a verb step.
	Verb string `json:"verb,omitempty"`
	// Detail is the verb's most telling argument on one line: what it looked
	// for, which project, which filter. Quoted model output, so inert text.
	Detail string `json:"detail,omitempty"`
	// SessionID is the session the verb was about, when it named one. A client
	// turns it into the session's name from the list it already holds.
	SessionID string `json:"sessionId,omitempty"`
	// Status is [StepRunning], [StepDone], [StepRefused] or [StepFailed].
	Status string `json:"status,omitempty"`
	// Outcome is what the verb answered, in a few words: "3 facts",
	// "2 sessions", a refusal's sentence.
	Outcome string `json:"outcome,omitempty"`
	// Facts are what a recall returned, so the operator can see which facts a
	// reply was built on and confirm or flag one.
	Facts []StepFact `json:"facts,omitempty"`
	// Text is a thought's text, when the provider sent any.
	Text string `json:"text,omitempty"`
	// Encrypted marks a thought whose text the provider withheld.
	Encrypted bool `json:"encrypted,omitempty"`
	// DurationMs is how long a verb took to answer.
	DurationMs int64 `json:"durationMs,omitempty"`
}

// StepFact is one fact a recall returned.
type StepFact struct {
	ID    string `json:"id,omitempty"`
	Scope string `json:"scope,omitempty"`
	Text  string `json:"text,omitempty"`
	// Source is the fact's provenance. "reported" is agent-written text about
	// repository content nobody here authored, and renders as a quotation.
	Source string `json:"source,omitempty"`
}

// StepPush is the live `assistant.step` push: one step started or settled.
//
// The same step arrives twice for a verb — running, then settled — and a client
// replaces by [Step.Seq]. The stored message carries the final list, so a push
// that is missed costs a live row and never the record.
type StepPush struct {
	// Surface is the surface whose ask the turn is answering.
	Surface string `json:"surface,omitempty"`
	Step    *Step  `json:"step,omitempty"`
}

// turnSteps collects one turn's steps.
//
// Its own lock, because it is written from two goroutines that know nothing of
// each other: the MCP handler running a verb and the runtime's event loop
// delivering a thought.
type turnSteps struct {
	mu      sync.Mutex
	steps   []Step
	omitted int
}

// begin records a new step and answers it with its sequence number, or false
// when the turn has kept all it keeps.
func (t *turnSteps) begin(step Step) (Step, bool) {
	if t == nil {
		return Step{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.steps) >= maxTurnSteps {
		t.omitted++
		return Step{}, false
	}
	step.Seq = len(t.steps) + 1
	t.steps = append(t.steps, step)
	return step, true
}

// settle applies an update to a recorded step and answers the result.
func (t *turnSteps) settle(seq int, update func(*Step)) (Step, bool) {
	if t == nil || seq < 1 {
		return Step{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if seq > len(t.steps) {
		return Step{}, false
	}
	update(&t.steps[seq-1])
	return t.steps[seq-1], true
}

// snapshot answers a copy of what the turn kept and how many it did not.
func (t *turnSteps) snapshot() ([]Step, int) {
	if t == nil {
		return nil, 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.steps) == 0 {
		return nil, t.omitted
	}
	out := make([]Step, len(t.steps))
	copy(out, t.steps)
	for i := range out {
		// A verb still marked running when the turn ended never answered: the
		// turn was cut off underneath it. Saying "running" forever in a stored
		// record would claim it is still going.
		if out[i].Status == StepRunning {
			out[i].Status = StepFailed
			out[i].Outcome = "no answer before the turn ended"
		}
	}
	return out, t.omitted
}

// currentSteps is the in-flight turn's recorder, or nil between turns.
func (s *Service) currentSteps() (*turnSteps, string) {
	s.head.mu.Lock()
	defer s.head.mu.Unlock()
	return s.head.steps, s.head.surface
}

// beginVerbStep records a verb the head just called and pushes it.
func (s *Service) beginVerbStep(name string, args map[string]any) (*turnSteps, Step, bool) {
	rec, surface := s.currentSteps()
	step, ok := rec.begin(Step{
		Kind:      StepVerb,
		Verb:      name,
		Detail:    stepDetail(args),
		SessionID: strings.TrimSpace(stringArg(args, "session_id")),
		Status:    StepRunning,
	})
	if ok {
		s.pushStep(surface, step)
	}
	return rec, step, ok
}

// settleVerbStep records what a verb answered and pushes it.
func (s *Service) settleVerbStep(rec *turnSteps, seq int, payload map[string]any, failed, refused bool, took time.Duration) {
	step, ok := rec.settle(seq, func(step *Step) {
		step.Status = StepDone
		switch {
		case failed:
			step.Status = StepFailed
		case refused:
			step.Status = StepRefused
		}
		step.Outcome = stepOutcome(payload, failed || refused)
		step.Facts = stepFacts(payload)
		step.DurationMs = took.Milliseconds()
	})
	if !ok {
		return
	}
	_, surface := s.currentSteps()
	s.pushStep(surface, step)
}

// onHeadThought is the runtime's hook for one block of reasoning. text is ""
// when the provider withheld it.
func (s *Service) onHeadThought(text string) {
	rec, surface := s.currentSteps()
	text = strings.TrimSpace(text)
	step, ok := rec.begin(Step{
		Kind:      StepThought,
		Status:    StepDone,
		Text:      clampRunes(text, maxThoughtRunes),
		Encrypted: text == "",
	})
	if ok {
		s.pushStep(surface, step)
	}
}

func (s *Service) pushStep(surface string, step Step) {
	s.broadcast(EventStep, StepPush{Surface: surface, Step: &step})
}

// stepDetailKeys is the order a verb's arguments are read in for its one-line
// detail: whatever says most about what was asked comes first. session_id is
// not here — it is carried as [Step.SessionID], because a uuid on a line is not
// something anybody reads.
var stepDetailKeys = []string{
	"target", "query", "project", "filter", "name", "category", "id", "text", "prompt", "reason",
}

// stepDetail picks a verb's most telling argument.
func stepDetail(args map[string]any) string {
	for _, key := range stepDetailKeys {
		if value := oneLine(stringArg(args, key)); value != "" {
			return clampRunes(value, maxStepDetailRunes)
		}
	}
	return ""
}

// stepOutcome says in a few words what a verb answered.
//
// A refusal's own sentence is the outcome, because it was written to be read.
// Otherwise the first list in the answer is counted, in the answer's own word
// for what it holds — which is what every read verb answers with — and anything
// else is "done".
func stepOutcome(payload map[string]any, refusal bool) string {
	if refusal {
		if said := oneLine(stringArg(payload, "error")); said != "" {
			return clampRunes(said, maxStepOutcomeRunes)
		}
		return "refused"
	}

	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		n, isList := listLen(payload[key])
		if !isList {
			continue
		}
		if n == 0 {
			return "nothing found"
		}
		return countOf(n, key)
	}
	return "done"
}

// stepFacts carries a recall's facts onto its step.
func stepFacts(payload map[string]any) []StepFact {
	var raw []map[string]any
	switch facts := payload["facts"].(type) {
	case []map[string]any:
		raw = facts
	case []any:
		for _, item := range facts {
			if fact, ok := item.(map[string]any); ok {
				raw = append(raw, fact)
			}
		}
	}
	if len(raw) == 0 {
		return nil
	}

	out := make([]StepFact, 0, min(len(raw), maxStepFacts))
	for _, fact := range raw {
		if len(out) == maxStepFacts {
			break
		}
		id := stringArg(fact, "id")
		if id == "" {
			continue
		}
		out = append(out, StepFact{
			ID:     id,
			Scope:  stringArg(fact, "scope"),
			Text:   clampRunes(oneLine(stringArg(fact, "text")), maxStepFactRunes),
			Source: stringArg(fact, "source"),
		})
	}
	return out
}

func listLen(value any) (int, bool) {
	switch list := value.(type) {
	case []any:
		return len(list), true
	case []map[string]any:
		return len(list), true
	case []string:
		return len(list), true
	}
	return 0, false
}

// countOf is "1 fact" or "3 facts", from the answer's own plural key.
func countOf(n int, key string) string {
	word := strings.ReplaceAll(key, "_", " ")
	if n == 1 {
		word = strings.TrimSuffix(word, "s")
	}
	return fmt.Sprintf("%d %s", n, word)
}

// oneLine collapses whitespace, newlines included, to single spaces.
func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// withTurnSteps runs fn with a recorder installed for the turn, and answers
// what it kept.
//
// Installed under the head's lock and removed however fn returns, so a verb
// call arriving after the turn — a late answer from a head that was stopped —
// finds no recorder and is not written into the next turn's record.
//
// The surface is stamped here as well as in [Service.ensureHead], because a
// step can be pushed before the head is up — and a push naming the previous
// turn's surface is a live row on the wrong page.
func (s *Service) withTurnSteps(surface string, fn func()) ([]Step, int) {
	rec := &turnSteps{}
	s.head.mu.Lock()
	s.head.steps = rec
	s.head.surface = surface
	s.head.mu.Unlock()

	defer func() {
		s.head.mu.Lock()
		if s.head.steps == rec {
			s.head.steps = nil
		}
		s.head.mu.Unlock()
	}()

	fn()
	return rec.snapshot()
}
