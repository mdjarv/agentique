package ws

import (
	"strings"
	"testing"
	"time"
)

// The assistant's three ops are in the registry whether or not the feature is
// on — the table is package-level, and the read lane's membership test demands
// that every name on it be a registered handler — so "off" has to be an ANSWER
// rather than an absence.
//
// It answers in words and names the switch, because a client that got here
// either ignored `features.assistant` or is talking to a peer with the
// assistant turned off, and those are worth telling apart from a bug.
func TestAssistantOpsAnswerWhenTheAssistantIsOff(t *testing.T) {
	for _, op := range []struct {
		name    string
		payload string
	}{
		{"assistant.say", `{"text":"what needs me?"}`},
		{"assistant.history", `{}`},
		{"assistant.journal", `{}`},
		{"assistant.unseen", `{}`},
		{"assistant.mark-seen", `{}`},
		{"assistant.proposals", `{}`},
		{"assistant.decide", `{"id":"p1","accept":true}`},
		{"assistant.digest", `{}`},
		{"assistant.policies", `{}`},
		{"assistant.policy-save", `{"name":"nightly tests"}`},
		{"assistant.policy-delete", `{"id":"pol1"}`},
	} {
		t.Run(op.name, func(t *testing.T) {
			c := newDispatchTestConn()
			defer c.close()

			c.dispatch(ClientMessage{ID: "1", Type: op.name, Payload: []byte(op.payload)})

			resp := awaitResponse(c, time.Second)
			if resp == nil {
				t.Fatal("no response: an op in the registry must always answer")
			}
			if resp.Error == nil {
				t.Fatalf("%s answered %+v with no assistant wired", op.name, resp.Payload)
			}
			if !strings.Contains(resp.Error.Message, "assistant") {
				t.Errorf("the refusal does not name the switch: %q", resp.Error.Message)
			}
		})
	}
}

// A policy save with no name names the field rather than reaching the core, and
// a delete with no id does the same.
func TestAssistantPolicyWritesValidateTheirFields(t *testing.T) {
	for _, tt := range []struct {
		op, payload, want string
	}{
		{"assistant.policy-save", `{"text":"when the tests pass, say so"}`, "name"},
		{"assistant.policy-delete", `{}`, "id"},
	} {
		t.Run(tt.op, func(t *testing.T) {
			c := newDispatchTestConn()
			defer c.close()

			c.dispatch(ClientMessage{ID: "1", Type: tt.op, Payload: []byte(tt.payload)})

			resp := awaitResponse(c, time.Second)
			if resp == nil || resp.Error == nil {
				t.Fatalf("%s answered %+v, want a validation refusal", tt.op, resp)
			}
			if !strings.Contains(resp.Error.Message, tt.want) {
				t.Errorf("the refusal does not name the field: %q", resp.Error.Message)
			}
		})
	}
}

// A decide with no proposal names the field rather than reaching the core.
func TestAssistantDecideRefusesWithoutAnID(t *testing.T) {
	c := newDispatchTestConn()
	defer c.close()

	c.dispatch(ClientMessage{ID: "1", Type: "assistant.decide", Payload: []byte(`{"accept":true}`)})

	resp := awaitResponse(c, time.Second)
	if resp == nil || resp.Error == nil {
		t.Fatalf("a decide with no id answered %+v, want a validation refusal", resp)
	}
	if !strings.Contains(resp.Error.Message, "id") {
		t.Errorf("the refusal does not name the field: %q", resp.Error.Message)
	}
}

// The write op validates before it reaches the core, so an empty send costs a
// round trip rather than a turn.
func TestAssistantSayRefusesAnEmptyTurn(t *testing.T) {
	c := newDispatchTestConn()
	defer c.close()

	c.dispatch(ClientMessage{ID: "1", Type: "assistant.say", Payload: []byte(`{"text":"   "}`)})

	resp := awaitResponse(c, time.Second)
	if resp == nil || resp.Error == nil {
		t.Fatalf("an empty say answered %+v, want a validation refusal", resp)
	}
	if !strings.Contains(resp.Error.Message, "text") {
		t.Errorf("the refusal does not name the field: %q", resp.Error.Message)
	}
}

// The two reads are on the concurrent lane and the write is not. Membership
// there is a CLAIM that the handler mutates nothing a later request could
// observe out of order, and `assistant.say` starts a turn.
func TestAssistantLanes(t *testing.T) {
	for _, op := range []string{
		"assistant.history", "assistant.journal", "assistant.unseen", "assistant.proposals",
		"assistant.policies",
	} {
		if !concurrentOps[op] {
			t.Errorf("%s is on the serial lane; it is a read", op)
		}
	}
	// `assistant.decide` performs the action and `assistant.digest` writes a
	// message; both are mutations. Both also run off the dispatch loop through
	// handleRequestAsync rather than on the read lane — a merge takes seconds
	// and a digest resolves a name per line, and a read there would be claiming
	// it mutates nothing.
	// The two policy writes are on the serial lane for the ordering reason rather
	// than a slowness one: two saves of one policy have to land in the order they
	// were sent, or the row keeps the older edit.
	for _, op := range []string{
		"assistant.say", "assistant.mark-seen", "assistant.decide", "assistant.digest",
		"assistant.policy-save", "assistant.policy-delete",
	} {
		if concurrentOps[op] {
			t.Errorf("%s is on the read lane, and it writes", op)
		}
	}
}
