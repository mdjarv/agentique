package server

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/mdjarv/agentique/backend/internal/assistant"
	"github.com/mdjarv/agentique/backend/internal/brain"
	"github.com/mdjarv/agentique/backend/internal/memory"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// assistantMemory is assistant.Memory over the brain.
//
// The seam is here rather than in internal/assistant for the same reason the
// directory and the dispatcher are: the assistant asks its questions in its own
// vocabulary — a project id, a provenance, a fact — and this file answers them in
// the brain's, which is scopes, sources and records. `project:<id>` is agentique
// policy (brain.ScopeForProject), and the core is deliberately ignorant of it.
//
// It is also where a scope becomes readable. A head shown `project:8f2c…` cannot
// do anything with it — it never passes a scope back, it names a project in words
// — so every scope that reaches the assistant is already a label.
type assistantMemory struct {
	brain   *brain.Service
	queries *store.Queries
}

// maxAssistantSearchK is the ceiling on one pull, clamped SERVER-SIDE.
//
// The verb asks for its own number and this is the number it cannot exceed:
// clamping at the caller would put the bound on the side of the seam that a
// future caller gets to choose.
const maxAssistantSearchK = 20

// maxAssistantIndexLines bounds the index the preamble carries.
//
// The index is the one memory read that rides EVERY fresh head, so it is the one
// that has to stay a glance. Areas come first and are sorted largest-first, so a
// brain with two hundred areas loses its smallest rather than its biggest.
//
// The two halves are budgeted SEPARATELY (see [assistantMemory.Index]): areas are
// unbounded, where the scope lines are bounded by the number of projects, so one
// truncation over the concatenation would spend the whole budget on areas and leave
// the head with no line naming any namespace at all.
const maxAssistantIndexLines = 40

// maxAssistantPinnedFacts bounds the pinned set the preamble carries.
//
// Pinned facts ride every fresh head WITH THEIR BODIES, and the head can grow that
// set itself: `remember(category: "identity")` pins on the way in. So the read is
// capped like the index is, most recently touched first.
//
// A truncation is not a lie to the head: the preamble already says that everything
// not printed is behind `recall`, which is exactly where the overflow is. The
// operator sees the whole pinned set on the memory page, and an overflow is logged.
const maxAssistantPinnedFacts = 40

func newAssistantMemory(b *brain.Service, queries *store.Queries) *assistantMemory {
	return &assistantMemory{brain: b, queries: queries}
}

// Index is one line per cross-scope area and one per scope.
//
// Two reads and one clustering pass, and no fact text at all: this is what the
// head's preamble carries in place of the facts themselves, which is the whole of
// what changed between the brain's first consumer and this one.
//
// An area listing that fails is not fatal to the index — the scope lines are the
// half that always exists, because every fact has a scope and only some belong to
// an area. Which is also why the budget reserves them: see the truncation below.
func (m *assistantMemory) Index(ctx context.Context) ([]assistant.IndexLine, error) {
	records, err := m.brain.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("assistant memory index: %w", err)
	}
	labels := m.scopeLabels(ctx)

	var areaLines []assistant.IndexLine

	areas, err := m.brain.PreviewAreas(ctx)
	if err != nil {
		// Areas are a rebuildable index over the facts, so losing them costs the
		// head the topic view and nothing else. The scopes below still answer
		// "what is there to ask about".
		slog.Warn("assistant memory: areas unavailable", "error", err)
	}
	for _, area := range areas {
		scopes := make([]string, 0, len(area.Scopes))
		for _, scope := range area.Scopes {
			scopes = append(scopes, labels.of(scope))
		}
		sort.Strings(scopes)
		areaLines = append(areaLines, assistant.IndexLine{
			Kind:   assistant.IndexArea,
			Label:  area.Label,
			Size:   area.Size,
			Scopes: scopes,
		})
	}

	// One line per scope, counting DURABLE facts only. A capture is never
	// recalled, so counting it would promise the head facts that `recall` cannot
	// return — and an index whose numbers do not survive being asked about is
	// worse than no index.
	counts := make(map[memory.Scope]int)
	for _, record := range records {
		if record.Source.Staged() || memory.IsArchived(record) {
			continue
		}
		counts[record.Scope]++
	}
	scoped := make([]assistant.IndexLine, 0, len(counts))
	for scope, count := range counts {
		scoped = append(scoped, assistant.IndexLine{
			Kind:  assistant.IndexScope,
			Label: labels.of(scope),
			Size:  count,
		})
	}
	// Biggest first, then by label: deterministic, and the same order the areas
	// arrive in, so the whole index reads one way.
	sort.Slice(scoped, func(i, j int) bool {
		if scoped[i].Size != scoped[j].Size {
			return scoped[i].Size > scoped[j].Size
		}
		return scoped[i].Label < scoped[j].Label
	})
	return budgetIndexLines(areaLines, scoped), nil
}

