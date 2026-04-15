package ws

import (
	"context"

	"github.com/mdjarv/agentique/backend/internal/session"
)

func (c *conn) handleChannelCreate(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p ChannelCreatePayload) (session.ChannelInfo, error) {
		return c.svc.CreateChannel(ctx, p.ProjectID, p.Name)
	})
}

func (c *conn) handleChannelDelete(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p ChannelDeletePayload) (struct{}, error) {
		return struct{}{}, c.svc.DeleteChannel(ctx, p.ChannelID)
	})
}

func (c *conn) handleChannelDissolve(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p ChannelDissolvePayload) (struct{}, error) {
		return struct{}{}, c.svc.DissolveChannel(ctx, p.ChannelID)
	})
}

func (c *conn) handleChannelDissolveKeep(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p ChannelDissolveKeepPayload) (struct{}, error) {
		return struct{}{}, c.svc.DissolveChannelKeepHistory(ctx, p.ChannelID)
	})
}

func (c *conn) handleChannelJoin(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p ChannelJoinPayload) (session.ChannelInfo, error) {
		return c.svc.JoinChannel(ctx, p.SessionID, p.ChannelID, p.Role)
	})
}

func (c *conn) handleChannelLeave(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p ChannelLeavePayload) (struct{}, error) {
		return struct{}{}, c.svc.LeaveChannel(ctx, p.SessionID, p.ChannelID)
	})
}

func (c *conn) handleChannelList(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p ChannelListPayload) ([]session.ChannelInfo, error) {
		return c.svc.ListChannels(ctx, p.ProjectID)
	})
}

func (c *conn) handleChannelInfo(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p ChannelInfoPayload) (session.ChannelInfo, error) {
		return c.svc.GetChannelInfo(ctx, p.ChannelID)
	})
}

func (c *conn) handleChannelTimeline(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p ChannelTimelinePayload) ([]session.WireAgentMessageEvent, error) {
		return c.svc.GetChannelTimeline(ctx, p.ChannelID)
	})
}

func (c *conn) handleChannelSendMessage(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p ChannelSendMessagePayload) (struct{}, error) {
		return struct{}{}, c.svc.RouteAgentMessage(ctx, session.AgentMessagePayload{
			SenderSessionID: p.SenderSessionID,
			TargetSessionID: p.TargetSessionID,
			Content:         p.Content,
		})
	})
}

func (c *conn) handleChannelBroadcast(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p ChannelBroadcastPayload) (struct{}, error) {
		return struct{}{}, c.svc.BroadcastToChannel(ctx, p.ChannelID, p.Content)
	})
}

func (c *conn) handleChannelCreateSwarm(msg ClientMessage) {
	handleRequest(c, msg, func(ctx context.Context, p ChannelCreateSwarmPayload) (session.CreateSwarmResult, error) {
		return c.svc.CreateSwarm(ctx, session.CreateSwarmParams{
			ProjectID:     p.ProjectID,
			ChannelName:   p.ChannelName,
			LeadSessionID: p.LeadSessionID,
			Members:       p.Members,
		})
	})
}
