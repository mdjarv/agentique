package ws

import (
	"context"

	"github.com/mdjarv/agentique/backend/internal/assistant"
)

// The assistant's ops (docs/assistant.md).
//
// Three of them, split across the socket's two lanes on the rule the lanes are
// for: `assistant.say` writes and stays on the serial MUTATION lane, so what the
// operator sent in order lands in order; `assistant.history` and
// `assistant.journal` mutate nothing a later request could observe out of order
// and run on the READ lane, where a slow one cannot hold up the session history
// queued behind it.
//
// Everything the thread renders arrives as a push on the global topic — the
// conversation is project-less, so there is no topic to scope it to — and the
// replies here are only what the caller needs immediately.

// handleAssistantSay puts the operator's turn in the conversation and starts the
// head on it.
//
// It answers with the STORED ASK, not the reply. A head turn takes seconds to
// minutes and `dispatchLoop` runs a mutation serially in arrival order, so
// waiting for the answer here would hold this connection's whole mutation lane
// for the length of the turn with every later mutation — a stop, an approval —
// queued behind it. Nothing is lost by answering early: the reply arrives on
// `assistant.delta` and then `assistant.message`, which is what the thread
// renders from either way.
func (c *conn) handleAssistantSay(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p AssistantSayPayload) (assistant.Message, error) {
		if c.assistantSvc == nil {
			return assistant.Message{}, errAssistantDisabled
		}
		// The surface is this socket's, not the payload's: see
		// [AssistantSayPayload].
		return c.assistantSvc.SayAsync(ctx, assistant.SurfaceThread, p.Text)
	})
}

// handleAssistantHistory reads one page of the conversation, oldest first.
//
// It CREATES nothing, which is what makes its place on the read lane honest: the
// first visit to the thread on a fresh machine answers an empty page rather than
// inserting the channel row and stamping the state row. Creating the
// conversation belongs to the writers, on the mutation lane.
func (c *conn) handleAssistantHistory(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p AssistantHistoryPayload) (assistant.Page, error) {
		if c.assistantSvc == nil {
			return assistant.Page{}, errAssistantDisabled
		}
		return c.assistantSvc.History(ctx, p.Before, p.Limit)
	})
}

// handleAssistantJournal reads what has happened, newest first.
func (c *conn) handleAssistantJournal(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p AssistantJournalPayload) (AssistantJournalResult, error) {
		if c.assistantSvc == nil {
			return AssistantJournalResult{}, errAssistantDisabled
		}
		entries, err := c.assistantSvc.Journal(ctx, p.Since, p.Limit)
		if err != nil {
			return AssistantJournalResult{}, err
		}
		return AssistantJournalResult{Entries: entries}, nil
	})
}

// handleAssistantUnseen counts what the thread has never been shown: the rail
// row's notch, read once per connection. A pure read, on the read lane.
func (c *conn) handleAssistantUnseen(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, _ AssistantUnseenPayload) (AssistantUnseenResult, error) {
		if c.assistantSvc == nil {
			return AssistantUnseenResult{}, errAssistantDisabled
		}
		n, err := c.assistantSvc.UnseenCount(ctx, assistant.SurfaceThread)
		if err != nil {
			return AssistantUnseenResult{}, err
		}
		return AssistantUnseenResult{Count: n}, nil
	})
}

// handleAssistantMarkSeen stamps the thread's look. It WRITES — the journal's
// seen marks and the conversation mark — so it is on the mutation lane, and
// it is the only assistant op the thread sends that does not carry text.
func (c *conn) handleAssistantMarkSeen(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, _ AssistantMarkSeenPayload) (struct{}, error) {
		if c.assistantSvc == nil {
			return struct{}{}, errAssistantDisabled
		}
		return struct{}{}, c.assistantSvc.Look(ctx, assistant.SurfaceThread)
	})
}

// AssistantJournalResult wraps the entries rather than answering a bare array.
//
// An object is the shape every other list op answers with, and it is the one a
// later field (a cursor, a count) can be added to without a wire transition —
// where a top-level array cannot grow at all.
type AssistantJournalResult struct {
	Entries []assistant.JournalEntry `json:"entries,omitempty"`
}
