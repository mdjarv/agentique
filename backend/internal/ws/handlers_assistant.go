package ws

import (
	"context"

	"github.com/mdjarv/agentique/backend/internal/assistant"
)

// The assistant's ops (docs/assistant.md).
//
// Split across the socket's two lanes on the rule the lanes are for: `assistant.say` writes and stays on the serial MUTATION lane, so what the
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

// handleAssistantProposals lists what has been proposed: open first, then
// decided, newest first.
//
// A pure read, on the read lane. The TTL is applied lazily and on the WRITE
// side — [assistant.Service.Proposals] derives an expired status rather than
// stamping one — because membership of this lane is the claim that the handler
// mutates nothing a later request could observe out of order, and an expiring
// sweep here would be exactly that.
func (c *conn) handleAssistantProposals(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p AssistantProposalsPayload) (AssistantProposalsResult, error) {
		if c.assistantSvc == nil {
			return AssistantProposalsResult{}, errAssistantDisabled
		}
		proposals, err := c.assistantSvc.Proposals(ctx, p.Limit)
		if err != nil {
			return AssistantProposalsResult{}, err
		}
		return AssistantProposalsResult{Proposals: proposals}, nil
	})
}

// handleAssistantDecide is the yes and the no.
//
// Through handleRequestAsync, because accepting performs the action: a merge
// shells out to git five times and takes seconds, and on the dispatch loop that
// would stall every RPC queued behind it — a session stop, an approval answer.
// Responses match by request id, so this handler has no ordering contract with
// its neighbours, and the core serialises decisions itself.
//
// It answers the ROW, whatever happened: accepted with what the executor did,
// stale with what had changed, failed with git's own word, or the row unchanged
// when somebody else had already decided it. A surface renders the answer
// rather than inferring one.
func (c *conn) handleAssistantDecide(msg ClientMessage) {
	handleRequestAsync(c, msg, func(ctx context.Context, p AssistantDecidePayload) (assistant.Proposal, error) {
		if c.assistantSvc == nil {
			return assistant.Proposal{}, errAssistantDisabled
		}
		// The surface is this socket's, as for every assistant op: a client that
		// could name one could record a yes as having been given on a call.
		return c.assistantSvc.Decide(ctx, assistant.SurfaceThread, p.ID, p.Accept)
	})
}

// handleAssistantDigest posts a digest and answers the message.
//
// A mutation: it writes a message into the conversation and stamps the window
// it covered. Through handleRequestAsync all the same, like `assistant.decide`
// and for the same reason — no model runs, but naming the subjects does: every
// open proposal and every line in every group is resolved through
// `Directory.SessionBrief`, which is a session enrich plus a project list
// apiece, so one digest is hundreds of queries. On the dispatch loop that
// would sit in front of every later mutation on this socket, a session stop
// and an approval answer included. It has no ordering contract worth holding:
// nothing a client sends next reads the digest, and the stamp it writes is
// only the window the next one measures from.
func (c *conn) handleAssistantDigest(msg ClientMessage) {
	handleRequestAsync(c, msg, func(ctx context.Context, _ AssistantDigestPayload) (assistant.Message, error) {
		if c.assistantSvc == nil {
			return assistant.Message{}, errAssistantDisabled
		}
		return c.assistantSvc.Digest(ctx)
	})
}

// handleAssistantCompact folds the journal's older days and answers the report.
//
// Through handleRequestAsync, and here the reason is the strongest of the three
// that use it: a pass is bounded at five minutes and spends a model call per day
// it folds, so on the dispatch loop it would hold this connection's whole
// mutation lane — a session stop, an approval answer — behind a background tidy
// nobody is waiting on. It has no ordering contract worth holding either: what it
// deletes is a fortnight old, and nothing a client sends next reads it.
//
// It answers the report rather than a bare ok, because the two numbers that
// matter are not guessable from outside: how many days folded, and whether
// anything was folded at all (a machine with no summariser folds nothing, on
// purpose, and says so in `note`).
func (c *conn) handleAssistantCompact(msg ClientMessage) {
	handleRequestAsync(c, msg, func(ctx context.Context, _ AssistantCompactPayload) (assistant.CompactReport, error) {
		if c.assistantSvc == nil {
			return assistant.CompactReport{}, errAssistantDisabled
		}
		return c.assistantSvc.Compact(ctx)
	})
}

// handleAssistantPolicies lists the standing instructions, enabled or not.
//
// A pure read on the read lane: the policies page renders from it, and the
// heartbeat reads the same rows for itself rather than through a client.
func (c *conn) handleAssistantPolicies(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, _ AssistantPoliciesPayload) (AssistantPoliciesResult, error) {
		if c.assistantSvc == nil {
			return AssistantPoliciesResult{}, errAssistantDisabled
		}
		policies, err := c.assistantSvc.Policies(ctx)
		if err != nil {
			return AssistantPoliciesResult{}, err
		}
		return AssistantPoliciesResult{Policies: policies}, nil
	})
}

// handleAssistantPolicySave writes one, new or edited.
//
// On the serial MUTATION lane, and not because it is slow: two saves of the same
// policy have to land in the order they were sent, or the row ends up holding
// the older edit. It answers the stored row, so the form renders what was
// written rather than what it sent — the id of a new one, and the budgets the
// server defaulted.
func (c *conn) handleAssistantPolicySave(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p AssistantPolicySavePayload) (assistant.Policy, error) {
		if c.assistantSvc == nil {
			return assistant.Policy{}, errAssistantDisabled
		}
		return c.assistantSvc.SavePolicy(ctx, assistant.Policy{
			ID:             p.ID,
			Name:           p.Name,
			Text:           p.Text,
			Enabled:        p.Enabled,
			BudgetInFlight: p.BudgetInFlight,
			BudgetPerDay:   p.BudgetPerDay,
		})
	})
}

// handleAssistantPolicyDelete removes one. Also a mutation, and idempotent:
// deleting one that is already gone is the state the caller asked for.
func (c *conn) handleAssistantPolicyDelete(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p AssistantPolicyDeletePayload) (struct{}, error) {
		if c.assistantSvc == nil {
			return struct{}{}, errAssistantDisabled
		}
		return struct{}{}, c.assistantSvc.DeletePolicy(ctx, p.ID)
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

// AssistantProposalsResult wraps the proposals, on the same argument.
type AssistantProposalsResult struct {
	Proposals []assistant.Proposal `json:"proposals,omitempty"`
}
