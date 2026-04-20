package ws

import "errors"

// --- Agent Profile payloads ---

type AgentProfileCreatePayload struct {
	Name        string `json:"name"`
	Role        string `json:"role"`
	Description string `json:"description"`
	ProjectID   string `json:"projectId"`
	Avatar      string `json:"avatar"`
	Config      string `json:"config"`
}

type AgentProfileUpdatePayload struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Role        string `json:"role"`
	Description string `json:"description"`
	ProjectID   string `json:"projectId"`
	Avatar      string `json:"avatar"`
	Config      string `json:"config"`
}

type AgentProfileDeletePayload struct {
	ID string `json:"id"`
}

// --- Team payloads ---

type TeamCreatePayload struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type TeamUpdatePayload struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type TeamDeletePayload struct {
	ID string `json:"id"`
}

type TeamAddMemberPayload struct {
	TeamID         string `json:"teamId"`
	AgentProfileID string `json:"agentProfileId"`
	SortOrder      int    `json:"sortOrder"`
}

type TeamRemoveMemberPayload struct {
	TeamID         string `json:"teamId"`
	AgentProfileID string `json:"agentProfileId"`
}

// --- Persona payloads ---

type PersonaQueryPayload struct {
	ProfileID string `json:"profileId"`
	TeamID    string `json:"teamId"`
	Question  string `json:"question"`
}

type PersonaListPayload struct {
	TeamID string `json:"teamId"`
	Limit  int64  `json:"limit"`
	Offset int64  `json:"offset"`
}

type ProfileGeneratePayload struct {
	ProjectID   string `json:"projectId"`
	Brief       string `json:"brief"`
	Name        string `json:"name,omitempty"`
	Role        string `json:"role,omitempty"`
	Description string `json:"description,omitempty"`
	Avatar      string `json:"avatar,omitempty"`
}

// --- Team validation errors ---

var (
	errProfileIDRequired     = errors.New("id is required")
	errProfileNameRequired   = errors.New("name is required")
	errTeamIDRequired        = errors.New("id is required")
	errTeamNameRequired      = errors.New("name is required")
	errTeamMemberIDsRequired = errors.New("teamId and agentProfileId are required")
	errPersonaQueryRequired  = errors.New("profileId, teamId and question are required")
	errPersonaTeamIDRequired = errors.New("teamId is required")
)

// --- Agent Profile / Team / Persona Validate methods ---

func (p *AgentProfileCreatePayload) Validate() error {
	if p.Name == "" {
		return errProfileNameRequired
	}
	if err := validateMaxLen("name", p.Name, maxNameLen); err != nil {
		return err
	}
	if err := validateOptionalUUID("projectId", p.ProjectID); err != nil {
		return err
	}
	return nil
}

func (p *AgentProfileUpdatePayload) Validate() error {
	if p.ID == "" {
		return errProfileIDRequired
	}
	if err := validateUUID("id", p.ID); err != nil {
		return err
	}
	if p.Name == "" {
		return errProfileNameRequired
	}
	if err := validateMaxLen("name", p.Name, maxNameLen); err != nil {
		return err
	}
	if err := validateOptionalUUID("projectId", p.ProjectID); err != nil {
		return err
	}
	return nil
}

func (p *AgentProfileDeletePayload) Validate() error {
	if p.ID == "" {
		return errProfileIDRequired
	}
	return validateUUID("id", p.ID)
}

func (p *TeamCreatePayload) Validate() error {
	if p.Name == "" {
		return errTeamNameRequired
	}
	return validateMaxLen("name", p.Name, maxNameLen)
}

func (p *TeamUpdatePayload) Validate() error {
	if p.ID == "" {
		return errTeamIDRequired
	}
	if err := validateUUID("id", p.ID); err != nil {
		return err
	}
	if p.Name == "" {
		return errTeamNameRequired
	}
	return validateMaxLen("name", p.Name, maxNameLen)
}

func (p *TeamDeletePayload) Validate() error {
	if p.ID == "" {
		return errTeamIDRequired
	}
	return validateUUID("id", p.ID)
}

func (p *TeamAddMemberPayload) Validate() error {
	if p.TeamID == "" || p.AgentProfileID == "" {
		return errTeamMemberIDsRequired
	}
	if err := validateUUID("teamId", p.TeamID); err != nil {
		return err
	}
	return validateUUID("agentProfileId", p.AgentProfileID)
}

func (p *TeamRemoveMemberPayload) Validate() error {
	if p.TeamID == "" || p.AgentProfileID == "" {
		return errTeamMemberIDsRequired
	}
	if err := validateUUID("teamId", p.TeamID); err != nil {
		return err
	}
	return validateUUID("agentProfileId", p.AgentProfileID)
}

func (p *PersonaQueryPayload) Validate() error {
	if p.ProfileID == "" || p.TeamID == "" || p.Question == "" {
		return errPersonaQueryRequired
	}
	if err := validateUUID("profileId", p.ProfileID); err != nil {
		return err
	}
	if err := validateUUID("teamId", p.TeamID); err != nil {
		return err
	}
	return validateMaxLen("question", p.Question, maxPromptLen)
}

func (p *PersonaListPayload) Validate() error {
	if p.TeamID == "" {
		return errPersonaTeamIDRequired
	}
	return validateUUID("teamId", p.TeamID)
}