// budgetIndexLines fits the index into [maxAssistantIndexLines] by RESERVING the scope
// lines and spending what is left on areas.
//
// Truncating the concatenation instead spent the whole budget on areas: at forty of
// them the head was shown topic labels with no line naming any namespace, which is the
// half that always exists (every fact has a scope; only some belong to an area) and the
// half the area listing falls back to when it fails. Areas are unbounded where the
// scopes are bounded by the number of projects, so the unbounded half is the one that
// pays. Both arrive biggest-first, so each loses its smallest.
func budgetIndexLines(areas, scoped []assistant.IndexLine) []assistant.IndexLine {
	if len(scoped) > maxAssistantIndexLines {
		scoped = scoped[:maxAssistantIndexLines]
	}
	if room := maxAssistantIndexLines - len(scoped); len(areas) > room {
		areas = areas[:room]
	}
	return append(areas, scoped...)
}

// Pinned is every pinned fact, across every scope, up to
// [maxAssistantPinnedFacts].
//
// Archived facts are excluded, matching what recall does: the cold tier is never
// surfaced, and a pinned fact that has been archived would otherwise ride every
// preamble while `recall` denied it existed.
//
// The cap is on the same argument as the index's: this rides every fresh head, with
// bodies, and the head can add to it itself by remembering an `identity` fact. What
// it keeps is the most recently touched, so a set that has outgrown the preamble
// loses what nobody has edited or recalled in longest.
func (m *assistantMemory) Pinned(ctx context.Context) ([]assistant.Fact, error) {
	records, err := m.brain.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("assistant pinned memory: %w", err)
	}
	labels := m.scopeLabels(ctx)

	pinned := make([]memory.Record, 0, 8)
	for _, record := range records {
		if !record.Pinned || record.Source.Staged() || memory.IsArchived(record) {
			continue
		}
		pinned = append(pinned, record)
	}
	if len(pinned) > maxAssistantPinnedFacts {
		sort.Slice(pinned, func(i, j int) bool {
			return touchedAt(pinned[i]).After(touchedAt(pinned[j]))
		})
		slog.Warn("assistant memory: pinned set truncated for the head's preamble",
			"pinned", len(pinned), "kept", maxAssistantPinnedFacts)
		pinned = pinned[:maxAssistantPinnedFacts]
	}

	facts := make([]assistant.Fact, 0, len(pinned))
	for _, record := range pinned {
		facts = append(facts, factFrom(record, labels))
	}
	// Deterministic, so a head restarted twice reads the same preamble: by scope,
	// then by the text itself.
	sort.Slice(facts, func(i, j int) bool {
		if facts[i].Scope != facts[j].Scope {
			return facts[i].Scope < facts[j].Scope
		}
		return facts[i].Text < facts[j].Text
	})
	return facts, nil
}

// Search is the pull behind the recall verb.
//
// Over every scope, because the assistant's own conversation belongs to no
// project and the operator's question rarely names one. Pinned facts are dropped
// from the answer: they are already in the head's instruction, and returning them
// again would spend a pull on what it was handed for free.
//
// A pull IS a successful recall, so the ids it returns are stamped through
// brain.MarkUsed — that is the retrieval-practice signal the design counts on
// ("a fact the head pulled and then used is a real signal, where an injected fact
// that was maybe read was not"). Best effort: a stamp that fails costs a counter,
// never the answer.
func (m *assistantMemory) Search(ctx context.Context, query string, k int) ([]assistant.Fact, error) {
	if k <= 0 || k > maxAssistantSearchK {
		k = maxAssistantSearchK
	}
	// An empty list means "every scope" to memory.Recall, which is the same set
	// this asked for — so a store with nothing in it needs no special case.
	scopes, err := m.brain.ListScopes(ctx)
	if err != nil {
		return nil, fmt.Errorf("assistant memory scopes: %w", err)
	}
	// RecallForPull rather than Recall: this is the one surface here that feeds what
	// it returns to a model, so it is the one that applies the read-time disuse fade
	// an operator opts into with `archive-after`. The page's search box keeps showing
	// what is there.
	result, err := m.brain.RecallForPull(ctx, scopes, query, k)
	if err != nil {
		return nil, fmt.Errorf("assistant memory search: %w", err)
	}
	labels := m.scopeLabels(ctx)

	facts := make([]assistant.Fact, 0, len(result.Recalled))
	ids := make([]string, 0, len(result.Recalled))
	for _, record := range result.Recalled {
		facts = append(facts, factFrom(record, labels))
		ids = append(ids, record.ID)
	}
	if len(ids) > 0 {
		if err := m.brain.MarkUsed(ctx, ids...); err != nil {
			slog.Warn("assistant memory: use counters not stamped", "error", err)
		}
	}
	return facts, nil
}

