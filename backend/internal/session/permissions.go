package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	claudecli "github.com/allbin/claudecli-go"
	"github.com/allbin/agentique/backend/internal/store"
	"github.com/google/uuid"
)

// PermissionMode returns the current permission mode string.
func (s *Session) PermissionMode() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.permissionMode
}

// AutoApproveMode returns the auto-approve mode ("manual", "auto", "fullAuto").
func (s *Session) AutoApproveMode() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.autoApproveMode
}

// SetPermissionMode changes the CLI permission mode (plan, acceptEdits, default).
func (s *Session) SetPermissionMode(mode string) error {
	var m claudecli.PermissionMode
	switch mode {
	case "plan":
		m = claudecli.PermissionPlan
	case "acceptEdits":
		m = claudecli.PermissionAcceptEdits
	default:
		mode = "default"
		m = claudecli.PermissionDefault
	}

	s.mu.Lock()
	if s.cliSess == nil {
		s.mu.Unlock()
		return ErrNotLive
	}
	cli := s.cliSess
	s.mu.Unlock()

	if err := cli.SetPermissionMode(m); err != nil {
		return fmt.Errorf("set permission mode: %w", err)
	}

	s.mu.Lock()
	s.permissionMode = mode
	resolved := s.approvalState.resolveBypassable()
	s.mu.Unlock()

	for _, id := range resolved {
		s.broadcast("session.approval-auto-resolved", PushApprovalResolved{SessionID: s.ID, ApprovalID: id})
	}
	return nil
}

// SetAutoApproveMode sets the auto-approve mode. Valid values: "manual", "auto", "fullAuto".
// If the new mode permits pending tool approvals, they are auto-resolved.
func (s *Session) SetAutoApproveMode(mode string) {
	switch mode {
	case "auto", "fullAuto":
	default:
		mode = "manual"
	}
	s.mu.Lock()
	s.autoApproveMode = mode
	resolved := s.approvalState.resolveBypassable()
	s.mu.Unlock()

	for _, id := range resolved {
		s.broadcast("session.approval-auto-resolved", PushApprovalResolved{SessionID: s.ID, ApprovalID: id})
	}
}

// transitionPlanMode updates permission mode if changed, persists to DB, and
// broadcasts to connected clients. Idempotent — no-op if already in target mode.
func (s *Session) transitionPlanMode(mode string) {
	s.mu.Lock()
	if s.permissionMode == mode {
		s.mu.Unlock()
		return
	}
	s.permissionMode = mode
	s.mu.Unlock()

	if err := s.queries.UpdateSessionPermissionMode(context.Background(), store.UpdateSessionPermissionModeParams{
		PermissionMode: mode,
		ID:             s.ID,
	}); err != nil {
		slog.Warn("failed to persist permission mode", "session_id", s.ID, "mode", mode, "error", err)
	}

	s.broadcast("session.permission-mode-changed", PushPermissionModeChanged{SessionID: s.ID, PermissionMode: mode})
}

// requestPlanReview creates a synthetic pending approval for ExitPlanMode so
// the user can review the plan before execution begins. Interrupts the session
// to prevent tool calls between ExitPlanMode and user confirmation.
func (s *Session) requestPlanReview(input json.RawMessage) {
	approvalID := uuid.New().String()
	ch := make(chan *claudecli.PermissionResponse, 1)

	pa := &pendingApproval{
		id:       approvalID,
		toolName: "ExitPlanMode",
		input:    input,
		ch:       ch,
	}

	s.mu.Lock()
	s.pendingApprovals[approvalID] = pa
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.pendingApprovals, approvalID)
		s.mu.Unlock()
	}()

	s.broadcast("session.tool-permission", PushToolPermission{
		SessionID: s.ID, ApprovalID: approvalID, ToolName: "ExitPlanMode", Input: input,
	})

	select {
	case resp := <-ch:
		if resp.Allow {
			s.transitionPlanMode("default")
			if err := s.waitForIdle(5 * time.Second); err != nil {
				slog.Warn("wait for idle after plan approval failed", "session_id", s.ID, "error", err)
				return
			}
			if err := s.Query(context.Background(), "Plan approved. Proceed with implementation.", nil); err != nil {
				slog.Warn("auto-resume after plan approval failed", "session_id", s.ID, "error", err)
			}
		} else {
			// User chose to keep chatting or start fresh. The CLI already
			// exited plan mode internally — set it back.
			s.mu.Lock()
			cli := s.cliSess
			s.mu.Unlock()
			if cli != nil {
				if err := cli.SetPermissionMode(claudecli.PermissionPlan); err != nil {
					slog.Warn("failed to restore plan mode after deny", "session_id", s.ID, "error", err)
				}
			}
		}
	case <-s.ctx.Done():
		// Session closed while waiting for review.
	}
}

