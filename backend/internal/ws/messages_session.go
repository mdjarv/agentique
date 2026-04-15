package ws

import (
	"errors"
	"fmt"

	"github.com/allbin/agentique/backend/internal/session"
)

// --- Session payloads ---

type SessionCreatePayload struct {
	ProjectID       string                 `json:"projectId"`
	Name            string                 `json:"name"`
	Worktree        bool                   `json:"worktree"`
	Branch          string                 `json:"branch"`
	Model           string                 `json:"model"`
	PlanMode        bool                   `json:"planMode"`
	AutoApproveMode string                 `json:"autoApproveMode"`
	Effort          string                 `json:"effort"`
	MaxBudget       float64                `json:"maxBudget"`
	MaxTurns        int                    `json:"maxTurns"`
	BehaviorPresets session.BehaviorPresets `json:"behaviorPresets"`
	AgentProfileID  string                 `json:"agentProfileId"`
	IdempotencyKey  string                 `json:"idempotencyKey,omitempty"`
}

type SessionQueryPayload struct {
	SessionID   string                    `json:"sessionId"`
	Prompt      string                    `json:"prompt"`
	Attachments []session.QueryAttachment `json:"attachments,omitempty"`
}

type SessionListPayload struct {
	ProjectID string `json:"projectId"`
}

type SessionStopPayload struct {
	SessionID string `json:"sessionId"`
}

type SessionResumePayload struct {
	SessionID string `json:"sessionId"`
}

type SessionResetConversationPayload struct {
	SessionID string `json:"sessionId"`
}

type SessionHistoryPayload struct {
	SessionID string `json:"sessionId"`
	Limit     int    `json:"limit,omitempty"`
}

type SessionDiffPayload struct {
	SessionID string `json:"sessionId"`
}

type SessionInterruptPayload struct {
	SessionID string `json:"sessionId"`
}

type SessionMergePayload struct {
	SessionID string `json:"sessionId"`
	Mode      string `json:"mode"` // "merge" | "complete" | "delete"
}

type SessionCreatePRPayload struct {
	SessionID string `json:"sessionId"`
	Title     string `json:"title"`
	Body      string `json:"body"`
}

type SessionDeletePayload struct {
	SessionID string `json:"sessionId"`
}

type SessionDeleteBulkPayload struct {
	SessionIDs []string `json:"sessionIds"`
}

type SessionDeleteBulkResultItem struct {
	SessionID string `json:"sessionId"`
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
}

type SessionDeleteBulkResult struct {
	Results []SessionDeleteBulkResultItem `json:"results"`
}

type SessionSetModelPayload struct {
	SessionID string `json:"sessionId"`
	Model     string `json:"model"`
}

type SessionSetPermissionPayload struct {
	SessionID string `json:"sessionId"`
	Mode      string `json:"mode"`
}

type SessionResolveApprovalPayload struct {
	SessionID  string `json:"sessionId"`
	ApprovalID string `json:"approvalId"`
	Allow      bool   `json:"allow"`
	Message    string `json:"message"`
}

type SessionSetAutoApprovePayload struct {
	SessionID string `json:"sessionId"`
	Mode      string `json:"mode"`
}

type SessionResolveQuestionPayload struct {
	SessionID  string            `json:"sessionId"`
	QuestionID string            `json:"questionId"`
	Answers    map[string]string `json:"answers"`
}

type SessionRenamePayload struct {
	SessionID string `json:"sessionId"`
	Name      string `json:"name"`
}

type SessionCommitPayload struct {
	SessionID string `json:"sessionId"`
	Message   string `json:"message"`
}

type SessionRebasePayload struct {
	SessionID string `json:"sessionId"`
}

type SessionGeneratePRDescPayload struct {
	SessionID string `json:"sessionId"`
}

type SessionGenerateCommitMsgPayload struct {
	SessionID string `json:"sessionId"`
}

type SessionMarkDonePayload struct {
	SessionID string `json:"sessionId"`
}

type SessionCleanPayload struct {
	SessionID string `json:"sessionId"`
}

type SessionUncommittedFilesPayload struct {
	SessionID string `json:"sessionId"`
}

type SessionUncommittedDiffPayload struct {
	SessionID string `json:"sessionId"`
}

type SessionRefreshGitPayload struct {
	SessionID string `json:"sessionId"`
}

type SessionCommitLogPayload struct {
	SessionID string `json:"sessionId"`
}

type SessionPRStatusPayload struct {
	SessionID string `json:"sessionId"`
}

// --- Session validation errors ---

var (
	errProjectIDRequired          = errors.New("projectId is required")
	errSessionIDRequired          = errors.New("sessionId is required")
	errSessionIDAndPromptRequired = errors.New("sessionId and prompt are required")
	errSessionIDsRequired         = errors.New("sessionIds is required")
	errSessionIDAndNameRequired   = errors.New("sessionId and name are required")
	errSessionIDAndModelRequired  = errors.New("sessionId and model are required")
	errSessionIDAndModeRequired   = errors.New("sessionId and mode are required")
	errApprovalIDRequired         = errors.New("sessionId and approvalId are required")
	errQuestionIDRequired         = errors.New("sessionId and questionId are required")
	errSessionIDAndMsgRequired    = errors.New("sessionId and message are required")
	errTooManyAttachments         = errors.New("too many attachments (max 50)")
)

// --- Session Validate methods ---

