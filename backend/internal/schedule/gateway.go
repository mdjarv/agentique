package schedule

import (
	"context"

	"github.com/mdjarv/agentique/backend/internal/session"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// sessionGateway adapts session.Service to the scheduler's Gateway.
type sessionGateway struct {
	svc *session.Service
	q   *store.Queries
}

// NewSessionGateway wires the scheduler to the live session service.
func NewSessionGateway(svc *session.Service, q *store.Queries) Gateway {
	return &sessionGateway{svc: svc, q: q}
}

func (g *sessionGateway) Deliver(ctx context.Context, sessionID, prompt string, origin session.QueryOrigin) (int, <-chan session.TurnOutcome, error) {
	return g.svc.QuerySessionWithOutcome(ctx, sessionID, prompt, nil, origin)
}

func (g *sessionGateway) SessionFinished(ctx context.Context, sessionID string) (bool, error) {
	row, err := g.q.GetSession(ctx, sessionID)
	if err != nil {
		return false, err
	}
	completed := row.CompletedAt.Valid && row.CompletedAt.String != ""
	return completed || row.WorktreeMerged != 0, nil
}

func (g *sessionGateway) PendingHumanInput(sessionID string) string {
	return g.svc.PendingHumanInput(sessionID)
}
