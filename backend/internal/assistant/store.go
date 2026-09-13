package assistant

import (
	"context"
	"database/sql"

	"github.com/mdjarv/agentique/backend/internal/store"
)

// Store is the database, narrowed to what the assistant writes and reads.
//
// An interface rather than *store.Queries so a test can drive the service with
// a stub for one method, and so this file is the whole list of tables this
// package touches: the two channel rows that hold the conversation, the
// messages in it, and the three assistant tables. Anything else it wants is a
// question for a collaborator ([Directory], [Dispatcher]), not a query of its
// own — that is what stops the assistant growing a second reading of a fact
// the session pipeline already owns.
//
// It is satisfied by *store.Queries as generated.
type Store interface {
	// The conversation channel: created once, then found by kind if the state
	// row is ever lost.
	CreateAssistantChannel(ctx context.Context, arg store.CreateAssistantChannelParams) (store.Channel, error)
	GetAssistantChannel(ctx context.Context) (store.Channel, error)

	// The conversation itself. Written to the same messages table every channel
	// timeline uses, because it IS a channel timeline — through its own insert,
	// which passes created_at in rather than taking the column's default.
	InsertAssistantMessage(ctx context.Context, arg store.InsertAssistantMessageParams) (store.Message, error)
	ListAssistantMessagesBefore(ctx context.Context, arg store.ListAssistantMessagesBeforeParams) ([]store.Message, error)
	ListAssistantMessagesSince(ctx context.Context, arg store.ListAssistantMessagesSinceParams) ([]store.Message, error)

	// The single state row.
	GetAssistantState(ctx context.Context) (store.AssistantState, error)
	SetAssistantChannel(ctx context.Context, arg store.SetAssistantChannelParams) error
	SetAssistantSurfaceMark(ctx context.Context, arg store.SetAssistantSurfaceMarkParams) error
	SetAssistantDigestAt(ctx context.Context, arg store.SetAssistantDigestAtParams) error
	SetAssistantHeartbeatAt(ctx context.Context, arg store.SetAssistantHeartbeatAtParams) error

	// Standing instructions, and the heartbeat's gate. The gate is a COUNT
	// rather than a list on purpose: the common tick asks "has anything
	// happened" and nothing else, which is what makes a short interval
	// affordable.
	ListAssistantPolicies(ctx context.Context) ([]store.AssistantPolicy, error)
	UpsertAssistantPolicy(ctx context.Context, arg store.UpsertAssistantPolicyParams) (store.AssistantPolicy, error)
	DeleteAssistantPolicy(ctx context.Context, id string) error
	TouchAssistantPolicy(ctx context.Context, arg store.TouchAssistantPolicyParams) error
	CountAssistantJournalSince(ctx context.Context, since string) (int64, error)

	// The three reads behind a policy's budgets, and every one of them is a
	// COUNT: what the journal says this policy has created today, how many of
	// what it created is still unfinished, and — as the backstop a lost journal
	// write cannot widen — how many sessions the assistant has created at all
	// today. The per-policy attribution is the journal's, because only the
	// journal records which instruction an action was taken under; counting in
	// SQL is what keeps a budget check O(1) in a journal whose fourteen-day
	// window compaction deliberately does not reach.
	CountPolicySessionsCreatedSince(ctx context.Context, arg store.CountPolicySessionsCreatedSinceParams) (int64, error)
	CountLiveSessionsForPolicy(ctx context.Context, arg store.CountLiveSessionsForPolicyParams) (int64, error)
	CountSessionsByOriginSince(ctx context.Context, arg store.CountSessionsByOriginSinceParams) (int64, error)

	// The journal: append-only, plus the one update that stamps seen_by --
	// through a boundary rather than row by row, so a look means "caught up to
	// here" and a backlog bigger than one page is not re-delivered as news.
	InsertAssistantJournalEntry(ctx context.Context, arg store.InsertAssistantJournalEntryParams) (store.AssistantJournal, error)
	ListAssistantJournalSince(ctx context.Context, arg store.ListAssistantJournalSinceParams) ([]store.AssistantJournal, error)
	ListAssistantJournalUnseen(ctx context.Context, arg store.ListAssistantJournalUnseenParams) ([]store.AssistantJournal, error)
	CountAssistantJournalUnseen(ctx context.Context, surface sql.NullString) (int64, error)
	MarkAssistantJournalSeenThrough(ctx context.Context, arg store.MarkAssistantJournalSeenThroughParams) error

	// Compaction (M5), and the journal is the only table with a DELETE against
	// it. The reads all come before the two deletes in every pass: which days
	// still hold raw rows, one page of one day, the shape of the whole day the
	// payload keeps, and whether that day already has a summary -- which is what
	// tells a crashed pass's leftovers from a day nobody has folded. The mark
	// says whether today's pass has run.
	//
	// The two aggregates are over the WHOLE day where the page is bounded: the
	// prose is written from a day's newest rows and the delete takes all of
	// them, so anything the payload keeps has to be counted rather than
	// rendered.
	ListAssistantJournalRawDaysBefore(ctx context.Context, arg store.ListAssistantJournalRawDaysBeforeParams) ([]store.ListAssistantJournalRawDaysBeforeRow, error)
	ListAssistantJournalRawForDay(ctx context.Context, arg store.ListAssistantJournalRawForDayParams) ([]store.AssistantJournal, error)
	CountAssistantJournalRawKindsForDay(ctx context.Context, arg store.CountAssistantJournalRawKindsForDayParams) ([]store.CountAssistantJournalRawKindsForDayRow, error)
	ListAssistantJournalRawPolicyIDsForDay(ctx context.Context, arg store.ListAssistantJournalRawPolicyIDsForDayParams) ([]string, error)
	CountAssistantDaySummaries(ctx context.Context, arg store.CountAssistantDaySummariesParams) (int64, error)
	DeleteAssistantJournalRawForDay(ctx context.Context, arg store.DeleteAssistantJournalRawForDayParams) (int64, error)
	DeleteAssistantDaySummariesBefore(ctx context.Context, before string) (int64, error)
	SetAssistantCompactedAt(ctx context.Context, arg store.SetAssistantCompactedAtParams) error

	// The one read of the sessions table this package makes, and it is a read
	// of two flags: the session.state observer's baseline, so that a snapshot
	// of a session archived last month is not news (see ObserveSessionState).
	ListSessionOutcomeBaseline(ctx context.Context) ([]store.ListSessionOutcomeBaselineRow, error)

	// The proposals: the uncontained tier's card and the record of its yes or
	// no. Expiry is one statement rather than a row-by-row sweep; the decision
	// is guarded on status = 'open' in SQL so two surfaces cannot both decide
	// one proposal, and it answers the row count so the caller knows whether
	// the guard matched; and one open row per verb and target is a partial
	// unique index (migration 058), so the insert is what refuses a duplicate.
	InsertAssistantProposal(ctx context.Context, arg store.InsertAssistantProposalParams) (store.AssistantProposal, error)
	GetAssistantProposal(ctx context.Context, id string) (store.AssistantProposal, error)
	GetOpenAssistantProposalFor(ctx context.Context, arg store.GetOpenAssistantProposalForParams) (store.AssistantProposal, error)
	ListAssistantProposals(ctx context.Context, lim int64) ([]store.AssistantProposal, error)
	DecideAssistantProposal(ctx context.Context, arg store.DecideAssistantProposalParams) (int64, error)
	ExpireAssistantProposals(ctx context.Context, at string) error

	// The watch list.
	UpsertAssistantFollow(ctx context.Context, arg store.UpsertAssistantFollowParams) error
	SetAssistantFollowBriefed(ctx context.Context, arg store.SetAssistantFollowBriefedParams) error
	DeleteAssistantFollow(ctx context.Context, sessionID string) error
	ListAssistantFollows(ctx context.Context) ([]store.AssistantFollow, error)
}

// The generated queries are the implementation, asserted here so a query
// renamed by `just sqlc` is a compile error in this file rather than a wiring
// error in the server.
var _ Store = (*store.Queries)(nil)
