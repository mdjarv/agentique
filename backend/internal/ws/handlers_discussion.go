package ws

import (
	"context"

	"github.com/mdjarv/agentique/backend/internal/session"
)

func (c *conn) handleDiscussionStart(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p DiscussionStartPayload) (session.DiscussionInfo, error) {
		specs := make([]session.DiscussionPersonaSpec, len(p.Personas))
		for i, pp := range p.Personas {
			specs[i] = session.DiscussionPersonaSpec{
				AgentProfileID: pp.AgentProfileID,
				Name:           pp.Name,
				Model:          pp.Model,
				Effort:         pp.Effort,
				WriteAccess:    pp.WriteAccess,
				NoNamePrefix:   pp.NoNamePrefix,
			}
		}
		return c.svc.StartDiscussion(ctx, session.StartDiscussionParams{
			ProjectID:  p.ProjectID,
			GroupName:  p.GroupName,
			Mode:       session.DiscussionMode(p.Mode),
			Scope:      session.DiscussionScope(p.Scope),
			AutoCommit: p.AutoCommit,
			Personas:   specs,
			Prompt:     p.Prompt,
		})
	})
}

func (c *conn) handleDiscussionRound(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p DiscussionRoundPayload) (struct{}, error) {
		return struct{}{}, c.svc.SendDiscussionRound(ctx, p.ChannelID, p.Prompt)
	})
}

func (c *conn) handleDiscussionStop(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p DiscussionStopPayload) (struct{}, error) {
		return struct{}{}, c.svc.StopDiscussion(ctx, p.ChannelID)
	})
}
