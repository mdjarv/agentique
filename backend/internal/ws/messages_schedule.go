package ws

import "errors"

// --- Scheduled-loop payloads (docs/scheduled-loops.md) ---

type ScheduleCreatePayload struct {
	ProjectID string `json:"projectId"`
	SessionID string `json:"sessionId"`
	Name      string `json:"name"`
	Prompt    string `json:"prompt"`
	// Exactly one of Cron (recurring), At (one-shot, RFC3339), or Dynamic
	// (self-paced) is required.
	Cron      string `json:"cron"`
	At        string `json:"at"`
	Dynamic   bool   `json:"dynamic"`
	ExpiresAt string `json:"expiresAt"`
}

func (p *ScheduleCreatePayload) Validate() error {
	if p.SessionID == "" || p.ProjectID == "" {
		return errors.New("projectId and sessionId are required")
	}
	return nil
}

type ScheduleUpdatePayload struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Prompt    string `json:"prompt"`
	Cron      string `json:"cron"`
	ExpiresAt string `json:"expiresAt"`
}

func (p *ScheduleUpdatePayload) Validate() error {
	if p.ID == "" {
		return errors.New("id is required")
	}
	return nil
}

type ScheduleIDPayload struct {
	ID string `json:"id"`
}

func (p *ScheduleIDPayload) Validate() error {
	if p.ID == "" {
		return errors.New("id is required")
	}
	return nil
}

type ScheduleRunsPayload struct {
	ScheduleID string `json:"scheduleId"`
	Limit      int    `json:"limit"`
	Offset     int    `json:"offset"`
}

func (p *ScheduleRunsPayload) Validate() error {
	if p.ScheduleID == "" {
		return errors.New("scheduleId is required")
	}
	return nil
}
