package session

import (
	"context"
	"database/sql"

	"github.com/mdjarv/agentique/backend/internal/store"
)

// ---------------------------------------------------------------------------
// Base query interfaces — compose these into consumer-specific interfaces.
// ---------------------------------------------------------------------------

// sessionReader provides read access to sessions and their parent projects.
type sessionReader interface {
	GetSession(ctx context.Context, id string) (store.Session, error)
	GetProject(ctx context.Context, id string) (store.Project, error)
}

// sessionWriter provides write access for core session lifecycle operations.
type sessionWriter interface {
	UpdateSessionState(ctx context.Context, arg store.UpdateSessionStateParams) error
	InsertEvent(ctx context.Context, arg store.InsertEventParams) error
	UpdateClaudeSessionID(ctx context.Context, arg store.UpdateClaudeSessionIDParams) error
	UpdateSessionPermissionMode(ctx context.Context, arg store.UpdateSessionPermissionModeParams) error
	SetSessionCompleted(ctx context.Context, id string) error
}

// ---------------------------------------------------------------------------
// Consumer-specific interfaces — each embeds the base interfaces it needs
// plus any additional methods specific to that consumer.
// ---------------------------------------------------------------------------

// sessionQueries is used by Session (session.go, state.go).
type sessionQueries interface {
	sessionReader
	sessionWriter
}

// managerQueries is used by Manager (manager.go).
// Embeds sessionQueries because Manager passes its queries to newSession.
type managerQueries interface {
	sessionQueries
	CreateSession(ctx context.Context, arg store.CreateSessionParams) (store.Session, error)
	MaxTurnIndex(ctx context.Context, sessionID string) (int64, error)
	ListSessionsByProject(ctx context.Context, projectID string) ([]store.Session, error)
	ListAllSessions(ctx context.Context) ([]store.Session, error)
	RecoverStaleSessions(ctx context.Context) error
}

// serviceQueries is used by Service (service.go, channel.go, helpers.go).
type serviceQueries interface {
	sessionReader
	sessionWriter

	ListProjects(ctx context.Context) ([]store.Project, error)
	SessionSummariesByProject(ctx context.Context, projectID string) ([]store.SessionSummariesByProjectRow, error)
	AllSessionSummaries(ctx context.Context) ([]store.AllSessionSummariesRow, error)
	DeleteSession(ctx context.Context, id string) error
	UpdateSessionModel(ctx context.Context, arg store.UpdateSessionModelParams) error
	UpdateSessionAutoApproveMode(ctx context.Context, arg store.UpdateSessionAutoApproveModeParams) error
	UpdateSessionName(ctx context.Context, arg store.UpdateSessionNameParams) error
	UpdateSessionLastQueryAt(ctx context.Context, id string) error
	CountActiveSessionsByProject(ctx context.Context, projectID string) (int64, error)
	ListEventsBySession(ctx context.Context, sessionID string) ([]store.SessionEvent, error)
	ListRecentEventsBySession(ctx context.Context, arg store.ListRecentEventsBySessionParams) ([]store.SessionEvent, error)
	CountTurnsBySession(ctx context.Context, sessionID string) (int64, error)
	UpdateSessionWorktree(ctx context.Context, arg store.UpdateSessionWorktreeParams) error
	ListChildSessions(ctx context.Context, parentID sql.NullString) ([]store.Session, error)

	// Agent profile / team queries (for preamble injection)
	GetAgentProfile(ctx context.Context, id string) (store.AgentProfile, error)
	ListTeamsForAgent(ctx context.Context, agentProfileID string) ([]store.Team, error)
	ListTeamMembers(ctx context.Context, teamID string) ([]store.AgentProfile, error)

	// Channel queries
	CreateChannel(ctx context.Context, arg store.CreateChannelParams) (store.Channel, error)
	GetChannel(ctx context.Context, id string) (store.Channel, error)
	ListChannelsByProject(ctx context.Context, projectID string) ([]store.Channel, error)
	DeleteChannel(ctx context.Context, id string) error
	UpdateChannelName(ctx context.Context, arg store.UpdateChannelNameParams) error
	AddChannelMember(ctx context.Context, arg store.AddChannelMemberParams) error
	RemoveChannelMember(ctx context.Context, arg store.RemoveChannelMemberParams) error
	RemoveSessionFromAllChannels(ctx context.Context, sessionID string) error
	ListChannelMemberSessions(ctx context.Context, channelID string) ([]store.ListChannelMemberSessionsRow, error)
	ListSessionChannels(ctx context.Context, sessionID string) ([]store.ListSessionChannelsRow, error)
	// Unified message queries
	InsertMessage(ctx context.Context, arg store.InsertMessageParams) (store.Message, error)
	GetMessage(ctx context.Context, id string) (store.Message, error)
	ListMessagesByChannel(ctx context.Context, channelID string) ([]store.Message, error)
	DeleteMessagesByChannel(ctx context.Context, channelID string) error
	CountSessionIntroductionsInChannel(ctx context.Context, arg store.CountSessionIntroductionsInChannelParams) (int64, error)
	InsertEventWithMessageID(ctx context.Context, arg store.InsertEventWithMessageIDParams) error

	// Delivery queries
	InsertMessageDelivery(ctx context.Context, arg store.InsertMessageDeliveryParams) error
	UpdateDeliveryStatus(ctx context.Context, arg store.UpdateDeliveryStatusParams) error
	ListPendingDeliveriesForSession(ctx context.Context, recipientSessionID string) ([]store.ListPendingDeliveriesForSessionRow, error)
}

// gitServiceQueries is used by GitService (git_service.go).
type gitServiceQueries interface {
	sessionReader
	SetWorktreeMerged(ctx context.Context, id string) error
	SetSessionCompleted(ctx context.Context, id string) error
	UpdateSessionState(ctx context.Context, arg store.UpdateSessionStateParams) error
	UpdateWorktreeBaseSHA(ctx context.Context, arg store.UpdateWorktreeBaseSHAParams) error
	UpdateSessionPRUrl(ctx context.Context, arg store.UpdateSessionPRUrlParams) error
	ListSessionsByProject(ctx context.Context, projectID string) ([]store.Session, error)
}

// historyQueries is used by history replay (history.go).
type historyQueries interface {
	ListEventsBySession(ctx context.Context, sessionID string) ([]store.SessionEvent, error)
	ListRecentEventsBySession(ctx context.Context, arg store.ListRecentEventsBySessionParams) ([]store.SessionEvent, error)
	CountTurnsBySession(ctx context.Context, sessionID string) (int64, error)
}
