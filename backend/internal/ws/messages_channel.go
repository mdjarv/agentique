package ws

import (
	"errors"

	"github.com/mdjarv/agentique/backend/internal/session"
)

// --- Channel payloads ---

type ChannelCreatePayload struct {
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
}

type ChannelDeletePayload struct {
	ChannelID string `json:"channelId"`
}

type ChannelDissolvePayload struct {
	ChannelID string `json:"channelId"`
}

type ChannelDissolveKeepPayload struct {
	ChannelID string `json:"channelId"`
}

type ChannelJoinPayload struct {
	SessionID string `json:"sessionId"`
	ChannelID string `json:"channelId"`
	Role      string `json:"role"`
}

type ChannelLeavePayload struct {
	SessionID string `json:"sessionId"`
	ChannelID string `json:"channelId"`
}

type ChannelListPayload struct {
	ProjectID string `json:"projectId"`
}

type ChannelInfoPayload struct {
	ChannelID string `json:"channelId"`
}

type ChannelTimelinePayload struct {
	ChannelID string `json:"channelId"`
}

type ChannelSendMessagePayload struct {
	SenderSessionID string `json:"senderSessionId"`
	TargetSessionID string `json:"targetSessionId"`
	Content         string `json:"content"`
}

type ChannelBroadcastPayload struct {
	ChannelID string `json:"channelId"`
	Content   string `json:"content"`
}

type ChannelCreateSwarmPayload struct {
	ProjectID     string                    `json:"projectId"`
	ChannelName   string                    `json:"channelName"`
	LeadSessionID string                    `json:"leadSessionId"`
	Members       []session.SwarmMemberSpec `json:"members"`
}

// --- Channel validation errors ---

var (
	errChannelIDRequired                = errors.New("channelId is required")
	errChannelSessionAndChannelRequired = errors.New("sessionId and channelId are required")
	errChannelSendMessageRequired       = errors.New("senderSessionId, targetSessionId, and content are required")
	errChannelBroadcastRequired         = errors.New("channelId and content are required")
	errChannelNameRequired              = errors.New("channel name is required")
)

// validateChannelID checks presence and UUID format.
func validateChannelID(id string) error {
	if id == "" {
		return errChannelIDRequired
	}
	return validateUUID("channelId", id)
}

// --- Channel Validate methods ---

func (p *ChannelCreatePayload) Validate() error {
	if err := validateProjectID(p.ProjectID); err != nil {
		return err
	}
	name := trimSpace(p.Name)
	if name == "" {
		return errChannelNameRequired
	}
	return validateMaxLen("name", name, maxNameLen)
}

func (p *ChannelDeletePayload) Validate() error {
	return validateChannelID(p.ChannelID)
}

func (p *ChannelDissolvePayload) Validate() error {
	return validateChannelID(p.ChannelID)
}

func (p *ChannelDissolveKeepPayload) Validate() error {
	return validateChannelID(p.ChannelID)
}

func (p *ChannelJoinPayload) Validate() error {
	if p.ChannelID == "" {
		return errChannelSessionAndChannelRequired
	}
	if err := validateSessionID(p.SessionID); err != nil {
		return err
	}
	return validateChannelID(p.ChannelID)
}

func (p *ChannelLeavePayload) Validate() error {
	if p.ChannelID == "" {
		return errChannelSessionAndChannelRequired
	}
	if err := validateSessionID(p.SessionID); err != nil {
		return err
	}
	return validateChannelID(p.ChannelID)
}

func (p *ChannelListPayload) Validate() error {
	return validateProjectID(p.ProjectID)
}

func (p *ChannelInfoPayload) Validate() error {
	return validateChannelID(p.ChannelID)
}

func (p *ChannelTimelinePayload) Validate() error {
	return validateChannelID(p.ChannelID)
}

func (p *ChannelSendMessagePayload) Validate() error {
	if p.SenderSessionID == "" || p.TargetSessionID == "" || p.Content == "" {
		return errChannelSendMessageRequired
	}
	if err := validateUUID("senderSessionId", p.SenderSessionID); err != nil {
		return err
	}
	if err := validateUUID("targetSessionId", p.TargetSessionID); err != nil {
		return err
	}
	return validateMaxLen("content", p.Content, maxContentLen)
}

func (p *ChannelBroadcastPayload) Validate() error {
	if p.Content == "" {
		return errChannelBroadcastRequired
	}
	if err := validateChannelID(p.ChannelID); err != nil {
		return err
	}
	return validateMaxLen("content", p.Content, maxContentLen)
}

func (p *ChannelCreateSwarmPayload) Validate() error {
	if err := validateProjectID(p.ProjectID); err != nil {
		return err
	}
	if len(p.Members) == 0 {
		return errors.New("at least one member is required")
	}
	if err := validateOptionalUUID("leadSessionId", p.LeadSessionID); err != nil {
		return err
	}
	return validateMaxLen("channelName", p.ChannelName, maxNameLen)
}
