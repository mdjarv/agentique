package assistant

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mdjarv/agentique/backend/internal/memory"
)

// The assistant's long-term memory.
//
// The brain was built for the wrong consumer: a coding agent has the repository,
// CLAUDE.md and git history in front of it, and facts injected beside those
// competed with them, arrived without provenance and were judged by an outcome
// signal a session never gives cleanly. The facts it holds are about the
// OPERATOR'S world, which is what the assistant needs on every turn — so as of
// M2 the assistant is the brain's only reader.
//
// The policy that changes is push versus pull. **Knowledge is pulled; only news
// is pushed.** The noise was push: a retriever guessing relevance from keyword or
// cosine overlap and injecting its guess into every turn. So the head's preamble
// carries the pinned set and an INDEX — labels and counts, never bodies — and a
// fact reaches a turn only because the head called [VerbRecall], which it does
// because it knows what it is trying to do. The one push is the journal's news,
// and that is news rather than knowledge.
//
// This package holds the interface and the server implements it over
// brain.Service: the scope vocabulary (`project:<id>`, `global`) is brain policy
// and the assistant deals in project ids, which is the one place these two
// signatures differ from the ones docs/assistant.md names.

// Fact is one durable memory as a head sees it.
//
// Its ID is load-bearing rather than decoration: it is what [VerbConfirmMemory]
// and [VerbFlagMemory] take, so a fact the head recalled can be confirmed when
// the operator agrees with it and flagged when they contradict it. That is the
// conversational outcome signal the brain could never get from a session.
type Fact struct {
	ID   string `json:"id,omitempty"`
	Text string `json:"text,omitempty"`
	// Category is the closed memory.Category set — fact, identity, preference,
	// contact, project, goal, task.
	Category memory.Category `json:"category,omitempty"`
	// Source is where the fact came from. It is shown to the head because a fact
	// the operator stated and a fact an agent reported are not the same claim:
	// memory.SourceReported is agent-written text about untrusted repository
	// content, and the head quotes it rather than asserting it.
	Source memory.Source `json:"source,omitempty"`
	// Scope is the namespace it lives in, already rendered for a reader — a
	// project's name where the implementation could resolve one, else the raw
	// scope. The head never passes a scope back; it names a project in words.
	Scope string `json:"scope,omitempty"`
	// Pinned marks what the operator said to always keep in mind. Pinned facts
	// ride the preamble; nothing else does.
	Pinned bool `json:"pinned,omitempty"`
	// Confidence is the coarse trust tier (extracted, inferred, ambiguous),
	// never the 0..1 score behind it: the tier is what a reader can act on,
	// where a score is a ranking signal that reads as false precision in a
	// sentence.
	Confidence memory.ConfidenceTier `json:"confidence,omitempty"`
}

// IndexKind says what one index line counts.
type IndexKind string

const (
	// IndexArea is a cross-scope topic area: the unit of transferable knowledge,
	// and the line that answers "what does it know about" rather than "where".
	IndexArea IndexKind = "area"
	// IndexScope is one namespace — the global one, or a project.
	IndexScope IndexKind = "scope"
)

// IndexLine is one line of the memory index: a label, how much is behind it, and
// nothing else.
//
// This is the whole of what the preamble carries about memory it has not pulled,
// on the precedent of a memory directory whose index is loaded and whose bodies
// are read on demand. A line is a reason to call [VerbRecall]; it is never a
// substitute for having done so.
type IndexLine struct {
	Kind IndexKind `json:"kind,omitempty"`
	// Label names the area or the scope, in words a reader can use.
	Label string `json:"label,omitempty"`
	// Size is how many durable facts it holds. Captures are not counted: they
	// are never recalled, so counting them would promise facts `recall` cannot
	// return.
	Size int `json:"size,omitempty"`
	// Scopes are the namespaces an area spans, already rendered for a reader.
	// Empty for an [IndexScope] line, which is one scope by construction.
	Scopes []string `json:"scopes,omitempty"`
}

// Provenance is who a remembered fact came from, in the head's own vocabulary.
//
// Two values and no third. It maps onto memory.Source ([Provenance.Source]), and
// the mapping lives here rather than in the implementation so there is one place
// that decides what "the operator said it" means in the store.
type Provenance string

const (
	// ProvenanceOperator: the operator stated or confirmed it. Ground truth,
	// exempt from consolidation rewrite and decay.
	ProvenanceOperator Provenance = "operator"
	// ProvenanceAssistant: the head concluded it from facts the server gave it.
	// Trusted enough to keep, never ground truth.
	ProvenanceAssistant Provenance = "assistant"
)

// provenances is the closed set, for the verb's enum and its refusal.
var provenances = []Provenance{ProvenanceOperator, ProvenanceAssistant}

// Source is the store's name for this provenance.
func (p Provenance) Source() memory.Source {
	if p == ProvenanceOperator {
		return memory.SourceHuman
	}
	return memory.SourceAgent
}

