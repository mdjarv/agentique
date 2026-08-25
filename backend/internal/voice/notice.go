package voice

import "fmt"

// NoticeKind is a runtime fact about a followed session.
//
// These are exactly the three things a working agent **cannot** report about
// itself, which is why they come from the runtime rather than from a tool call:
// it is suspended, it is gone, or it has stopped. Everything an agent *can*
// judge — surprises, decisions, milestones — arrives as a [Report] instead.
type NoticeKind string

const (
	// NoticeFinished: the turn ended cleanly. The summary is what the listener
	// has been waiting for.
	NoticeFinished NoticeKind = "finished"
	// NoticeFailed: the turn ended badly.
	NoticeFailed NoticeKind = "failed"
	// NoticeBlocked: the run is waiting on something this call cannot supply.
	//
	// Live voice runs in auto mode precisely so this should not happen. When it
	// does it means "go and look at a screen", never "answer me" — there is no
	// spoken approval, because approving a command you cannot see, through a
	// transcription, is not consent.
	NoticeBlocked NoticeKind = "blocked"
)

// priority orders notices by how much they demand of the listener, matching
// lib/session/priority.ts: the thing that holds a process outranks the thing
// that already stopped. One rule, both surfaces — a session that says "needs
// approval" in the rail cannot say something else in your ear.
func (k NoticeKind) priority() int {
	switch k {
	case NoticeBlocked:
		return 0
	case NoticeFailed:
		return 1
	case NoticeFinished:
		return 2
	default:
		return 3
	}
}

// endsWork reports whether this notice means the run is over, so the call can
// go back to treating silence as abandonment rather than as work in progress.
func (k NoticeKind) endsWork() bool {
	return k == NoticeFinished || k == NoticeFailed
}

// Notice is one runtime fact, headed for whoever is listening.
type Notice struct {
	Kind NoticeKind `json:"kind"`
	// Headline is the spoken form. For a finished run this is the summary.
	Headline string `json:"headline"`
}

// ParseNoticeKind validates a kind from a caller outside this package.
func ParseNoticeKind(kind string) (NoticeKind, error) {
	k := NoticeKind(kind)
	switch k {
	case NoticeFinished, NoticeFailed, NoticeBlocked:
		return k, nil
	default:
		return "", fmt.Errorf("unknown notice kind %q: want finished, failed or blocked", kind)
	}
}
