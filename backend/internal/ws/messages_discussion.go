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
	errDiscussionScope    = errors.New("scope must be \"web-only\" or \"repo-backed\"")
	errDiscussionMode     = errors.New("mode must be \"round-robin\" or \"parallel\"")
)

func (p *DiscussionStartPayload) Validate() error {
	// projectId is required only for repo-backed discussions (they need a project
	// path for the shared worktree). Web-only discussions are project-less; an
	// empty projectId is valid, a present one must still be a UUID. Empty scope
	// defaults to web-only (see Service.StartDiscussion).
	switch p.Scope {
	case "", "web-only":
		if p.ProjectID != "" {
			if err := validateUUID("projectId", p.ProjectID); err != nil {
				return err
			}
		}
	case "repo-backed":
		if err := validateProjectID(p.ProjectID); err != nil {
			return err
		}
	default:
		return errDiscussionScope
	}
	switch p.Mode {
	case "", "round-robin", "parallel":
	default:
		return errDiscussionMode
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