// parseProvenance resolves what a head said, and refuses rather than guessing.
//
// There is no sensible default. Defaulting to the operator would launder the
// head's own conclusion as something the operator said, which is the one claim
// in this store that outranks corroboration; defaulting to the assistant would
// file the operator's own words as a guess. Both are silent, so neither is worth
// the round trip it saves.
func parseProvenance(raw string) (Provenance, bool) {
	p := Provenance(strings.ToLower(strings.TrimSpace(raw)))
	for _, known := range provenances {
		if p == known {
			return known, true
		}
	}
	return "", false
}

// categories is the closed memory.Category set, spelled once so the verb's enum
// and its refusal cannot disagree with the store.
var categories = []memory.Category{
	memory.CategoryFact,
	memory.CategoryIdentity,
	memory.CategoryPreference,
	memory.CategoryContact,
	memory.CategoryProject,
	memory.CategoryGoal,
	memory.CategoryTask,
}

// parseCategory resolves a category, refusing an invented one.
//
// Not defaulted to `fact`, because category is not cosmetic: an `identity` fact
// is pinned on the way in, so it rides every turn's preamble from then on. A
// head that meant identity and silently got a fact would believe it had made a
// standing note nothing will ever show it again.
func parseCategory(raw string) (memory.Category, bool) {
	c := memory.Category(strings.ToLower(strings.TrimSpace(raw)))
	for _, known := range categories {
		if c == known {
			return known, true
		}
	}
	return "", false
}

// categoryNames renders the closed set for a refusal or a schema.
func categoryNames() []string {
	out := make([]string, 0, len(categories))
	for _, c := range categories {
		out = append(out, string(c))
	}
	return out
}

// provenanceNames renders the closed set for a refusal or a schema.
func provenanceNames() []string {
	out := make([]string, 0, len(provenances))
	for _, p := range provenances {
		out = append(out, string(p))
	}
	return out
}

// Memory is the assistant's long-term memory, narrowed to what a head needs.
//
// Implemented in internal/server over brain.Service, on the same rule every
// other collaborator here follows: this package names what it wants in its own
// vocabulary and the server answers. NIL IS VALID and means the head has no
// memory at all — the preamble's "What you remember" section is absent and the
// four memory verbs are not in the table, because a verb that cannot work must
// not be offered.
//
// Every method may be slow (the store is on disk and the index is a clustering
// pass), so every caller here is either a tool handler with a bounded budget or
// the preamble, which is composed once per head.
type Memory interface {
	// Index is one line per area and one per scope: labels and counts, never
	// bodies. What the preamble carries.
	Index(ctx context.Context) ([]IndexLine, error)

	// Pinned is what the operator said to always keep in mind, across every
	// scope. The other half of what the preamble carries.
	Pinned(ctx context.Context) ([]Fact, error)

	// Search is the pull behind [VerbRecall]: query-relevant facts across every
	// scope, with k clamped by the implementation. Pinned facts are NOT
	// repeated here — they are already in front of the head.
	Search(ctx context.Context, query string, k int) ([]Fact, error)

	// Remember writes one durable fact into a project's scope, or the global one
	// when projectID is empty.
	//
	// projectID rather than a scope: the assistant holds project ids and
	// `project:<id>` is brain policy, so the implementation spells it. That is
	// the one deviation from the signature docs/assistant.md names.
	Remember(ctx context.Context, text string, category memory.Category, provenance Provenance, projectID string) (Fact, error)

	// Confirm marks a fact as operator-confirmed ground truth: the accept half
	// of the conversational outcome signal.
	Confirm(ctx context.Context, id string) error

	// Flag records that a fact was contradicted, with the reason, and weakens it
	// into the review queue rather than deleting it — a person decides.
	Flag(ctx context.Context, id, reason string) error

	// Capture stages raw episodic material for consolidation to judge. It is
	// NEVER recalled and never injected; promotion is consolidation's call.
	//
	// source is capture tier — memory.SourceCapture for the assistant's own
	// words, memory.SourceReported when the text was agent-written. projectID
	// places it, as in Remember.
	Capture(ctx context.Context, projectID, text string, source memory.Source) error
}

// WithMemory gives the assistant its long-term memory.
//
// Pass only a live implementation. A typed-nil pointer in an interface reads as
// present and would put four verbs in the table that panic on first use, which
// is the same trap the wiring already avoids for voice's Conversation.
func WithMemory(m Memory) Option { return func(s *Service) { s.mem = m } }

// HasMemory reports whether a memory is wired, which is what decides both
// whether the memory verbs are in the table and whether the preamble has a
// "What you remember" section.
func (s *Service) HasMemory() bool { return s.mem != nil }

