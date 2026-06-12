package session

import (
	"testing"
)

func TestCoalescePending(t *testing.T) {
	t.Run("single message returns unchanged", func(t *testing.T) {
		atts := []QueryAttachment{{Name: "a.png"}}
		prompt, got := coalescePending([]pendingMessage{{prompt: "hello", attachments: atts}})
		if prompt != "hello" {
			t.Errorf("prompt = %q, want %q", prompt, "hello")
		}
		if len(got) != 1 || got[0].Name != "a.png" {
			t.Errorf("attachments = %+v, want single a.png", got)
		}
	})

	t.Run("multiple messages join with blank line and concat attachments", func(t *testing.T) {
		msgs := []pendingMessage{
			{prompt: "first", attachments: []QueryAttachment{{Name: "a.png"}}},
			{prompt: "second"},
			{prompt: "third", attachments: []QueryAttachment{{Name: "b.pdf"}}},
		}
		prompt, atts := coalescePending(msgs)
		want := "first\n\nsecond\n\nthird"
		if prompt != want {
			t.Errorf("prompt = %q, want %q", prompt, want)
		}
		if len(atts) != 2 || atts[0].Name != "a.png" || atts[1].Name != "b.pdf" {
			t.Errorf("attachments = %+v, want [a.png b.pdf] in order", atts)
		}
	})
}

func TestQueuePendingMessage_GatesOnRunning(t *testing.T) {
	t.Run("running enqueues and echoes a queued user_message", func(t *testing.T) {
		var events []any
		sess := &Session{
			ID:        "s1",
			state:     StateRunning,
			broadcast: func(_ string, payload any) { events = append(events, payload) },
		}

		ok := sess.QueuePendingMessage("do the thing", nil)
		if !ok {
			t.Fatal("QueuePendingMessage returned false for a running session")
		}
		if len(sess.pendingMessages) != 1 {
			t.Fatalf("pendingMessages len = %d, want 1", len(sess.pendingMessages))
		}
		if sess.pendingMessages[0].prompt != "do the thing" {
			t.Errorf("queued prompt = %q, want %q", sess.pendingMessages[0].prompt, "do the thing")
		}

		if len(events) != 1 {
			t.Fatalf("broadcast count = %d, want 1", len(events))
		}
		push, ok := events[0].(PushSessionEvent)
		if !ok {
			t.Fatalf("broadcast payload type = %T, want PushSessionEvent", events[0])
		}
		ev, ok := push.Event.(WireUserMessageEvent)
		if !ok {
			t.Fatalf("event type = %T, want WireUserMessageEvent", push.Event)
		}
		if !ev.Queued {
			t.Error("echoed user_message should have Queued=true")
		}
		if ev.Content != "do the thing" || ev.MessageID == "" {
			t.Errorf("echoed event = %+v, want content set and non-empty messageId", ev)
		}
		// The echo must carry the same id as the buffered message so the UI's
		// queued bubble maps to the right entry.
		if ev.MessageID != sess.pendingMessages[0].id {
			t.Errorf("echo messageId %q != buffered id %q", ev.MessageID, sess.pendingMessages[0].id)
		}
	})

	t.Run("not running rejects without enqueue or broadcast", func(t *testing.T) {
		var broadcasts int
		sess := &Session{
			ID:        "s2",
			state:     StateIdle,
			broadcast: func(_ string, _ any) { broadcasts++ },
		}

		if sess.QueuePendingMessage("nope", nil) {
			t.Error("QueuePendingMessage should return false when not running")
		}
		if len(sess.pendingMessages) != 0 {
			t.Errorf("pendingMessages len = %d, want 0", len(sess.pendingMessages))
		}
		if broadcasts != 0 {
			t.Errorf("broadcast count = %d, want 0", broadcasts)
		}
	})
}

func TestFlushPendingMessages_NoopWhenNotDrainable(t *testing.T) {
	t.Run("empty queue is a no-op", func(t *testing.T) {
		// rt is nil — if flush tried to Query it would panic/ErrNotLive, so
		// reaching the end without touching Query proves the empty-queue guard.
		sess := &Session{ID: "s3", state: StateIdle}
		sess.flushPendingMessages()
		if len(sess.pendingMessages) != 0 {
			t.Errorf("pendingMessages len = %d, want 0", len(sess.pendingMessages))
		}
	})

	t.Run("non-idle state preserves the queue", func(t *testing.T) {
		sess := &Session{
			ID:              "s4",
			state:           StateRunning,
			pendingMessages: []pendingMessage{{id: "m1", prompt: "buffered"}},
		}
		sess.flushPendingMessages()
		if len(sess.pendingMessages) != 1 {
			t.Fatalf("pendingMessages len = %d, want 1 (preserved)", len(sess.pendingMessages))
		}
		if sess.pendingMessages[0].prompt != "buffered" {
			t.Errorf("preserved prompt = %q, want %q", sess.pendingMessages[0].prompt, "buffered")
		}
	})
}
