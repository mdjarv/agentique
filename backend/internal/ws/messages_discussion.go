package ws

import "errors"

// DiscussionPersonaPayload is one participant in a discussion-start request.
type DiscussionPersonaPayload struct {
	AgentProfileID string `json:"agentProfileId"`
	Name           string `json:"name"`
	Model          string `json:"model"`
	Effort         string `json:"effort"`
	WriteAccess    bool   `json:"writeAccess"`
	NoNamePrefix   bool   `json:"noNamePrefix"`
}

// DiscussionStartPayload starts a new discussion group.
type DiscussionStartPayload struct {
	ProjectID  string                     `json:"projectId"`
	GroupName  string                     `json:"groupName"`
	Mode       string                     `json:"mode"`  // "round-robin" | "parallel"
	Scope      string                     `json:"scope"` // "web-only" | "repo-backed"
	AutoCommit bool                       `json:"autoCommit"`
	Personas   []DiscussionPersonaPayload `json:"personas"`
	Prompt     string                     `json:"prompt"`
}

// DiscussionRoundPayload drives another round in a running discussion.
type DiscussionRoundPayload struct {
	ChannelID string `json:"channelId"`
	Prompt    string `json:"prompt"`
}

// DiscussionStopPayload stops a running discussion (keeps the transcript).
type DiscussionStopPayload struct {
	ChannelID string `json:"channelId"`
}

var (
	errDiscussionPersonas = errors.New("a discussion needs 2–8 personas")
	errDiscussionPrompt   = errors.New("prompt is required")
)

func (p *DiscussionStartPayload) Validate() error {
	if err := validateProjectID(p.ProjectID); err != nil {
		return err
	}
	if len(p.Personas) < 2 || len(p.Personas) > 8 {
		return errDiscussionPersonas
	}
	if trimSpace(p.Prompt) == "" {
		return errDiscussionPrompt
	}
	if err := validateMaxLen("groupName", p.GroupName, maxNameLen); err != nil {
		return err
	}
	return validateMaxLen("prompt", p.Prompt, maxContentLen)
}

func (p *DiscussionRoundPayload) Validate() error {
	if trimSpace(p.Prompt) == "" {
		return errDiscussionPrompt
	}
	if err := validateChannelID(p.ChannelID); err != nil {
		return err
	}
	return validateMaxLen("prompt", p.Prompt, maxContentLen)
}

func (p *DiscussionStopPayload) Validate() error {
	return validateChannelID(p.ChannelID)
}
