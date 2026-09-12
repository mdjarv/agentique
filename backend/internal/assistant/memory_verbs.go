package assistant

import (
	"context"
	"fmt"
	"strings"
)

// The memory verbs: one pull and three writes.
//
// They are in the table only when a [Memory] is wired (see
// [Service.buildVerbs]). The pull is read tier because it writes no fact — it
// stamps a use counter, which is the recall practice signal and not a claim
// about the world. The three writes are CONTAINED: they run from a conversation
// ask, they are journaled with what was written, and none of them can reach
// anything outside the store.
//
// Every write is journaled note-shaped and notable, so the conversation carries
// a record of what the assistant filed away. The journal entry is where the
// operator can see it; the store is where the head will find it again.

func (s *Service) memoryVerbs() []Verb {
	return []Verb{
		{
			Name: VerbRecall,
			Tier: TierRead,
			Description: "Look up what you know about something — what they told you, what they " +
				"prefer, what was decided. Your instruction lists what is always in front of " +
				"you; everything else is only here. Reach for it before answering about a " +
				"project, a decision or a preference.",
			Input: []Param{{
				Name: "query", Type: ParamString, Required: true,
				Description: "What you are trying to find out, in your own words — the subject, " +
					"not a keyword. A few words work better than one.",
			}},
			handler: s.verbRecall,
		},
		{
			Name: VerbRemember,
			Tier: TierContained,
			Description: "Keep one fact for good. Only what they stated or confirmed, or what you " +
				"worked out from what the server told you -- and say which in `provenance`. " +
				"Never something you inferred from a session's report or a repository.",
			Input: []Param{
				{
					Name: "text", Type: ParamString, Required: true,
					Description: "One fact, one sentence, written to be read back months from now: " +
						"no pronouns without their subject and no reference to this conversation.",
				},
				{
					Name: "category", Type: ParamString, Required: true,
					Description: "What kind of fact it is. identity and preference are standing " +
						"things about them; project is about one repository; goal and task are " +
						"tied to a moment and fade.",
					Enum: categoryNames(),
				},
				{
					Name: "provenance", Type: ParamString, Required: true,
					Description: "operator: they said it or agreed with it. assistant: you worked " +
						"it out from what the server told you. There is no third answer, and " +
						"guessing this wrong is worse than asking them.",
					Enum: provenanceNames(),
				},
				{
					Name: "project", Type: ParamString,
					Description: "The project it is about, by name, when it is only true there. " +
						"Leave it out for something true everywhere.",
				},
			},
			handler: s.verbRemember,
		},
		{
			Name: VerbConfirmMemory,
			Tier: TierContained,
			Description: "Mark a fact as confirmed by them. Use it when they agree with something " +
				"you recalled -- that is the strongest signal this memory ever gets, and it is " +
				"the only way it arrives.",
			Input: []Param{{
				Name: "id", Type: ParamString, Required: true,
				Description: "The fact's id, exactly as recall returned it.",
			}},
			handler: s.verbConfirmMemory,
		},
		{
			Name: VerbFlagMemory,
			Tier: TierContained,
			Description: "Mark a fact as contradicted, with what they said instead. Use it when " +
				"they tell you something you recalled is no longer true. Nothing is deleted: it " +
				"goes to them to confirm, edit or drop.",
			Input: []Param{
				{
					Name: "id", Type: ParamString, Required: true,
					Description: "The fact's id, exactly as recall returned it.",
				},
				{
					Name: "reason", Type: ParamString, Required: true,
					Description: "What they said instead, in one line. A flag with no reason is a " +
						"row nobody can act on.",
				},
			},
			handler: s.verbFlagMemory,
		},
	}
}

