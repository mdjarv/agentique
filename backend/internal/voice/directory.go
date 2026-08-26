package voice

import (
	"context"
	"fmt"
	"strings"
)

// Directory is what the call knows about the sessions on this machine.
//
// It is a seam, not a convenience. This package must stay independent of the
// session pipeline — the same reason [Delivery] mirrors session.MessageDelivery
// rather than importing it — so the assistant asks four questions in its own
// vocabulary and the server answers them.
//
// Every method degrades to nothing rather than failing: a directory that cannot
// read the database makes the assistant vaguer, never mute. A nil Directory is
// valid and means the call can talk about the session it opened on and nothing
// else.
type Directory interface {
	// Orientation is one short paragraph the drafter is given at call open:
	// how many sessions there are and which of them are waiting on the
	// operator. It is spoken material, so it is prose and it is brief.
	Orientation(ctx context.Context) string

	// ListSessions answers a filter — [FilterNeedsAttention], [FilterRunning],
	// [FilterRecent] or [FilterAll] — most recently active first. An unknown
	// filter is treated as [FilterRecent], because a mis-transcribed word must
	// not turn into an empty answer.
	ListSessions(ctx context.Context, filter string) []SessionRow

	// SessionBrief looks up one session. The second return is false for
	// anything this machine does not own, which is the test for "can work be
	// started here" — a remote session's transcript and CLI are elsewhere.
	SessionBrief(ctx context.Context, id string) (SessionRow, bool)

	// Summarize distils a session's recent history and hands it to deliver.
	//
	// Asynchronous by contract: summarising runs a local model, and the speech
	// model is paused for the whole of a tool call. deliver is called exactly
	// once, possibly with "" — the caller has already answered the tool and is
	// waiting to say something, so "nothing to say" has to arrive as an answer
	// rather than as silence.
	Summarize(ctx context.Context, id string, deliver func(summary string))

	// ListProjects answers "where could a new session go", most recently worked
	// in first. LOCAL projects only: a session is created through this server's
	// session service, so a repository checked out on another machine is not a
	// place this call can start one.
	//
	// Like every other method here it degrades to nothing rather than failing.
	ListProjects(ctx context.Context) []ProjectRow

	// CreateSession opens a new session in projectID and returns it, already
	// described the way the assistant will speak about it.
	//
	// model is a SPOKEN FAMILY NAME ("fable", "opus") or "" for the same default
	// the composer's new-session flow gets. A name the catalog does not have
	// yields an [UnknownModelError] and no session — the assistant asks again
	// rather than running something nobody chose.
	//
	// It goes down the same path the composer's send button's sibling uses, for
	// the same reason dispatch does: one route into the session pipeline,
	// whether the gesture was a click or a sentence.
	CreateSession(ctx context.Context, projectID, model string) (SessionRow, error)
}

// UnknownModelError is what a spoken model name that is not in the catalog
// comes back as.
//
// It carries the families that ARE available, because the answer to "let's use
// fable" on a deployment without Fable is the list, not a substitute: this is
// the one place a wrong guess would be invisible to the operator until the
// session had already run on the wrong model.
type UnknownModelError struct {
	// Spoken is what the assistant asked for.
	Spoken string
	// Families are the labels this deployment does offer.
	Families []string
}

func (e *UnknownModelError) Error() string {
	if len(e.Families) == 0 {
		return fmt.Sprintf("There is no model called %q here, and I cannot see which ones there are. "+
			"Ask them to pick the model on screen instead.", e.Spoken)
	}
	return fmt.Sprintf("There is no model called %q. The ones available are %s. "+
		"Ask which of those they want; do not choose for them.", e.Spoken, spokenList(e.Families))
}

