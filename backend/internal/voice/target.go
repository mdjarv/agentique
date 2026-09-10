package voice

import "fmt"

// Where a prompt lands is the one thing on this call nobody can see.
//
// Everything else a dispatch can get wrong, the server can check: a session on
// another machine is refused, a session that would stop for approval is
// refused. The target was not checkable at all, because [call.runPrompt] took
// no target — it read the focus, which is ambient state the assistant can only
// guess at. So the two could disagree and nothing in the system was in a
// position to notice.
//
// It happened. A call opened on a session in one project, the operator dictated
// a prompt that named a different project out loud, the assistant never created
// or focused anything, and `run_prompt` sent the work to the session the call
// happened to be pointing at. The coding agent that received it worked out on
// its own that it had been handed somebody else's job.
//
// The fix is not a better instruction. It is giving the tool a target to
// disagree with: the assistant passes the name it just said out loud, and this
// is where that claim meets the focus. A name rather than an id on purpose —
// an id is a token the model can copy correctly while believing something else,
// where the name is the same string the operator's yes was given against, so
// checking it is checking the read-back rather than a parallel fact.
//
// The rule is asymmetric, because the two mistakes cost different amounts.
// Sending to the wrong session loses real work and is invisible to someone
// driving. Refusing a send that was fine costs a sentence and a retry. So this
// accepts generously and refuses loudly, and every refusal is written to be
// spoken.

// Reasons a dispatch was refused on its target. They are logged, never spoken —
// the words are separate, because the log wants a token to grep and the
// listener wants a sentence.
const (
	// targetMissing: the assistant did not say where it was sending.
	targetMissing = "target-missing"
	// targetElsewhere: it named a session that is not the focused one.
	targetElsewhere = "target-elsewhere"
	// targetOtherProject: it named a project the focused session is not in.
	targetOtherProject = "target-other-project"
	// targetUnrecognised: it named something nothing here matches.
	targetUnrecognised = "target-unrecognised"
)

// targetJudgement is what the server makes of the target the assistant named.
type targetJudgement struct {
	// OK is whether the dispatch may proceed.
	OK bool
	// Reason is the machine-readable refusal, for the log. Empty when OK.
	Reason string
	// Say is the refusal, written to be read out. Empty when OK.
	Say string
}

// judgeTarget decides whether the target the assistant named is the session the
// call is aimed at.
//
// known is every session the server has named to the model, which is the pool a
// wrong target is most likely drawn from; it may include the focus, which is
// filtered out. projects is looked up lazily and only when a refusal is already
// certain, because it is a database read and the accepting path is the common
// one — it may be nil, which costs the sentence its sharpest branch and nothing
// else.
func judgeTarget(spoken string, focus SessionRow, known []SessionRow, projects func() []ProjectRow) targetJudgement {
	// A check that cannot be performed must not refuse — including the check
	// that a target was given at all, since demanding one buys nothing when
	// there is nothing to compare it against. A call wired to no directory knows
	// exactly one session, the one it opened on, so there is no other target for
	// a prompt to reach.
	if !describable(focus) {
		return targetJudgement{OK: true}
	}

	tokens := normalizeTokens(spoken)
	if len(tokens) == 0 {
		return targetJudgement{Reason: targetMissing, Say: "NOTHING WAS SENT: this call needs to be " +
			"told where the prompt is going. Call it again straight away with `target` set to the " +
			"session name you just read back to them. Say nothing to them about this first — it is " +
			"between you and me, and they are waiting on the send they already agreed to."}
	}

	// The strongest signal first: the words fit some other session better than
	// they fit this one, and fit it well enough to be worth confirming. That
	// catches the near miss inside one project, which naming alone would let
	// through.
	if other, ok := clearlyElsewhere(spoken, focus, known); ok {
		return targetJudgement{
			Reason: targetElsewhere,
			Say: fmt.Sprintf("NOTHING WAS SENT. You named %s, and this call is aimed at %s. They are "+
				"different sessions, so the prompt would have gone to the wrong one. If %s is where "+
				"they agreed it should go, switch to it with %s, read the prompt back naming that "+
				"session, and send once they say yes.",
				displayFor(other), displayFor(focus), displayFor(other), ToolFocusSession),
		}
	}

	// Otherwise the question is only whether they said a word that belongs to
	// this session — its own name, or the project it is in. Either is a fair way
	// to name where work is going, and one salient word is enough: "the riff one"
	// and "Live Melodikrysset Sessions" are the same claim.
	if namesRow(tokens, focus) {
		return targetJudgement{OK: true}
	}

	if project, ok := namedProject(spoken, focus, projects); ok {
		return targetJudgement{
			Reason: targetOtherProject,
			Say: fmt.Sprintf("NOTHING WAS SENT. You named %s, and this call is aimed at %s, which is "+
				"not in it. Work belonging in %s cannot go there. If it needs a new session, call %s "+
				"with that project and this prompt in one call. If it belongs in a session that "+
				"already exists there, find it with %s and switch to it first.",
				project.displayName(), displayFor(focus), project.displayName(),
				ToolCreateSession, ToolFindSession),
		}
	}

	return targetJudgement{
		Reason: targetUnrecognised,
		Say: fmt.Sprintf("NOTHING WAS SENT: I cannot tell whether %q is the session this call is "+
			"aimed at, which is %s. Tell them out loud that it is going to %s and ask whether that is "+
			"right. If they say yes, send again with `target` set to that name.",
			spoken, displayFor(focus), displayFor(focus)),
	}
}

