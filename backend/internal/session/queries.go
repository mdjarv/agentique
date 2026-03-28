package session

import (
	"context"
	"database/sql"

	"github.com/allbin/agentique/backend/internal/store"
)

type managerQueries interface {
	sessionQueries
	CreateSession(ctx context.Context, arg store.CreateSessionParams) (store.Session, error)
	MaxTurnIndex(ctx context.Context, sessionID string) (int64, error)
	ListSessionsByProject(ctx context.Context, projectID string) ([]store.Session, error)
	ListAllSessions(ctx context.Context) ([]store.Session, error)
	RecoverStaleSessions(ctx context.Context) error
}

type serviceQueries interface {
	GetProject(ctx context.Context, id string) (store.Project, error)
	GetSession(ctx context.Context, id string) (store.Session, error)
	ListProjects(ctx context.Context) ([]store.Project, error)
	SessionSummariesByProject(ctx context.Context, projectID string) ([]store.SessionSummariesByProjectRow, error)
	AllSessionSummaries(ctx context.Context) ([]store.AllSessionSummariesRow, error)
	DeleteSession(ctx context.Context, id string) error
	UpdateSessionModel(ctx context.Context, arg store.UpdateSessionModelParams) error
	UpdateSessionPermissionMode(ctx context.Context, arg store.UpdateSessionPermissionModeParams) error
	UpdateSessionAutoApprove(ctx context.Context, arg store.UpdateSessionAutoApproveParams) error
	UpdateSessionState(ctx context.Context, arg store.UpdateSessionStateParams) error
	UpdateSessionName(ctx context.Context, arg store.UpdateSessionNameParams) error
	UpdateSessionLastQueryAt(ctx context.Context, id string) error
	ListEventsBySession(ctx context.Context, sessionID string) ([]store.SessionEvent, error)
	SetSessionCompleted(ctx context.Context, id string) error
	InsertEvent(ctx context.Context, arg store.InsertEventParams) error

	// Team queries
	CreateTeam(ctx context.Context, arg store.CreateTeamParams) (store.Team, error)
	GetTeam(ctx context.Context, id string) (store.Team, error)
	ListTeamsByProject(ctx context.Context, projectID string) ([]store.Team, error)
	DeleteTeam(ctx context.Context, id string) error
	UpdateTeamName(ctx context.Context, arg store.UpdateTeamNameParams) error
	ListTeamMembers(ctx context.Context, teamID sql.NullString) ([]store.Session, error)
	SetSessionTeam(ctx context.Context, arg store.SetSessionTeamParams) error
	ClearSessionTeam(ctx context.Context, id string) error
	ListAgentMessagesByTeam(ctx context.Context, teamID sql.NullString) ([]store.SessionEvent, error)
}

type gitServiceQueries interface {
	GetSession(ctx context.Context, id string) (store.Session, error)
	GetProject(ctx context.Context, id string) (store.Project, error)
	SetWorktreeMerged(ctx context.Context, id string) error
	SetSessionCompleted(ctx context.Context, id string) error
	UpdateSessionState(ctx context.Context, arg store.UpdateSessionStateParams) error
	UpdateWorktreeBaseSHA(ctx context.Context, arg store.UpdateWorktreeBaseSHAParams) error
	UpdateSessionPRUrl(ctx context.Context, arg store.UpdateSessionPRUrlParams) error
	ListSessionsByProject(ctx context.Context, projectID string) ([]store.Session, error)
}

type historyQueries interface {
	ListEventsBySession(ctx context.Context, sessionID string) ([]store.SessionEvent, error)
}

type sessionQueries interface {
	UpdateSessionState(ctx context.Context, arg store.UpdateSessionStateParams) error
	InsertEvent(ctx context.Context, arg store.InsertEventParams) error
	UpdateClaudeSessionID(ctx context.Context, arg store.UpdateClaudeSessionIDParams) error
	UpdateSessionPermissionMode(ctx context.Context, arg store.UpdateSessionPermissionModeParams) error
	GetSession(ctx context.Context, id string) (store.Session, error)
	GetProject(ctx context.Context, id string) (store.Project, error)
	SetSessionCompleted(ctx context.Context, id string) error
	UnsetSessionCompleted(ctx context.Context, id string) error
}
