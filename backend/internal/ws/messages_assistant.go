package ws

import "errors"

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

// --- Assistant validation ---

var (
	errAssistantTextRequired = errors.New("text is required")
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

func (p *AssistantSayPayload) Validate() error {
	text := trimSpace(p.Text)
	if text == "" {
		return errAssistantTextRequired
	}
	return validateMaxLen("text", text, maxContentLen)
}