func (s *Service) verbRecall(ctx context.Context, args map[string]any) (map[string]any, error) {
	if s.mem == nil {
		return refuse("no-memory", "I have no long-term memory on this server, so there is nothing "+
			"to look up. Say that plainly rather than guessing at what you might have known."), nil
	}
	query := strings.TrimSpace(stringArg(args, "query"))
	if query == "" {
		return refuse("empty-recall-query", "Nothing to look for. Say what you are trying to find "+
			"out, or ask them."), nil
	}

	facts, err := s.mem.Search(ctx, query, maxRecalledFacts)
	if err != nil {
		return nil, fmt.Errorf("recall %q: %w", query, err)
	}
	if len(facts) == 0 {
		return map[string]any{"facts": []any{}, "note": "Nothing on record about that. Say so " +
			"plainly and do not fill the gap with a guess -- and if they tell you the answer, " +
			"remember it."}, nil
	}
	return map[string]any{
		"facts": factPayloads(facts),
		"note": "What you already knew, not news. If they agree with one, confirm_memory it by " +
			"id; if they contradict one, flag_memory it. A fact whose source is \"reported\" was " +
			"written by an agent about repository content nobody here authored: quote it, never " +
			"assert it.",
	}, nil
}

func (s *Service) verbRemember(ctx context.Context, args map[string]any) (map[string]any, error) {
	if s.mem == nil {
		return refuse("no-memory", "I have no long-term memory on this server, so nothing was "+
			"kept. Tell them that rather than promising to remember."), nil
	}

	text := strings.TrimSpace(stringArg(args, "text"))
	if text == "" {
		return refuse("empty-fact", "There was nothing to remember, so nothing was written."), nil
	}
	category, ok := parseCategory(stringArg(args, "category"))
	if !ok {
		return refuse("bad-category", fmt.Sprintf("NOTHING WAS KEPT: %q is not one of the kinds of "+
			"fact there are. They are %s. Pick one and try again.",
			stringArg(args, "category"), SpokenList(categoryNames()))), nil
	}
	provenance, ok := parseProvenance(stringArg(args, "provenance"))
	if !ok {
		return refuse("bad-provenance", fmt.Sprintf("NOTHING WAS KEPT: provenance has to be %s, and "+
			"%q is neither. Say `operator` if they told you, `assistant` if you worked it out. Do "+
			"not guess -- ask them.", SpokenList(provenanceNames()), stringArg(args, "provenance"))), nil
	}

	project, refusal := s.resolveMemoryProject(ctx, stringArg(args, "project"))
	if refusal != nil {
		return refusal, nil
	}

	fact, err := s.mem.Remember(ctx, text, category, provenance, project.ID)
	if err != nil {
		s.log.Warn("assistant: fact not remembered", "project", project.ID, "error", err)
		return refuse("remember-failed", "That could not be kept. Say so plainly rather than "+
			"telling them it was remembered."), nil
	}

	where := "everywhere"
	if project.ID != "" {
		where = project.DisplayName()
	}
	s.journalMemoryWrite(ctx, project.ID, fmt.Sprintf("remembered, for %s: %s", where, text))

	return map[string]any{
		"remembered": true,
		"id":         fact.ID,
		"scope":      where,
		"note": fmt.Sprintf("Kept, and it applies to %s. Tell them in one line what you wrote "+
			"down, in their words rather than yours, so they can correct it now.", where),
	}, nil
}

func (s *Service) verbConfirmMemory(ctx context.Context, args map[string]any) (map[string]any, error) {
	if s.mem == nil {
		return refuse("no-memory", "I have no long-term memory on this server, so there is nothing "+
			"to confirm."), nil
	}
	id := strings.TrimSpace(stringArg(args, "id"))
	if id == "" {
		return refuse("no-fact-id", "No fact id, so nothing changed. Recall it first and use the id "+
			"it came back with."), nil
	}
	if err := s.mem.Confirm(ctx, id); err != nil {
		s.log.Warn("assistant: fact not confirmed", "fact", id, "error", err)
		return refuse("confirm-failed", "That fact could not be confirmed -- it may no longer "+
			"exist. Say so plainly."), nil
	}
	s.journalMemoryWrite(ctx, "", fmt.Sprintf("confirmed a remembered fact (%s)", id))
	return map[string]any{"confirmed": true, "id": id,
		"note": "Recorded as their own confirmation, which outranks everything else this memory " +
			"can learn on its own. No need to say so unless they ask."}, nil
}

