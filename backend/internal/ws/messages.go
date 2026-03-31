package ws

import (
	"encoding/json"
	"errors"

	"github.com/mdjarv/agentique/backend/internal/session"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// ClientMessage is the envelope for all client -> server messages.
type ClientMessage struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// ServerResponse is sent back to the client correlated by ID.
type ServerResponse struct {
	ID      string     `json:"id"`
	Type    string     `json:"type"` // always "response"
	Payload any        `json:"payload,omitempty"`
	Error   *ErrorBody `json:"error,omitempty"`
}

// ServerPush is a fire-and-forget event from server to client.
type ServerPush struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

// ErrorBody is the error field in a ServerResponse.
type ErrorBody struct {
	Message string `json:"message"`
}

// --- Method payloads ---

type ProjectSubscribePayload struct {
	ProjectID string `json:"projectId"`
}

type SessionCreatePayload struct {
	ProjectID       string                  `json:"projectId"`
	Name            string                  `json:"name"`
	Worktree        bool                    `json:"worktree"`
	Branch          string                  `json:"branch"`
	Model           string                  `json:"model"`
	PlanMode        bool                    `json:"planMode"`
	AutoApproveMode string                  `json:"autoApproveMode"`
	Effort          string                  `json:"effort"`
	MaxBudget       float64                 `json:"maxBudget"`
	MaxTurns        int                     `json:"maxTurns"`
	BehaviorPresets session.BehaviorPresets  `json:"behaviorPresets"`
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

// --- Project git payloads ---

type ProjectGitStatusPayload struct {
	ProjectID string `json:"projectId"`
}

type ProjectFetchPayload struct {
	ProjectID string `json:"projectId"`
}

type ProjectPushPayload struct {
	ProjectID string `json:"projectId"`
}

type ProjectCommitPayload struct {
	ProjectID string `json:"projectId"`
	Message   string `json:"message"`
}

type ProjectTrackedFilesPayload struct {
	ProjectID string `json:"projectId"`
}

type ProjectCommandsPayload struct {
	ProjectID string `json:"projectId"`
}

type ProjectReorderPayload struct {
	ProjectIDs []string `json:"projectIds"`
}

type ProjectSetFavoritePayload struct {
	ProjectID string `json:"projectId"`
	Favorite  bool   `json:"favorite"`
}

type ProjectSetTagsPayload struct {
	ProjectID string   `json:"projectId"`
	TagIDs    []string `json:"tagIds"`
}

// --- Tag payloads ---

type TagCreatePayload struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type TagUpdatePayload struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type TagDeletePayload struct {
	ID string `json:"id"`
}

type TagListResult struct {
	Tags        []store.Tag        `json:"tags"`
	ProjectTags []store.ProjectTag `json:"projectTags"`
}

// --- Validate methods ---

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
)

func (p *ProjectSubscribePayload) Validate() error {
	if p.ProjectID == "" {
		return errProjectIDRequired
	}
	return nil
}

func (p *SessionCreatePayload) Validate() error {
	if p.ProjectID == "" {
		return errProjectIDRequired
	}
	return nil
}

func (p *SessionQueryPayload) Validate() error {
	if p.SessionID == "" || p.Prompt == "" {
		return errSessionIDAndPromptRequired
	}
	return nil
}

func (p *SessionListPayload) Validate() error {
	if p.ProjectID == "" {
		return errProjectIDRequired
	}
	return nil
}

func (p *SessionStopPayload) Validate() error {
	if p.SessionID == "" {
		return errSessionIDRequired
	}
	return nil
}

func (p *SessionResumePayload) Validate() error {
	if p.SessionID == "" {
		return errSessionIDRequired
	}
	return nil
}

func (p *SessionHistoryPayload) Validate() error {
	if p.SessionID == "" {
		return errSessionIDRequired
	}
	return nil
}

func (p *SessionDiffPayload) Validate() error {
	if p.SessionID == "" {
		return errSessionIDRequired
	}
	return nil
}

func (p *SessionInterruptPayload) Validate() error {
	if p.SessionID == "" {
		return errSessionIDRequired
	}
	return nil
}

func (p *SessionMergePayload) Validate() error {
	if p.SessionID == "" {
		return errSessionIDRequired
	}
	return nil
}

func (p *SessionCreatePRPayload) Validate() error {
	if p.SessionID == "" {
		return errSessionIDRequired
	}
	return nil
}

func (p *SessionDeletePayload) Validate() error {
	if p.SessionID == "" {
		return errSessionIDRequired
	}
	return nil
}

func (p *SessionDeleteBulkPayload) Validate() error {
	if len(p.SessionIDs) == 0 {
		return errSessionIDsRequired
	}
	return nil
}

func (p *SessionRenamePayload) Validate() error {
	if p.SessionID == "" || p.Name == "" {
		return errSessionIDAndNameRequired
	}
	return nil
}

func (p *SessionSetModelPayload) Validate() error {
	if p.SessionID == "" || p.Model == "" {
		return errSessionIDAndModelRequired
	}
	return nil
}

func (p *SessionSetPermissionPayload) Validate() error {
	if p.SessionID == "" || p.Mode == "" {
		return errSessionIDAndModeRequired
	}
	return nil
}

func (p *SessionSetAutoApprovePayload) Validate() error {
	if p.SessionID == "" || p.Mode == "" {
		return errSessionIDAndModeRequired
	}
	return nil
}

func (p *SessionResolveApprovalPayload) Validate() error {
	if p.SessionID == "" || p.ApprovalID == "" {
		return errApprovalIDRequired
	}
	return nil
}

func (p *SessionResolveQuestionPayload) Validate() error {
	if p.SessionID == "" || p.QuestionID == "" {
		return errQuestionIDRequired
	}
	return nil
}

func (p *SessionCommitPayload) Validate() error {
	if p.SessionID == "" || p.Message == "" {
		return errSessionIDAndMsgRequired
	}
	return nil
}

func (p *SessionRebasePayload) Validate() error {
	if p.SessionID == "" {
		return errSessionIDRequired
	}
	return nil
}

func (p *SessionGeneratePRDescPayload) Validate() error {
	if p.SessionID == "" {
		return errSessionIDRequired
	}
	return nil
}

func (p *SessionGenerateCommitMsgPayload) Validate() error {
	if p.SessionID == "" {
		return errSessionIDRequired
	}
	return nil
}

func (p *SessionMarkDonePayload) Validate() error {
	if p.SessionID == "" {
		return errSessionIDRequired
	}
	return nil
}

func (p *SessionCleanPayload) Validate() error {
	if p.SessionID == "" {
		return errSessionIDRequired
	}
	return nil
}

func (p *SessionUncommittedFilesPayload) Validate() error {
	if p.SessionID == "" {
		return errSessionIDRequired
	}
	return nil
}

func (p *SessionUncommittedDiffPayload) Validate() error {
	if p.SessionID == "" {
		return errSessionIDRequired
	}
	return nil
}

func (p *SessionRefreshGitPayload) Validate() error {
	if p.SessionID == "" {
		return errSessionIDRequired
	}
	return nil
}

func (p *ProjectGitStatusPayload) Validate() error {
	if p.ProjectID == "" {
		return errProjectIDRequired
	}
	return nil
}

func (p *ProjectFetchPayload) Validate() error {
	if p.ProjectID == "" {
		return errProjectIDRequired
	}
	return nil
}

func (p *ProjectPushPayload) Validate() error {
	if p.ProjectID == "" {
		return errProjectIDRequired
	}
	return nil
}

var errProjectIDsRequired = errors.New("projectIds is required")

var errProjectIDAndMsgRequired = errors.New("projectId and message are required")

func (p *ProjectCommitPayload) Validate() error {
	if p.ProjectID == "" || p.Message == "" {
		return errProjectIDAndMsgRequired
	}
	return nil
}

func (p *ProjectTrackedFilesPayload) Validate() error {
	if p.ProjectID == "" {
		return errProjectIDRequired
	}
	return nil
}

func (p *ProjectCommandsPayload) Validate() error {
	if p.ProjectID == "" {
		return errProjectIDRequired
	}
	return nil
}

func (p *ProjectReorderPayload) Validate() error {
	if len(p.ProjectIDs) == 0 {
		return errProjectIDsRequired
	}
	return nil
}

func (p *ProjectSetFavoritePayload) Validate() error {
	if p.ProjectID == "" {
		return errProjectIDRequired
	}
	return nil
}

func (p *ProjectSetTagsPayload) Validate() error {
	if p.ProjectID == "" {
		return errProjectIDRequired
	}
	return nil
}

var (
	errTagNameRequired = errors.New("name is required")
	errTagIDRequired   = errors.New("id is required")
)

func (p *TagCreatePayload) Validate() error {
	if p.Name == "" {
		return errTagNameRequired
	}
	return nil
}

func (p *TagUpdatePayload) Validate() error {
	if p.ID == "" {
		return errTagIDRequired
	}
	if p.Name == "" {
		return errTagNameRequired
	}
	return nil
}

func (p *TagDeletePayload) Validate() error {
	if p.ID == "" {
		return errTagIDRequired
	}
	return nil
}

// --- Team payloads ---

type TeamCreatePayload struct {
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
}

type TeamDeletePayload struct {
	TeamID string `json:"teamId"`
}

type TeamDissolvePayload struct {
	TeamID string `json:"teamId"`
}

type TeamJoinPayload struct {
	SessionID string `json:"sessionId"`
	TeamID    string `json:"teamId"`
	Role      string `json:"role"`
}

type TeamLeavePayload struct {
	SessionID string `json:"sessionId"`
}

type TeamListPayload struct {
	ProjectID string `json:"projectId"`
}

type TeamInfoPayload struct {
	TeamID string `json:"teamId"`
}

type TeamTimelinePayload struct {
	TeamID string `json:"teamId"`
}

type TeamSendMessagePayload struct {
	SenderSessionID string `json:"senderSessionId"`
	TargetSessionID string `json:"targetSessionId"`
	Content         string `json:"content"`
}

var (
	errTeamIDRequired              = errors.New("teamId is required")
	errTeamProjectIDRequired       = errors.New("projectId is required")
	errTeamSessionIDRequired       = errors.New("sessionId is required")
	errTeamSessionAndTeamRequired  = errors.New("sessionId and teamId are required")
	errTeamSendMessageRequired     = errors.New("senderSessionId, targetSessionId, and content are required")
)

func (p *TeamCreatePayload) Validate() error {
	if p.ProjectID == "" {
		return errTeamProjectIDRequired
	}
	return nil
}

func (p *TeamDeletePayload) Validate() error {
	if p.TeamID == "" {
		return errTeamIDRequired
	}
	return nil
}

func (p *TeamDissolvePayload) Validate() error {
	if p.TeamID == "" {
		return errTeamIDRequired
	}
	return nil
}

func (p *TeamJoinPayload) Validate() error {
	if p.SessionID == "" || p.TeamID == "" {
		return errTeamSessionAndTeamRequired
	}
	return nil
}

func (p *TeamLeavePayload) Validate() error {
	if p.SessionID == "" {
		return errTeamSessionIDRequired
	}
	return nil
}

func (p *TeamListPayload) Validate() error {
	if p.ProjectID == "" {
		return errTeamProjectIDRequired
	}
	return nil
}

func (p *TeamInfoPayload) Validate() error {
	if p.TeamID == "" {
		return errTeamIDRequired
	}
	return nil
}

func (p *TeamTimelinePayload) Validate() error {
	if p.TeamID == "" {
		return errTeamIDRequired
	}
	return nil
}

func (p *TeamSendMessagePayload) Validate() error {
	if p.SenderSessionID == "" || p.TargetSessionID == "" || p.Content == "" {
		return errTeamSendMessageRequired
	}
	return nil
}

type TeamCreateSwarmPayload struct {
	ProjectID     string                     `json:"projectId"`
	TeamName      string                     `json:"teamName"`
	LeadSessionID string                     `json:"leadSessionId"`
	Members       []session.SwarmMemberSpec  `json:"members"`
}

func (p *TeamCreateSwarmPayload) Validate() error {
	if p.ProjectID == "" {
		return errTeamProjectIDRequired
	}
	if len(p.Members) == 0 {
		return errors.New("at least one member is required")
	}
	return nil
}