// Remember writes one durable fact.
//
// The provenance is mapped by the assistant's own enum, so what "the operator
// said it" means in the store is decided once, where the enum is declared.
func (m *assistantMemory) Remember(ctx context.Context, text string, category memory.Category,
	provenance assistant.Provenance, projectID string,
) (assistant.Fact, error) {
	record, err := m.brain.Add(ctx, brain.ScopeForProject(projectID), text, category, provenance.Source())
	if err != nil {
		return assistant.Fact{}, fmt.Errorf("assistant remember: %w", err)
	}
	return factFrom(record, m.scopeLabels(ctx)), nil
}

// Confirm is the accept half of the conversational outcome signal.
func (m *assistantMemory) Confirm(ctx context.Context, id string) error {
	if _, err := m.brain.Confirm(ctx, id); err != nil {
		return fmt.Errorf("assistant confirm memory %s: %w", id, err)
	}
	return nil
}

// Flag is the contradict half. It weakens the fact into the review queue and
// never deletes it: a person confirms, edits or drops.
func (m *assistantMemory) Flag(ctx context.Context, id, reason string) error {
	if _, err := m.brain.Flag(ctx, id, reason); err != nil {
		return fmt.Errorf("assistant flag memory %s: %w", id, err)
	}
	return nil
}

// Capture stages raw episodic material for consolidation to judge.
//
// It goes through brain.CaptureFrom rather than brain.Capture so the provenance
// survives the write: memory.SourceReported is a sentence an agent wrote about
// repository content nobody here authored, and losing that at the door would make
// it indistinguishable from something the operator said. Neither is recallable —
// that is what capture tier means — so the distinction is for consolidation.
//
// The category is left to the store's default: a journal entry is a sentence, not
// a classification, and guessing one here would be this layer inventing a fact
// about a fact.
func (m *assistantMemory) Capture(ctx context.Context, projectID, text string, source memory.Source) error {
	_, err := m.brain.CaptureFrom(ctx, brain.ScopeForProject(projectID), text, "", source)
	if err != nil {
		return fmt.Errorf("assistant capture: %w", err)
	}
	return nil
}

// The brain is the implementation, asserted here so a signature change in either
// package is a compile error in this file rather than a wiring error in server.New.
var _ assistant.Memory = (*assistantMemory)(nil)

// factFrom maps a stored record onto what a head sees.
//
// Confidence is the coarse TIER and never the 0..1 score: the tier is a reading a
// sentence can carry, where a score printed to two decimals is false precision
// about somebody's memory. It is normalized first, because the tier is always
// derived from (source, score) and an old record on disk may not carry one.
func factFrom(record memory.Record, labels scopeLabels) assistant.Fact {
	normalized := memory.NormalizeConfidence(record)
	return assistant.Fact{
		ID:         record.ID,
		Text:       strings.TrimSpace(record.Text),
		Category:   record.Category,
		Source:     record.Source,
		Scope:      labels.of(record.Scope),
		Pinned:     record.Pinned,
		Confidence: normalized.Confidence,
	}
}

// touchedAt is the last time anything happened to a fact — an edit or a recall,
// whichever is later. It is what "most recently touched" means for the pinned cap,
// and neither half alone says it: a fact recalled every day is never edited, and one
// edited this morning has not been pulled yet.
func touchedAt(record memory.Record) time.Time {
	if record.LastUsedAt.After(record.UpdatedAt) {
		return record.LastUsedAt
	}
	return record.UpdatedAt
}

// scopeLabels turns the brain's scope strings into words.
type scopeLabels map[memory.Scope]string

// of names one scope for a reader.
//
// An unrecognised scope answers with itself rather than with "unknown": a project
// deleted since a fact was filed still tells the reader more as `project:8f2c…`
// than as a blank, and it is the one case where the raw string is the honest
// answer.
func (l scopeLabels) of(scope memory.Scope) string {
	if label, ok := l[scope]; ok {
		return label
	}
	if scope == "" {
		return string(memory.ScopeGlobal)
	}
	return string(scope)
}

// scopeLabels builds the map, one project query per call.
//
// Degrades to an empty map rather than failing: every caller then prints raw
// scopes, which is worse to read and not wrong.
func (m *assistantMemory) scopeLabels(ctx context.Context) scopeLabels {
	labels := scopeLabels{memory.ScopeGlobal: "everywhere"}
	if m.queries == nil {
		return labels
	}
	projects, err := m.queries.ListProjects(ctx)
	if err != nil {
		slog.Warn("assistant memory: project names unavailable", "error", err)
		return labels
	}
	for _, project := range projects {
		name := project.Name
		if name == "" {
			name = project.Slug
		}
		if name == "" {
			continue
		}
		labels[brain.ScopeForProject(project.ID)] = "the project " + name
	}
	return labels
}