// describable reports whether there is enough of a row to check a spoken name
// against. An id alone is not a name, and refusing against it would refuse
// everything.
func describable(row SessionRow) bool {
	return row.Name != "" || row.ProjectName != "" || row.ProjectSlug != ""
}

// clearlyElsewhere reports the session the words fit better, when one stands out
// far enough to be worth saying.
//
// It reuses the matcher the assistant's own `find_session` uses, so "clear
// enough to act on" means the same thing here as it does there — and, as there,
// a near-tie is not clear and falls through to the gentler checks.
func clearlyElsewhere(spoken string, focus SessionRow, known []SessionRow) (SessionRow, bool) {
	others := make([]SessionRow, 0, len(known))
	for _, row := range known {
		if row.ID == "" || row.ID == focus.ID || !describable(row) {
			continue
		}
		others = append(others, row)
	}
	if len(others) == 0 {
		return SessionRow{}, false
	}

	candidates, topIsClear := MatchSessions(spoken, others)
	if !topIsClear || len(candidates) == 0 {
		return SessionRow{}, false
	}
	// A clear winner among the others is only a wrong target if it beats this
	// one. "The voice session" against a focus actually called that must not be
	// refused because a similarly-named session exists somewhere else.
	if scoreRow(normalizeTokens(spoken), focus) >= candidates[0].Score {
		return SessionRow{}, false
	}
	return candidates[0].Row, true
}

// namesRow reports whether any salient word of the spoken target belongs to this
// session — its own name, or the project it sits in.
//
// One word, not a mean over all of them. The assistant says "a new session in
// Agentique" and "the Agentique one", and a scored average punishes both for the
// words that carry no identity; what matters is whether anything they said is
// actually this session's.
func namesRow(tokens []string, row SessionRow) bool {
	fields := [][]string{
		normalizeTokens(row.Name),
		normalizeTokens(row.ProjectName),
		normalizeTokens(row.ProjectSlug),
	}
	for _, token := range tokens {
		for _, field := range fields {
			if bestTokenScore(token, field) >= scorePrefix {
				return true
			}
		}
	}
	return false
}

// namedProject reports the project the assistant named, when it named one and it
// is not the focused session's.
//
// This is the shape the incident took — a prompt about one repository sent to a
// session in another — so it gets its own sentence rather than falling into the
// generic refusal, and that sentence names the tool that would have been right.
func namedProject(spoken string, focus SessionRow, projects func() []ProjectRow) (ProjectRow, bool) {
	if projects == nil {
		return ProjectRow{}, false
	}
	rows := projects()
	if len(rows) == 0 {
		return ProjectRow{}, false
	}

	matched := MatchProjects(spoken, rows)
	// One match is a name; several is a description, and describing is not
	// naming. The rule everywhere else in this package: never pick.
	if len(matched) != 1 {
		return ProjectRow{}, false
	}
	if sameProject(matched[0], focus) {
		return ProjectRow{}, false
	}
	return matched[0], true
}

// sameProject reports whether a project row and a session's project are the
// same place. Compared by name and slug rather than id, because a session row
// from the browser's world snapshot carries no project id.
func sameProject(project ProjectRow, row SessionRow) bool {
	if project.Name != "" && project.Name == row.ProjectName {
		return true
	}
	return project.Slug != "" && project.Slug == row.ProjectSlug
}