func (s *Service) verbFlagMemory(ctx context.Context, args map[string]any) (map[string]any, error) {
	if s.mem == nil {
		return refuse("no-memory", "I have no long-term memory on this server, so there is nothing "+
			"to flag."), nil
	}
	id := strings.TrimSpace(stringArg(args, "id"))
	if id == "" {
		return refuse("no-fact-id", "No fact id, so nothing changed. Recall it first and use the id "+
			"it came back with."), nil
	}
	reason := strings.TrimSpace(stringArg(args, "reason"))
	if reason == "" {
		return refuse("no-flag-reason", "NOTHING WAS FLAGGED: a flag with no reason is a row nobody "+
			"can act on. Say in one line what they told you instead."), nil
	}
	if err := s.mem.Flag(ctx, id, reason); err != nil {
		s.log.Warn("assistant: fact not flagged", "fact", id, "error", err)
		return refuse("flag-failed", "That fact could not be flagged -- it may no longer exist. Say "+
			"so plainly."), nil
	}
	s.journalMemoryWrite(ctx, "", fmt.Sprintf("flagged a remembered fact (%s): %s", id, reason))
	return map[string]any{"flagged": true, "id": id,
		"note": "Kept, not deleted: it goes to them to confirm, edit or drop. Tell them it will " +
			"stop being treated as true."}, nil
}

// journalMemoryWrite records one memory write in the journal.
//
// Note-shaped and notable, because "what the assistant filed away" is exactly
// the kind of thing the conversation should carry a record of — and because a
// write nobody can see is a write nobody can correct.
//
// It is the one notable entry that is NOT itself captured. The fact is already
// in memory; staging a sentence that says a fact was stored would hand
// consolidation a second, meta-phrased copy to judge against the first.
func (s *Service) journalMemoryWrite(ctx context.Context, projectID, summary string) {
	if _, err := s.appendJournal(ctx, journalWrite{
		Kind:        JournalNote,
		ProjectID:   projectID,
		Summary:     summary,
		Notable:     true,
		SkipCapture: true,
	}); err != nil {
		s.log.Warn("assistant: memory write not journaled", "error", err)
	}
}

// resolveMemoryProject works out which project a fact belongs to from the name a
// head said, or the global scope when it said none.
//
// The same rule [Service.resolveProject] follows and different words: nothing is
// created here, so a refusal says nothing was KEPT. It never picks — one match is
// a name, several is a description, and a description gets a question. A named
// project nothing here matches refuses rather than falling back to global,
// because global is "true everywhere" and filing a project's fact there is the
// one mistake that cannot be seen from the answer.
func (s *Service) resolveMemoryProject(ctx context.Context, spoken string) (ProjectRow, map[string]any) {
	spoken = strings.TrimSpace(spoken)
	if spoken == "" {
		return ProjectRow{}, nil
	}
	if s.dir == nil {
		return ProjectRow{}, refuse("no-directory-for-scope", "NOTHING WAS KEPT: I cannot see this "+
			"machine's projects from here, so I cannot tell which one that is. Keep it without a "+
			"project if it is true everywhere.")
	}

	rows := s.dir.ListProjects(ctx)
	matched := MatchProjects(spoken, rows)
	switch len(matched) {
	case 0:
		return ProjectRow{}, refuse("scope-project-unrecognised", fmt.Sprintf("NOTHING WAS KEPT: "+
			"nothing on this machine is called %q. Ask which project they mean, or leave the "+
			"project out if it is true everywhere.", spoken))
	case 1:
		return matched[0], nil
	default:
		names := make([]string, 0, len(matched))
		for _, row := range matched {
			names = append(names, row.DisplayName())
		}
		return ProjectRow{}, refuse("scope-project-ambiguous", fmt.Sprintf("NOTHING WAS KEPT: more "+
			"than one project could be %q — %s. Ask which they mean and never choose for them.",
			spoken, SpokenList(names)))
	}
}