// validateSessionID checks presence and UUID format.
func validateSessionID(id string) error {
	if id == "" {
		return errSessionIDRequired
	}
	return validateUUID("sessionId", id)
}

// validateProjectID checks presence and UUID format.
func validateProjectID(id string) error {
	if id == "" {
		return errProjectIDRequired
	}
	return validateUUID("projectId", id)
}

func (p *SessionCreatePayload) Validate() error {
	if err := validateProjectID(p.ProjectID); err != nil {
		return err
	}
	if err := validateOptionalUUID("agentProfileId", p.AgentProfileID); err != nil {
		return err
	}
	if err := validateMaxLen("name", p.Name, maxNameLen); err != nil {
		return err
	}
	if err := validateBranchName(p.Branch); err != nil {
		return err
	}
	return nil
}

func (p *SessionQueryPayload) Validate() error {
	if p.Prompt == "" {
		return errSessionIDAndPromptRequired
	}
	if err := validateSessionID(p.SessionID); err != nil {
		return err
	}
	if err := validateMaxLen("prompt", p.Prompt, maxPromptLen); err != nil {
		return err
	}
	if len(p.Attachments) > maxAttachments {
		return errTooManyAttachments
	}
	return nil
}

func (p *SessionListPayload) Validate() error {
	return validateProjectID(p.ProjectID)
}

func (p *SessionStopPayload) Validate() error {
	return validateSessionID(p.SessionID)
}

func (p *SessionResumePayload) Validate() error {
	return validateSessionID(p.SessionID)
}

func (p *SessionResetConversationPayload) Validate() error {
	return validateSessionID(p.SessionID)
}

func (p *SessionHistoryPayload) Validate() error {
	return validateSessionID(p.SessionID)
}

func (p *SessionDiffPayload) Validate() error {
	return validateSessionID(p.SessionID)
}

func (p *SessionInterruptPayload) Validate() error {
	return validateSessionID(p.SessionID)
}

func (p *SessionMergePayload) Validate() error {
	return validateSessionID(p.SessionID)
}

func (p *SessionCreatePRPayload) Validate() error {
	if err := validateSessionID(p.SessionID); err != nil {
		return err
	}
	if err := validateMaxLen("title", p.Title, maxNameLen); err != nil {
		return err
	}
	return nil
}

func (p *SessionDeletePayload) Validate() error {
	return validateSessionID(p.SessionID)
}

func (p *SessionDeleteBulkPayload) Validate() error {
	if len(p.SessionIDs) == 0 {
		return errSessionIDsRequired
	}
	if len(p.SessionIDs) > maxBulkDeleteIDs {
		return fmt.Errorf("too many sessionIds (max %d)", maxBulkDeleteIDs)
	}
	for _, id := range p.SessionIDs {
		if err := validateUUID("sessionIds[]", id); err != nil {
			return err
		}
	}
	return nil
}

func (p *SessionRenamePayload) Validate() error {
	if p.Name == "" {
		return errSessionIDAndNameRequired
	}
	if err := validateSessionID(p.SessionID); err != nil {
		return err
	}
	return validateMaxLen("name", p.Name, maxNameLen)
}

func (p *SessionSetModelPayload) Validate() error {
	if p.Model == "" {
		return errSessionIDAndModelRequired
	}
	return validateSessionID(p.SessionID)
}

func (p *SessionSetPermissionPayload) Validate() error {
	if p.Mode == "" {
		return errSessionIDAndModeRequired
	}
	return validateSessionID(p.SessionID)
}

func (p *SessionSetAutoApprovePayload) Validate() error {
	if p.Mode == "" {
		return errSessionIDAndModeRequired
	}
	return validateSessionID(p.SessionID)
}

func (p *SessionResolveApprovalPayload) Validate() error {
	if p.ApprovalID == "" {
		return errApprovalIDRequired
	}
	return validateSessionID(p.SessionID)
}

func (p *SessionResolveQuestionPayload) Validate() error {
	if p.QuestionID == "" {
		return errQuestionIDRequired
	}
	return validateSessionID(p.SessionID)
}

func (p *SessionCommitPayload) Validate() error {
	if p.Message == "" {
		return errSessionIDAndMsgRequired
	}
	if err := validateSessionID(p.SessionID); err != nil {
		return err
	}
	return validateMaxLen("message", p.Message, maxCommitMsgLen)
}

func (p *SessionRebasePayload) Validate() error {
	return validateSessionID(p.SessionID)
}

func (p *SessionGeneratePRDescPayload) Validate() error {
	return validateSessionID(p.SessionID)
}

func (p *SessionGenerateCommitMsgPayload) Validate() error {
	return validateSessionID(p.SessionID)
}

func (p *SessionMarkDonePayload) Validate() error {
	return validateSessionID(p.SessionID)
}

func (p *SessionCleanPayload) Validate() error {
	return validateSessionID(p.SessionID)
}

func (p *SessionUncommittedFilesPayload) Validate() error {
	return validateSessionID(p.SessionID)
}

func (p *SessionUncommittedDiffPayload) Validate() error {
	return validateSessionID(p.SessionID)
}

func (p *SessionRefreshGitPayload) Validate() error {
	return validateSessionID(p.SessionID)
}

func (p *SessionCommitLogPayload) Validate() error {
	return validateSessionID(p.SessionID)
}

func (p *SessionPRStatusPayload) Validate() error {
	return validateSessionID(p.SessionID)
}