// spokenList renders a short list the way a person reads one out.
func spokenList(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

// ProjectRow is one project as the voice assistant sees it: enough to name it,
// tell it from a similarly-named one, and rank it by how recently it was worked
// in. Not enough to render it — that is the browser's job.
//
// Local by construction. Every row here is a repository this server can check
// out a worktree from, which is what makes it somewhere a session can be
// created; the world snapshot's remote projects are talked about, never used.
type ProjectRow struct {
	// ID is the project id, and is what [Directory.CreateSession] takes.
	ID string
	// Name is what the operator calls it.
	Name string
	// Slug is its stable handle, and is often what they say instead.
	Slug string
	// LastActivity is UTC RFC3339 seconds of the most recent work in it, or ""
	// when nothing has run there. Compared as text, which is correct for that
	// format and for nothing else.
	LastActivity string
}

// displayName is what to call a project out loud.
func (r ProjectRow) displayName() string {
	if r.Name != "" {
		return r.Name
	}
	if r.Slug != "" {
		return r.Slug
	}
	return "an unnamed project"
}

// Session list filters, as declared to the speech model.
const (
	// FilterNeedsAttention: waiting on the operator — approval, a question, or
	// a completion they have not seen.
	FilterNeedsAttention = "needs_attention"
	// FilterRunning: a turn is in flight.
	FilterRunning = "running"
	// FilterRecent: whatever was active most recently.
	FilterRecent = "recent"
	// FilterAll: everything not filed away.
	FilterAll = "all"
)

// Attention is why a session is waiting on the operator, in the vocabulary the
// deck's "Needs you" band uses. Ordered the way lib/session/priority.ts orders
// it: the two that hold a process come before the one that does not.
const (
	// AttentionApproval: stopped on a tool permission prompt.
	AttentionApproval = "approval"
	// AttentionQuestion: stopped on a question for the operator.
	AttentionQuestion = "question"
	// AttentionUnread: finished, and nobody has looked at the result.
	AttentionUnread = "unread"
)

// AttentionRank orders the reasons a session waits on the operator. Lower is
// more urgent, and anything with no claim on attention sorts last.
//
// Exported because the server side sorts its own rows by the same rule, and two
// copies of an ordering is how a session comes back "needs approval" in one
// answer and "finished" in the next.
func AttentionRank(attention string) int {
	switch attention {
	case AttentionApproval:
		return 0
	case AttentionQuestion:
		return 1
	case AttentionUnread:
		return 2
	default:
		return 3
	}
}

// stateRunning is the one session state this package reasons about: a turn is
// in flight. Every other state it only repeats.
const stateRunning = "running"

// SessionRow is one session as the voice assistant sees it: enough to name it,
// tell it apart from a session with a similar name on another machine, and say
// what it is doing. Not enough to render it — that is the browser's job.
//
// The same shape carries both halves of the picture. Rows from [Directory] are
// this machine's own, read from its database; rows from the browser's world
// snapshot describe every machine the operator has open, including ones this
// server cannot reach. They merge, and a local row wins where both exist.
type SessionRow struct {
	// ID is the session id. Empty is not a session.
	ID string
	// Name is what the operator calls it.
	Name string
	// ProjectSlug and ProjectName place it in a repository.
	ProjectSlug string
	ProjectName string
	// MachineID and MachineName say where it runs. A row whose machine is not
	// this one can be talked about, never dispatched to.
	MachineID   string
	MachineName string
	// State is the session lifecycle state — idle, running, failed and so on.
	State string
	// Attention is why it is waiting on the operator, or "" if it is not.
	Attention string
	// Branch is the worktree branch, where there is one.
	Branch string
	// Model is the stable family name the session runs — "Opus", "Fable" —
	// never a version or a slug, for the same reason the picker's labels carry
	// none. Empty where the machine that reported the row does not say.
	Model string
	// LastActivity is UTC RFC3339 seconds, or "" when unknown. Compared as
	// text, which is correct for that format and for nothing else.
	LastActivity string
}

// hasAttention reports whether this session is waiting on the operator.
func (r SessionRow) hasAttention() bool { return r.Attention != "" }