func (s *Session) waitForIdle(timeout time.Duration) error {
	// Check immediately before waiting.
	s.mu.Lock()
	st := s.state
	s.mu.Unlock()
	if st == StateIdle || st == StateDone || st == StateStopped {
		return nil
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case <-s.stateChangedCh:
			s.mu.Lock()
			st = s.state
			s.mu.Unlock()
			if st == StateIdle || st == StateDone || st == StateStopped {
				return nil
			}
		case <-timer.C:
			return fmt.Errorf("session %s: timed out waiting for idle (state: %s)", s.ID, s.State())
		case <-s.ctx.Done():
			return fmt.Errorf("session %s: context cancelled while waiting for idle", s.ID)
		}
	}
}

// planSafeCategories lists tool categories that can be auto-approved in plan
// mode. These are read-only tools that cannot mutate the filesystem or execute
// arbitrary commands.
var planSafeCategories = map[string]bool{
	"file_read": true,
	"web":       true,
	"agent":     true,
	"task":      true,
}

// isPlanSafeTool reports whether a tool can be auto-approved in plan mode.
func isPlanSafeTool(toolName string) bool {
	return planSafeCategories[classifyTool(toolName)]
}

// autoSafeCategories lists tool categories that can be auto-approved in "auto"
// mode. Superset of planSafeCategories — adds file writes, meta, and question
// tools. Only Bash and MCP tools remain gated.
var autoSafeCategories = map[string]bool{
	"file_read":  true, // Read, Glob, Grep
	"file_write": true, // Edit, Write, NotebookEdit, MultiEdit
	"web":        true, // WebSearch, WebFetch
	"agent":      true, // Agent, ExitWorktree
	"task":       true, // TodoWrite, TodoRead
	"meta":       true, // ToolSearch, Skill
	"question":   true, // AskUserQuestion
}

// isAutoSafeTool reports whether a tool can be auto-approved in "auto" mode.
func isAutoSafeTool(toolName string) bool {
	return autoSafeCategories[classifyTool(toolName)]
}

// shouldBypassPermission determines whether a tool should be auto-approved.
//
//	EnterPlanMode            → always bypass
//	fullAuto                 → always bypass
//	auto + plan permMode     → bypass only plan-safe tools
//	auto + non-plan permMode → bypass only auto-safe tools
//	manual                   → never bypass
func shouldBypassPermission(autoMode, permMode, toolName string) bool {
	if toolName == "EnterPlanMode" {
		return true
	}
	switch autoMode {
	case "fullAuto":
		return true
	case "auto":
		if permMode == "plan" {
			return isPlanSafeTool(toolName)
		}
		return isAutoSafeTool(toolName)
	default: // "manual"
		return false
	}
}

