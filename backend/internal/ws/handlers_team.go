package ws

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/mdjarv/agentique/backend/internal/gitops"
	"github.com/mdjarv/agentique/backend/internal/persona"
	"github.com/mdjarv/agentique/backend/internal/team"
)

// requireTeams returns true if the teams feature is available. If not, it sends
// an error response and returns false so the caller can bail out early.
func (c *conn) requireTeams(msgID string) bool {
	if c.teamSvc == nil {
		c.respond(msgID, nil, "teams feature is not enabled")
		return false
	}
	return true
}

// requirePersona returns true if the persona feature is available. If not, it
// sends an error response and returns false so the caller can bail out early.
func (c *conn) requirePersona(msgID string) bool {
	if c.personaSvc == nil {
		c.respond(msgID, nil, "persona feature is not enabled")
		return false
	}
	return true
}

// --- Agent Profile handlers ---

func (c *conn) handleAgentProfileList(msg ClientMessage) {
	if !c.requireTeams(msg.ID) {
		return
	}
	handleRequest(c, msg, func(ctx context.Context, _ struct{}) ([]team.AgentProfileInfo, error) {
		return c.teamSvc.ListAgentProfiles(ctx)
	})
}

func (c *conn) handleAgentProfileCreate(msg ClientMessage) {
	if !c.requireTeams(msg.ID) {
		return
	}
	handleRequest(c, msg, func(ctx context.Context, p AgentProfileCreatePayload) (team.AgentProfileInfo, error) {
		return c.teamSvc.CreateAgentProfile(ctx, p.Name, p.Role, p.Description, p.ProjectID, p.Avatar, p.Config)
	})
}

func (c *conn) handleAgentProfileUpdate(msg ClientMessage) {
	if !c.requireTeams(msg.ID) {
		return
	}
	handleRequest(c, msg, func(ctx context.Context, p AgentProfileUpdatePayload) (team.AgentProfileInfo, error) {
		return c.teamSvc.UpdateAgentProfile(ctx, p.ID, p.Name, p.Role, p.Description, p.ProjectID, p.Avatar, p.Config)
	})
}

func (c *conn) handleAgentProfileDelete(msg ClientMessage) {
	if !c.requireTeams(msg.ID) {
		return
	}
	handleRequest(c, msg, func(ctx context.Context, p AgentProfileDeletePayload) (struct{}, error) {
		return struct{}{}, c.teamSvc.DeleteAgentProfile(ctx, p.ID)
	})
}

// --- Team handlers ---

func (c *conn) handleTeamList(msg ClientMessage) {
	if !c.requireTeams(msg.ID) {
		return
	}
	handleRequest(c, msg, func(ctx context.Context, _ struct{}) ([]team.TeamInfo, error) {
		return c.teamSvc.ListTeams(ctx)
	})
}

func (c *conn) handleTeamCreate(msg ClientMessage) {
	if !c.requireTeams(msg.ID) {
		return
	}
	handleRequest(c, msg, func(ctx context.Context, p TeamCreatePayload) (team.TeamInfo, error) {
		return c.teamSvc.CreateTeam(ctx, p.Name, p.Description)
	})
}

func (c *conn) handleTeamUpdate(msg ClientMessage) {
	if !c.requireTeams(msg.ID) {
		return
	}
	handleRequest(c, msg, func(ctx context.Context, p TeamUpdatePayload) (team.TeamInfo, error) {
		return c.teamSvc.UpdateTeam(ctx, p.ID, p.Name, p.Description)
	})
}

func (c *conn) handleTeamDelete(msg ClientMessage) {
	if !c.requireTeams(msg.ID) {
		return
	}
	handleRequest(c, msg, func(ctx context.Context, p TeamDeletePayload) (struct{}, error) {
		return struct{}{}, c.teamSvc.DeleteTeam(ctx, p.ID)
	})
}

func (c *conn) handleTeamAddMember(msg ClientMessage) {
	if !c.requireTeams(msg.ID) {
		return
	}
	handleRequest(c, msg, func(ctx context.Context, p TeamAddMemberPayload) (team.TeamInfo, error) {
		return c.teamSvc.AddTeamMember(ctx, p.TeamID, p.AgentProfileID, p.SortOrder)
	})
}

func (c *conn) handleTeamRemoveMember(msg ClientMessage) {
	if !c.requireTeams(msg.ID) {
		return
	}
	handleRequest(c, msg, func(ctx context.Context, p TeamRemoveMemberPayload) (team.TeamInfo, error) {
		return c.teamSvc.RemoveTeamMember(ctx, p.TeamID, p.AgentProfileID)
	})
}

// --- Persona handlers ---

func (c *conn) handlePersonaQuery(msg ClientMessage) {
	if !c.requirePersona(msg.ID) {
		return
	}
	handleRequest(c, msg, func(ctx context.Context, p PersonaQueryPayload) (persona.QueryResult, error) {
		return c.personaSvc.Query(ctx, persona.QueryInput{
			ProfileID: p.ProfileID,
			TeamID:    p.TeamID,
			AskerType: "user",
			Question:  p.Question,
		})
	})
}

func (c *conn) handlePersonaList(msg ClientMessage) {
	if !c.requirePersona(msg.ID) {
		return
	}
	handleRequest(c, msg, func(ctx context.Context, p PersonaListPayload) ([]persona.InteractionInfo, error) {
		limit := p.Limit
		if limit <= 0 {
			limit = 50
		}
		return c.personaSvc.ListInteractions(ctx, p.TeamID, limit, p.Offset)
	})
}

func (c *conn) handleProfileGenerate(msg ClientMessage) {
	if !c.requirePersona(msg.ID) {
		return
	}
	handleRequest(c, msg, func(ctx context.Context, p ProfileGeneratePayload) (persona.GenerateProfileResult, error) {
		if p.ProjectID == "" {
			return persona.GenerateProfileResult{}, fmt.Errorf("projectId is required")
		}
		proj, err := c.queries.GetProject(ctx, p.ProjectID)
		if err != nil {
			return persona.GenerateProfileResult{}, fmt.Errorf("project not found")
		}

		claudeMD := readClaudeMD(proj.Path)
		files, err := gitops.ListTrackedFiles(proj.Path)
		if err != nil {
			slog.Warn("failed to list tracked files for profile generation", "project_id", p.ProjectID, "error", err)
		}

		return c.personaSvc.GenerateProfile(ctx, persona.GenerateProfileInput{
			ProjectName: proj.Name,
			ClaudeMD:    claudeMD,
			FileTree:    files,
			Brief:       p.Brief,
		})
	})
}

func readClaudeMD(projectPath string) string {
	data, err := os.ReadFile(filepath.Join(projectPath, "CLAUDE.md"))
	if err != nil {
		return ""
	}
	return string(data)
}