// memoryBriefingBudget bounds the whole briefing, both halves together.
//
// It is the same rule voice draws for its own gather (`briefingBudget` in
// docs/voice.md) and for the same reason: this runs from [Service.ensureHead],
// which is BEFORE the turn's own deadline exists and while the conversation's one
// turn lock is held and the thread's composer is shut. The index is a clustering
// pass over the durable corpus and, with an embedder configured, one HTTP round
// trip per batch of uncached facts — each bounded only by its own client's
// per-request timeout, which sums to minutes on a cold process. A slow index has
// to cost the head its index, never the start.
//
// A var rather than a const so a test can shorten it — the same reason
// `refineTimeout` in internal/brain is one.
var memoryBriefingBudget = 10 * time.Second

// memoryBriefing reads the two halves of what the preamble carries, and says when
// it could not read them.
//
// Both halves degrade to nothing rather than failing: a head with an unreadable
// index is a head that has to call recall for everything, which is worse than
// the alternative and is not worse than no assistant at all. The section is
// still rendered, because the verbs are still in the table.
//
// `unread` is the difference between "nothing is in there" and "I could not look",
// and it exists because the preamble has to say one of those rather than the other.
// Both halves come from the same store read, so one failure is the whole memory
// going dark — and a head told an empty store is empty will tell the operator it
// remembers nothing about them and then remember it all a second time.
func (s *Service) memoryBriefing(ctx context.Context) (pinned []Fact, index []IndexLine, unread bool) {
	if s.mem == nil {
		return nil, nil, false
	}

	ctx, cancel := context.WithTimeout(ctx, memoryBriefingBudget)
	defer cancel()

	var err error
	if pinned, err = s.mem.Pinned(ctx); err != nil {
		s.log.Warn("assistant: pinned memory unavailable", "error", err)
		pinned, unread = nil, true
	}
	if index, err = s.mem.Index(ctx); err != nil {
		s.log.Warn("assistant: memory index unavailable", "error", err)
		index, unread = nil, true
	}
	return pinned, index, unread
}

// captureNotable stages a notable journal entry in memory.
//
// Captures come from the conversation and from notable journal entries, never
// from transcripts: there is no background extraction here, and what the
// operator says is remembered through the visible [VerbRemember] call or not at
// all. A notable entry is the other door, because notable is precisely the mark
// that says "consolidation should look at this".
//
// The source is the entry's own trust: memory.SourceReported when the entry is
// untrusted — its text was written by an agent about repository content nobody
// here authored — and memory.SourceCapture otherwise. Neither is ever recalled,
// so the distinction costs nothing now and is what lets consolidation weigh them
// differently later.
//
// Failure is LOGGED and never propagated. The journal is the record of what
// happened and memory is an index over it; losing the index entry must not lose
// the fact, and the caller is a tool handler with a model waiting.
func (s *Service) captureNotable(ctx context.Context, entry JournalEntry) {
	if s.mem == nil {
		return
	}
	text := strings.TrimSpace(entry.Summary)
	if text == "" {
		return
	}
	source := memory.SourceCapture
	if entry.Untrusted {
		source = memory.SourceReported
	}
	if err := s.mem.Capture(ctx, entry.ProjectID, text, source); err != nil {
		s.log.Warn("assistant: notable entry not captured",
			"entry", entry.ID, "kind", entry.Kind, "error", err)
	}
}

// factPayloads renders facts for a head.
func factPayloads(facts []Fact) []map[string]any {
	out := make([]map[string]any, 0, len(facts))
	for _, fact := range facts {
		payload := map[string]any{"id": fact.ID, "text": fact.Text}
		if fact.Category != "" {
			payload["category"] = string(fact.Category)
		}
		if fact.Source != "" {
			payload["source"] = string(fact.Source)
		}
		if fact.Scope != "" {
			payload["scope"] = fact.Scope
		}
		if fact.Pinned {
			payload["pinned"] = true
		}
		if fact.Confidence != "" {
			payload["confidence"] = string(fact.Confidence)
		}
		out = append(out, payload)
	}
	return out
}

// factLine is one fact as the preamble prints it: the text, what kind of thing
// it is, and the id a confirm or a flag needs.
func factLine(fact Fact) string {
	text := strings.TrimSpace(strings.ReplaceAll(fact.Text, "\n", " "))
	if text == "" {
		return ""
	}
	category := string(fact.Category)
	if category == "" {
		category = string(memory.CategoryFact)
	}
	return fmt.Sprintf("- %s _(%s, id `%s`)_", text, category, fact.ID)
}

// indexLineText is one index line as the preamble prints it.
func indexLineText(line IndexLine) string {
	label := strings.TrimSpace(line.Label)
	if label == "" {
		return ""
	}
	facts := "facts"
	if line.Size == 1 {
		facts = "fact"
	}
	switch line.Kind {
	case IndexArea:
		if len(line.Scopes) > 0 {
			return fmt.Sprintf("- area %q — %d %s, spanning %s",
				label, line.Size, facts, SpokenList(line.Scopes))
		}
		return fmt.Sprintf("- area %q — %d %s", label, line.Size, facts)
	default:
		return fmt.Sprintf("- %s — %d %s", label, line.Size, facts)
	}
}
