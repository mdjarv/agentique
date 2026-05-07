package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/allbin/agentkit/runtime"
	claudecli "github.com/allbin/claudecli-go"
	"github.com/google/uuid"
	"github.com/mdjarv/agentique/backend/internal/store"
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
	switch mode {
	case "plan", "acceptEdits", "default":
	default:
		mode = "default"
	}

	s.mu.Lock()
	rt := s.rt
	s.mu.Unlock()
	if rt == nil {
		return ErrNotLive
	}

	if err := rt.SetPlanMode(runtime.PlanMode(mode)); err != nil {
		return fmt.Errorf("set permission mode: %w", err)
	}

	s.mu.Lock()
	s.permissionMode = mode
	s.mu.Unlock()

	for _, id := range s.resolveBypassable() {
		s.broadcast("session.approval-auto-resolved", PushApprovalResolved{SessionID: s.ID, ApprovalID: id})
	}
	return nil
}

// SetAutoApproveMode sets the auto-approve mode. Valid values: "manual", "auto", "fullAuto".
// If the new mode permits pending tool approvals, they are auto-resolved.
func (s *Session) SetAutoApproveMode(mode string) {
	mode = normalizeAutoApprove(mode)
	s.mu.Lock()
	s.autoApproveMode = mode
	rt := s.rt
	s.mu.Unlock()

	if rt != nil {
		rt.SetAutoApproveMode(runtimeAutoApproveMode(mode))
	}

	for _, id := range s.resolveBypassable() {
		s.broadcast("session.approval-auto-resolved", PushApprovalResolved{SessionID: s.ID, ApprovalID: id})
	}
}

// resolveBypassable auto-resolves any runtime-pending approval the current mode
// would bypass. Acquires s.mu internally; safe to call without the lock.
func (s *Session) resolveBypassable() []string {
	s.mu.Lock()
	rt := s.rt
	autoMode := s.autoApproveMode
	permMode := s.permissionMode
	s.mu.Unlock()

	if rt == nil {
		return nil
	}
	rtA, _ := rt.PendingState()
	if rtA == nil {
		return nil
	}
	if !shouldBypassPermission(autoMode, permMode, rtA.ToolName) {
		return nil
	}
	if err := rt.SubmitApproval(rtA.ID, runtime.Decision{Allow: true}); err != nil && err != runtime.ErrPendingNotFound {
		slog.Warn("resolve bypassable failed", "session_id", s.ID, "approval_id", rtA.ID, "error", err)
		return nil
	}
	return []string{rtA.ID}
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
//
// The synthetic approval is agentique-side only — it doesn't pass through the
// runtime approval pump, because the trigger is a CLI event observation
// (ExitPlanMode tool_use) rather than a permission callback.
func (s *Session) requestPlanReview(input json.RawMessage) {
	approvalID := uuid.New().String()
	ch := make(chan *claudecli.PermissionResponse, 1)

	sa := &syntheticApproval{
		id:       approvalID,
		toolName: "ExitPlanMode",
		input:    input,
		ch:       ch,
	}

	s.mu.Lock()
	s.syntheticApprovals[approvalID] = sa
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.syntheticApprovals, approvalID)
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
			s.mu.Lock()
			rt := s.rt
			s.mu.Unlock()
			if rt != nil {
				if err := rt.SetPlanMode(runtime.PlanMode("plan")); err != nil {
					slog.Warn("failed to restore plan mode after deny", "session_id", s.ID, "error", err)
				}
			}
		}
	case <-s.ctx.Done():
		// Session closed while waiting for review.
	}
}

func (s *Session) waitForIdle(timeout time.Duration) error {
	st := s.State()
	if st == StateIdle || st == StateDone || st == StateStopped {
		return nil
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case <-s.stateChangedCh:
			st = s.State()
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
//	fullAuto                 → always bypass (also handled by runtime.AutoApproveAll)
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
	default:
		return false
	}
}

// ResolveApproval sends a permission response for a pending tool approval.
// Tries synthetic approvals first (plan-review, spawn UI), then forwards to
// the runtime approval pump.
func (s *Session) ResolveApproval(approvalID string, allow bool, denyMessage string) error {
	s.mu.Lock()
	sa, ok := s.syntheticApprovals[approvalID]
	rt := s.rt
	s.mu.Unlock()

	if ok {
		select {
		case sa.ch <- &claudecli.PermissionResponse{Allow: allow, DenyMessage: denyMessage}:
			s.broadcast("session.approval-resolved", PushApprovalResolved{SessionID: s.ID, ApprovalID: approvalID})
			return nil
		default:
			return fmt.Errorf("approval %s already resolved", approvalID)
		}
	}

	if rt == nil {
		return fmt.Errorf("approval %s not found or already resolved", approvalID)
	}
	if err := rt.SubmitApproval(approvalID, runtime.Decision{Allow: allow, DenyMessage: denyMessage}); err != nil {
		if err == runtime.ErrPendingNotFound {
			return fmt.Errorf("approval %s not found or already resolved", approvalID)
		}
		return err
	}
	s.broadcast("session.approval-resolved", PushApprovalResolved{SessionID: s.ID, ApprovalID: approvalID})
	return nil
}

// ResolveQuestion sends answers for a pending user question.
func (s *Session) ResolveQuestion(questionID string, answers map[string]string) error {
	s.mu.Lock()
	rt := s.rt
	s.mu.Unlock()
	if rt == nil {
		return fmt.Errorf("question %s not found or already resolved", questionID)
	}
	if err := rt.SubmitAnswer(questionID, answers); err != nil {
		if err == runtime.ErrPendingNotFound {
			return fmt.Errorf("question %s not found or already resolved", questionID)
		}
		return err
	}
	s.broadcast("session.question-resolved", PushQuestionResolved{SessionID: s.ID, QuestionID: questionID})
	return nil
}

// nowUTC returns an RFC3339 UTC timestamp (broken out for test seams).
func nowUTC() string { return time.Now().UTC().Format(time.RFC3339) }
