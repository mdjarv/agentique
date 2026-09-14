package assistant

import (
	"context"
	"fmt"
	"strings"
)

// Directory is what the assistant knows about the sessions on this machine.
//
// It is a seam, not a convenience. This package must stay independent of the
// session pipeline — the same reason [Delivery] mirrors session.MessageDelivery
// rather than importing it — so the assistant asks its questions in its own
// vocabulary and the server answers them.
//
// Every method degrades to nothing rather than failing: a directory that cannot
// read the database makes the assistant vaguer, never mute. A nil Directory is
// valid and means the assistant can talk about nothing it was not told.
type Directory interface {
	// Orientation is one short paragraph a head is given when it starts: how
	// many sessions there are and which of them are waiting on the operator. It
	// may be spoken, so it is prose and it is brief.
	Orientation(ctx context.Context) string

	// ListSessions answers a filter — [FilterNeedsAttention], [FilterRunning],
	// [FilterRecent] or [FilterAll] — most recently active first. An unknown
	// filter is treated as [FilterRecent], because a mis-transcribed word must
	// not turn into an empty answer.
	//
	// It may include sessions on paired machines, marked by MachineID. Those
	// are description: [Directory.SessionBrief] does not answer for them, and
	// that is what every verb that acts on a session checks.
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
	// place the assistant can start one.
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

// PeerReachability is implemented by a [Directory] whose session lists include
// paired machines. It names the ones that did not answer, so a list without
// them can say so: "nothing matches" and "zbook is asleep" are different
// answers, and only one of them is true when a machine is off.
type PeerReachability interface {
	UnreachableMachines(ctx context.Context) []string
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
		"Ask which of those they want; do not choose for them.", e.Spoken, SpokenList(e.Families))
}

// SpokenList renders a short list the way a person reads one out.
//
// Exported because a refusal is written to be said, and every surface that
// writes one ("more than one project could be that -- a, b and c") has to read
// a list the same way.
func SpokenList(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

// ProjectRow is one project as the assistant sees it: enough to name it,
// tell it from a similarly-named one, and rank it by how recently it was worked
// in. Not enough to render it — that is the browser's job.
//
// A row is somewhere a session can be created when its [Reach] can act: this
// machine's own checkouts, and a paired machine's that accepts actions from
// this server. The world snapshot's remote projects are talked about, never
// used.
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
	// MachineID and MachineName say which machine holds this checkout. A
	// repository checked out on two machines is two rows: launching is
	// physical (docs/multi-machine.md).
	MachineID   string
	MachineName string
	// RemoteURL is the canonical git remote, the name a repository has across
	// machines. Empty for a checkout with no remote.
	RemoteURL string
	// Reach and AcceptsPolicies as on [SessionRow].
	Reach           Reach
	AcceptsPolicies bool
}

// DisplayName is what to call a project.
func (r ProjectRow) DisplayName() string {
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

// NormalizeFilter resolves what a head asked for.
//
// Anything unrecognised is [FilterRecent]: a mis-transcribed or invented
// filter must not come back as an empty list, which is indistinguishable from
// "there is nothing" — and "there is nothing" is the one answer here that
// makes an assistant look broken when it is not.
func NormalizeFilter(filter string) string {
	switch strings.ToLower(strings.TrimSpace(filter)) {
	case FilterNeedsAttention, "attention", "waiting", "needs attention":
		return FilterNeedsAttention
	case FilterRunning, "busy", "working":
		return FilterRunning
	case FilterAll, "everything":
		return FilterAll
	default:
		return FilterRecent
	}
}

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

// StateRunning is the one session state this package reasons about: a turn is
// in flight. Every other state it only repeats.
const StateRunning = "running"

// Reach is what this server can do with a session or a project.
//
// The zero value is [ReachView] on purpose: a row nobody vouched for — a world
// snapshot's, or one whose machine has not answered — is described and never
// acted on, so forgetting to set Reach fails closed.
type Reach string

const (
	// ReachView: known about, not actionable from here.
	ReachView Reach = ""
	// ReachLocal: this machine's own.
	ReachLocal Reach = "local"
	// ReachPeer: a paired machine that accepts actions from this server. Its
	// own guard still decides each one (docs/peers.md).
	ReachPeer Reach = "peer"
	// ReachPeerOff: a paired machine that serves the peer surface and has not
	// set [peer] accept-actions.
	ReachPeerOff Reach = "peer-off"
	// ReachPeerOld: a paired machine whose release predates the peer surface.
	ReachPeerOld Reach = "peer-old"
)

// CanAct reports whether work can be started through this reach.
func (r Reach) CanAct() bool { return r == ReachLocal || r == ReachPeer }

// Remote reports whether the thing lives on a paired machine this server has
// heard from, whatever that machine allows.
func (r Reach) Remote() bool { return r == ReachPeer || r == ReachPeerOff || r == ReachPeerOld }

// SessionRow is one session as the assistant sees it: enough to name it,
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
	// ProjectSlug and ProjectName place it in a repository, for a reader.
	ProjectSlug string
	ProjectName string
	// ProjectID is that repository's id, for a caller that has to FILE something
	// under it — a journal entry's subject, and through that the scope a capture
	// is staged in. Never shown: an id is noise to a reader and to a listener.
	//
	// Set for this machine's own rows only. A row from the browser's world
	// snapshot describes a project on another machine, whose id means nothing to
	// anything here, so it is left empty rather than carried over and trusted.
	ProjectID string
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
	// Reach is what can be done with it from here.
	Reach Reach
	// AcceptsPolicies is, for a [ReachPeer] row, whether its machine also
	// accepts work under a standing instruction. Always true for a local row.
	AcceptsPolicies bool
}

// HasAttention reports whether this session is waiting on the operator.
func (r SessionRow) HasAttention() bool { return r.Attention != "" }

// DisplayFor is what to call a session. Never its id: an id is noise to a
// listener and to a reader, and neither can act on it.
//
// **It always places the session in its project**, and that is the half that
// matters. Session names are auto-generated from a first prompt, so they are
// forgettable and often similar; the project is the word the operator is
// actually holding in their head. A confirmation that said "Live Melodikrysset
// Sessions" was true and useless to someone who had just asked about Agentique,
// where "Live Melodikrysset Sessions in riff" is caught in the one second it is
// still worth catching.
func DisplayFor(row SessionRow) string {
	project := row.ProjectName
	if project == "" {
		project = row.ProjectSlug
	}
	switch {
	case row.Name != "" && project != "":
		return row.Name + " in " + project
	case row.Name != "":
		return row.Name
	case project != "":
		return "an unnamed session in " + project
	default:
		return "an unnamed session"
	}
}
