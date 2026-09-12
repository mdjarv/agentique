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

	// The journal: append-only, plus the one update that stamps seen_by --
	// through a boundary rather than row by row, so a look means "caught up to
	// here" and a backlog bigger than one page is not re-delivered as news.
	InsertAssistantJournalEntry(ctx context.Context, arg store.InsertAssistantJournalEntryParams) (store.AssistantJournal, error)
	ListAssistantJournalSince(ctx context.Context, arg store.ListAssistantJournalSinceParams) ([]store.AssistantJournal, error)
	ListAssistantJournalUnseen(ctx context.Context, arg store.ListAssistantJournalUnseenParams) ([]store.AssistantJournal, error)
	CountAssistantJournalUnseen(ctx context.Context, surface sql.NullString) (int64, error)
	MarkAssistantJournalSeenThrough(ctx context.Context, arg store.MarkAssistantJournalSeenThroughParams) error

	// The one read of the sessions table this package makes, and it is a read
	// of two flags: the session.state observer's baseline, so that a snapshot
	// of a session archived last month is not news (see ObserveSessionState).
	ListSessionOutcomeBaseline(ctx context.Context) ([]store.ListSessionOutcomeBaselineRow, error)

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
