package ws

import (
	"errors"

	"github.com/mdjarv/agentique/backend/internal/assistant"
)

// --- Assistant payloads (docs/assistant.md) ---
//
// Every field is optional on the wire, as every field here is: the generated Zod
// schema mirrors these tags, and dropping omitempty makes a field REQUIRED —
// after which a client rejects the whole payload from any peer that does not
// send it. An absent field means "not set".

// AssistantSayPayload is the operator's turn in the conversation.
//
// It carries no surface. The conversation is shared and every message has to
// say where it was said, but this socket IS the thread — so the surface is a
// fact the server holds, never a name the caller hands in. A client that could
// claim one could attribute its message to a voice call and stamp that call's
// seen mark, which would swallow the next greeting's news. Another transport
// gets its own op or its own service call, the way a call goes through
// [assistant.Service.Mirror].
type AssistantSayPayload struct {
	// Text is what they said. Required in practice, and validated here rather
	// than by the core, so an empty send costs a round trip and not a turn.
	Text string `json:"text,omitempty"`
}

// AssistantHistoryPayload asks for one page of the conversation, newest page
// first and paging backwards.
type AssistantHistoryPayload struct {
	// Before is a cursor from a previous page, or "" for the newest.
	//
	// Opaque: it is a (timestamp, id) pair, because a boundary compared on the
	// stamp alone skips every row that shares it. Hand back what you were
	// given rather than assembling one.
	Before string `json:"before,omitempty"`
	// Limit is how many messages at most. 0 asks for the server's cap.
	Limit int `json:"limit,omitempty"`
}

// AssistantJournalPayload asks what has happened, newest first.
type AssistantJournalPayload struct {
	// Since is a UTC RFC3339-seconds timestamp to read from, or "" for
	// everything the cap allows.
	Since string `json:"since,omitempty"`
	// Limit is how many entries at most. 0 asks for the server's cap.
	Limit int `json:"limit,omitempty"`
}

// AssistantUnseenPayload asks how many journal entries the thread has never been
// shown. It carries nothing: the surface is this socket's, as for every
// assistant op.
type AssistantUnseenPayload struct{}

// AssistantUnseenResult is the count behind the rail row's notch.
type AssistantUnseenResult struct {
	Count int `json:"count,omitempty"`
}

// AssistantMarkSeenPayload says the thread is on screen and has shown what it
// holds. It carries nothing, for the same reason.
type AssistantMarkSeenPayload struct{}

// AssistantProposalsPayload asks what has been proposed, open first.
type AssistantProposalsPayload struct {
	// Limit is how many rows at most. 0 asks for the server's cap.
	Limit int `json:"limit,omitempty"`
}

// AssistantDecidePayload is the yes or the no on one proposal.
//
// It carries no surface, as no assistant op does: `decided_via` is the record
// of WHERE a decision was given, and a client that could name its own surface
// could record a card the operator pressed on screen as a yes spoken on a
// call.
type AssistantDecidePayload struct {
	// ID is the proposal's id.
	ID string `json:"id,omitempty"`
	// Accept is the decision. Absent is a decline, which is the safe reading:
	// nothing is performed.
	Accept bool `json:"accept,omitempty"`
}

// AssistantDigestPayload asks for a digest now. It carries nothing: the window
// is the server's mark, not a range a client picks.
type AssistantDigestPayload struct{}

// AssistantCompactPayload asks for the journal's older days to be folded now.
//
// It carries nothing, and deliberately no knobs: which days are old enough, how
// many one pass folds and how long a summary is kept are the server's rules
// (docs/assistant.md, the M5 contract), not a range a client picks. A client that
// could name the cutoff could delete this week.
type AssistantCompactPayload struct{}

// AssistantPoliciesPayload asks for the standing instructions. It carries
// nothing: they are one short list and there is nothing to page or filter.
type AssistantPoliciesPayload struct{}

// AssistantPolicySavePayload is one standing instruction, whole.
//
// The whole row rather than a patch, because the client is one form with a Save
// button: a field-by-field update would need a way to say "leave this alone",
// which is a shape nothing here asks for. An empty ID creates one.
//
// The budgets are optional and zero means "the server's default", never "none
// allowed" — a client that omits a field must not silently write a policy that
// can do nothing.
type AssistantPolicySavePayload struct {
	ID             string `json:"id,omitempty"`
	Name           string `json:"name,omitempty"`
	Text           string `json:"text,omitempty"`
	Enabled        bool   `json:"enabled,omitempty"`
	BudgetInFlight int    `json:"budgetInFlight,omitempty"`
	BudgetPerDay   int    `json:"budgetPerDay,omitempty"`
}

// AssistantPolicyDeletePayload removes one standing instruction.
type AssistantPolicyDeletePayload struct {
	ID string `json:"id,omitempty"`
}

// AssistantPoliciesResult wraps the list rather than answering a bare array, on
// the same argument as every other list op here: an object can grow a field
// without a wire transition and a top-level array cannot grow at all.
type AssistantPoliciesResult struct {
	Policies []assistant.Policy `json:"policies,omitempty"`
}

// --- Assistant validation ---

var (
	errAssistantTextRequired = errors.New("text is required")
	// errAssistantProposalRequired is what a decide with no id answers.
	errAssistantProposalRequired = errors.New("id is required")
	// errAssistantPolicyNameRequired is what a save with no name answers. The
	// core refuses it too — this is the cheap round trip, mirrored the way
	// lib/composer-constants.ts mirrors the attachment cap: refusing here what
	// the core would refuse anyway costs nothing and answers sooner.
	errAssistantPolicyNameRequired = errors.New("name is required")
	// errAssistantPolicyIDRequired is what a delete with no id answers.
	errAssistantPolicyIDRequired = errors.New("id is required")
	// errAssistantDisabled is what every assistant op answers when the feature
	// is off.
	//
	// The ops are in the registry unconditionally — it is a package-level table,
	// and a read on the concurrent lane must be a registered handler — so "off"
	// is this sentence rather than an unknown message type. It names the switch,
	// because a client that got here either ignored `features.assistant` or is
	// talking to a peer that has the assistant turned off, and both are worth
	// telling apart from a bug.
	errAssistantDisabled = errors.New("the assistant is not enabled on this machine ([experimental] assistant)")
)

func (p *AssistantDecidePayload) Validate() error {
	if trimSpace(p.ID) == "" {
		return errAssistantProposalRequired
	}
	return nil
}

func (p *AssistantPolicySavePayload) Validate() error {
	if trimSpace(p.Name) == "" {
		return errAssistantPolicyNameRequired
	}
	return nil
}

func (p *AssistantPolicyDeletePayload) Validate() error {
	if trimSpace(p.ID) == "" {
		return errAssistantPolicyIDRequired
	}
	return nil
}

func (p *AssistantSayPayload) Validate() error {
	text := trimSpace(p.Text)
	if text == "" {
		return errAssistantTextRequired
	}
	return validateMaxLen("text", text, maxContentLen)
}
