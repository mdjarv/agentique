package peer

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Settings are the owner's opt-ins, from the [peer] config section. Both
// default to false: a paired machine can be listed and can report, and acts on
// nothing until someone at it says so.
type Settings struct {
	// AcceptActions lets a paired server create sessions here and send to them.
	AcceptActions bool
	// AcceptPolicies additionally lets it do so under a standing instruction —
	// work nobody asked for in the moment. Inert without AcceptActions.
	AcceptPolicies bool
}

// Refusal reasons, stable on the wire so the acting side can say each one in
// its own words rather than relaying this server's sentence.
const (
	ReasonActionsOff    = "actions-not-accepted"
	ReasonPoliciesOff   = "policies-not-accepted"
	ReasonArchived      = "archived"
	ReasonMainWorktree  = "main-worktree"
	ReasonNotFullAuto   = "not-full-auto"
	ReasonRate          = "rate-limited"
	ReasonInFlight      = "too-many-in-flight"
	ReasonNotFound      = "not-found"
	ReasonNoProject     = "unknown-project"
	ReasonAmbiguous     = "ambiguous-project"
	ReasonUnknownModel  = "unknown-model"
	ReasonBadRequest    = "bad-request"
	ReasonNotPeer       = "peer-credential-required"
	fullAutoMode        = "fullAuto"
	maxPolicyIDBytes    = 128
	maxRequestIDBytes   = 128
	maxModelBytes       = 64
	maxSessionNameRunes = 200
)

// Rate ceilings per credential. A backstop the acting server's budgets cannot
// widen, sized for a person's working day of delegated work rather than for a
// loop: a burst past these is a runaway, not a busy morning.
const (
	SendsPerMinute       = 30
	CreatesPerHour       = 20
	MaxInFlightAssistant = 25
)

// Refusal is a guard's "no": an HTTP status, a stable reason and a sentence
// the owner stands behind. The zero value is "yes".
type Refusal struct {
	Status  int
	Reason  string
	Message string
}

// Refused reports whether this is a "no".
func (r Refusal) Refused() bool { return r.Reason != "" }

func refuse(status int, reason, format string, args ...any) Refusal {
	return Refusal{Status: status, Reason: reason, Message: fmt.Sprintf(format, args...)}
}

// SendFacts is what the owner knows about the target of a send.
type SendFacts struct {
	Archived        bool
	WorktreeBranch  string
	AutoApproveMode string
}

// JudgeSend decides whether a paired server may send to a session here.
//
// Ordered so the answer names the most fundamental reason: a machine that has
// not opted in says so before anything about the session is revealed.
func JudgeSend(s Settings, policyID string, f SendFacts) Refusal {
	if r := judgeOptIn(s, policyID); r.Refused() {
		return r
	}
	if f.Archived {
		return refuse(http.StatusConflict, ReasonArchived, "that session is archived")
	}
	// The main worktree is the operator's own checkout. Nothing merges out of
	// a linked worktree without a person, which is what makes a send there
	// contained; a send to the main one is not.
	if f.WorktreeBranch == "" {
		return refuse(http.StatusForbidden, ReasonMainWorktree, "that session works in the main worktree")
	}
	// Anything short of full auto can stop on a prompt, and nobody at the
	// acting server can answer it: the run would stop with nobody told.
	if f.AutoApproveMode != fullAutoMode {
		return refuse(http.StatusConflict, ReasonNotFullAuto, "that session is not in full auto (it is %q)", f.AutoApproveMode)
	}
	return Refusal{}
}

// JudgeCreate decides whether a paired server may create a session here.
// inFlight is how many assistant-origin sessions are unfinished right now.
func JudgeCreate(s Settings, policyID string, inFlight int) Refusal {
	if r := judgeOptIn(s, policyID); r.Refused() {
		return r
	}
	if inFlight >= MaxInFlightAssistant {
		return refuse(http.StatusTooManyRequests, ReasonInFlight,
			"%d assistant sessions are already unfinished here", inFlight)
	}
	return Refusal{}
}

func judgeOptIn(s Settings, policyID string) Refusal {
	if !s.AcceptActions {
		return refuse(http.StatusForbidden, ReasonActionsOff, "this machine does not accept actions from paired servers")
	}
	if policyID != "" && !s.AcceptPolicies {
		return refuse(http.StatusForbidden, ReasonPoliciesOff, "this machine does not accept work started under a standing instruction")
	}
	return Refusal{}
}

// limiter is a sliding-window count per credential and action.
type limiter struct {
	mu     sync.Mutex
	now    func() time.Time
	events map[string][]time.Time
}

func newLimiter(now func() time.Time) *limiter {
	return &limiter{now: now, events: make(map[string][]time.Time)}
}

// take records one event under key if fewer than max happened within window,
// and reports whether it did.
func (l *limiter) take(key string, max int, window time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	cutoff := now.Add(-window)
	kept := l.events[key][:0]
	for _, at := range l.events[key] {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	if len(kept) >= max {
		l.events[key] = kept
		return false
	}
	l.events[key] = append(kept, now)
	return true
}
