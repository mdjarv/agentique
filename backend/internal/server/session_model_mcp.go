package server

import (
	"context"

	"github.com/mdjarv/agentique/backend/internal/mcphttp"
	"github.com/mdjarv/agentique/backend/internal/session"
)

// sessionModelInspector lets the SessionModel MCP tool read session.Service's
// model report. The two report types are field-identical and stay separate
// because the session package imports mcphttp (Manager.SetMCPHTTP), so mcphttp
// cannot import the session package back. The translation lives here, at the
// wiring site, rather than in either package.
type sessionModelInspector struct {
	svc *session.Service
}

func (a sessionModelInspector) InspectSessionModel(ctx context.Context, sessionID string) (mcphttp.SessionModelReport, error) {
	rep, err := a.svc.InspectSessionModel(ctx, sessionID)
	if err != nil {
		return mcphttp.SessionModelReport{}, err
	}
	return mcphttp.SessionModelReport{
		SessionID:       rep.SessionID,
		Provider:        rep.Provider,
		RequestedSlug:   rep.RequestedSlug,
		ResolvedModelID: rep.ResolvedModelID,
		ResolvedAt:      rep.ResolvedAt,
		Source:          rep.Source,
	}, nil
}