// handleToolPermission is the callback for claudecli WithCanUseTool.
// Blocks until the user resolves the approval or Close() cancels all pending approvals.
// claudecli-go runs this in a goroutine and also selects on ctx.Done(), so even if
// this blocks, the SDK will unblock on context cancellation.
func (s *Session) handleToolPermission(toolName string, input json.RawMessage) (*claudecli.PermissionResponse, error) {
	if interceptor, ok := s.toolInterceptors[toolName]; ok {
		return interceptor(input)
	}

	approvalID := uuid.New().String()
	ch := make(chan *claudecli.PermissionResponse, 1)

	pa := &pendingApproval{
		id:       approvalID,
		toolName: toolName,
		input:    input,
		ch:       ch,
	}

	// Single lock: check bypass and register pending approval atomically.
	// Prevents TOCTOU race where SetAutoApproveMode iterates pending approvals
	// between the bypass check and registration, missing this approval.
	s.mu.Lock()
	bypass := shouldBypassPermission(s.autoApproveMode, s.permissionMode, toolName)
	if !bypass {
		s.pendingApprovals[approvalID] = pa
	}
	s.mu.Unlock()

	if bypass {
		slog.Info("auto-approved tool", "session_id", s.ID, "tool", toolName,
			"auto_approve_mode", s.autoApproveMode, "permission_mode", s.permissionMode)
		// Defensive fallback — processEvent also detects EnterPlanMode from the
		// event stream, but if the CLI ever starts sending can_use_tool for it,
		// handle it here too. transitionPlanMode is idempotent.
		if toolName == "EnterPlanMode" {
			s.transitionPlanMode("plan")
		}
		return &claudecli.PermissionResponse{Allow: true}, nil
	}

	defer func() {
		s.mu.Lock()
		delete(s.pendingApprovals, approvalID)
		s.mu.Unlock()
	}()

	slog.Debug("tool permission requested", "session_id", s.ID, "tool", toolName, "approval_id", approvalID)

	s.broadcast("session.tool-permission", PushToolPermission{
		SessionID: s.ID, ApprovalID: approvalID, ToolName: toolName, Input: input,
	})

	select {
	case resp := <-ch:
		slog.Debug("tool permission resolved", "session_id", s.ID, "tool", toolName, "approval_id", approvalID, "allow", resp.Allow)
		return resp, nil
	case <-s.ctx.Done():
		return &claudecli.PermissionResponse{Allow: false, DenyMessage: "session closed"}, nil
	}
}

// ResolveApproval sends a permission response for a pending tool approval.
func (s *Session) ResolveApproval(approvalID string, allow bool, denyMessage string) error {
	s.mu.Lock()
	pa, ok := s.pendingApprovals[approvalID]
	s.mu.Unlock()

	if !ok {
		return fmt.Errorf("approval %s not found or already resolved", approvalID)
	}

	resp := &claudecli.PermissionResponse{
		Allow:       allow,
		DenyMessage: denyMessage,
	}

	select {
	case pa.ch <- resp:
		s.broadcast("session.approval-resolved", PushApprovalResolved{SessionID: s.ID, ApprovalID: approvalID})
		return nil
	default:
		return fmt.Errorf("approval %s already resolved", approvalID)
	}
}

// handleUserInput is the callback for claudecli WithUserInput.
// Blocks until the user answers or Close() cancels all pending questions.
func (s *Session) handleUserInput(questions []claudecli.Question) (map[string]string, error) {
	questionID := uuid.New().String()
	ch := make(chan map[string]string, 1)

	pq := &pendingQuestion{
		id:        questionID,
		questions: questions,
		ch:        ch,
	}

	s.mu.Lock()
	s.pendingQuestions[questionID] = pq
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.pendingQuestions, questionID)
		s.mu.Unlock()
	}()

	wireQs := make([]WireQuestion, len(questions))
	for i, q := range questions {
		opts := make([]WireQuestionOption, len(q.Options))
		for j, o := range q.Options {
			opts[j] = WireQuestionOption{Label: o.Label, Description: o.Description}
		}
		wireQs[i] = WireQuestion{
			Question: q.Question, Header: q.Header, Options: opts, MultiSelect: q.MultiSelect,
		}
	}
	s.broadcast("session.user-question", PushUserQuestion{
		SessionID: s.ID, QuestionID: questionID, Questions: wireQs,
	})

	select {
	case answers := <-ch:
		if answers == nil {
			return nil, fmt.Errorf("question cancelled")
		}
		return answers, nil
	case <-s.ctx.Done():
		return nil, fmt.Errorf("session closed")
	}
}

// ResolveQuestion sends answers for a pending user question.
func (s *Session) ResolveQuestion(questionID string, answers map[string]string) error {
	s.mu.Lock()
	pq, ok := s.pendingQuestions[questionID]
	s.mu.Unlock()

	if !ok {
		return fmt.Errorf("question %s not found or already resolved", questionID)
	}

	select {
	case pq.ch <- answers:
		s.broadcast("session.question-resolved", PushQuestionResolved{SessionID: s.ID, QuestionID: questionID})
		return nil
	default:
		return fmt.Errorf("question %s already resolved", questionID)
	}
}
