package session

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	claudecli "github.com/allbin/claudecli-go"
	"github.com/allbin/agentique/backend/internal/gitops"
	"github.com/allbin/agentique/backend/internal/store"
	"github.com/google/uuid"
)

func sqlNullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// QueryAttachment represents a base64-encoded file (image or PDF) attached to a query.
type QueryAttachment struct {
	Name     string `json:"name"`
	MimeType string `json:"mimeType"`
	DataUrl  string `json:"dataUrl"`
}

// nullStr extracts the string value from a sql.NullString, returning "" if null.
func nullStr(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

// pendingApproval tracks a single tool permission request waiting for user input.
type pendingApproval struct {
	id       string
	toolName string
	input    json.RawMessage
	ch       chan *claudecli.PermissionResponse
}

// pendingQuestion tracks a single AskUserQuestion request waiting for user input.
type pendingQuestion struct {
	id        string
	questions []claudecli.Question
	ch        chan map[string]string
}

const (
	eventLoopShutdownTimeout = 3 * time.Second
	watchdogWarnAfter        = 2 * time.Minute
	watchdogFailAfter        = 5 * time.Minute

	// Tool result truncation thresholds for DB storage.
	maxToolResultDBSize = 10_000
	toolResultKeepHead  = 4_000
	toolResultKeepTail  = 1_000
)

// Session wraps a single claudecli-go interactive session.
type Session struct {
	ID        string
	ProjectID string

	ctx              context.Context
	cancelCtx        context.CancelFunc
	mu               sync.Mutex
	state            State
	cliSess          *claudecli.Session
	queryCount       int
	claudeSessionID  string
	turnIndex        int
	seqInTurn        int
	queries          sessionQueries
	broadcast        func(pushType string, payload any)
	pendingApprovals map[string]*pendingApproval
	pendingQuestions map[string]*pendingQuestion
	autoApprove    bool
	permissionMode string
	worktreeMerged bool
	gitOperation   string
	workDir        string
	eventLoopDone    chan struct{}
}

type sessionParams struct {
	id        string
	projectID string
	cliSess   *claudecli.Session
	queries   sessionQueries
	broadcast func(pushType string, payload any)
	turnIndex int
	workDir   string
}

func newSession(p sessionParams) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Session{
		ID:               p.id,
		ProjectID:        p.projectID,
		ctx:              ctx,
		cancelCtx:        cancel,
		state:            StateIdle,
		cliSess:          p.cliSess,
		queries:          p.queries,
		broadcast:        p.broadcast,
		turnIndex:        p.turnIndex,
		pendingApprovals: make(map[string]*pendingApproval),
		pendingQuestions: make(map[string]*pendingQuestion),
		permissionMode:   "default",
		workDir:          p.workDir,
		eventLoopDone:    make(chan struct{}),
	}
	s.broadcastState(StateIdle)
	if s.cliSess != nil {
		s.startEventLoop()
	}
	return s
}

// setCLISession attaches a connected CLI session and starts the event loop.
// Used when the Session must be created before Connect() so the permission callback
// can capture the Session reference.
func (s *Session) setCLISession(cliSess *claudecli.Session) {
	s.mu.Lock()
	s.cliSess = cliSess
	s.mu.Unlock()
	s.startEventLoop()
}

// ClaudeSessionID returns the Claude CLI session ID, if available.
func (s *Session) ClaudeSessionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.claudeSessionID
}

// QueryCount returns the number of queries sent to this session.
func (s *Session) QueryCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.queryCount
}

// Query sends a prompt (with optional images) to the Claude session and starts streaming events.
// The caller's ctx is NOT used — all DB writes and CLI operations use background
// contexts so they complete even if the triggering WS connection disconnects.
func (s *Session) Query(_ context.Context, prompt string, attachments []QueryAttachment) error {
	s.mu.Lock()
	if err := validateTransition(s.state, StateRunning, s.ID); err != nil {
		s.mu.Unlock()
		return err
	}
	s.state = StateRunning
	s.queryCount++
	s.turnIndex++
	s.seqInTurn = 0
	s.mu.Unlock()

	// Persist running state to DB so it survives server restarts.
	if err := s.queries.UpdateSessionState(context.Background(), store.UpdateSessionStateParams{
		State: string(StateRunning),
		ID:    s.ID,
	}); err != nil {
		slog.Error("persist running state failed", "session_id", s.ID, "error", err)
	}

	// Persist prompt (and images) as seq 0 of the new turn.
	promptPayload := map[string]any{"prompt": prompt}
	if len(attachments) > 0 {
		promptPayload["attachments"] = attachments
	}
	promptData, _ := json.Marshal(promptPayload)
	if err := s.queries.InsertEvent(context.Background(), store.InsertEventParams{
		SessionID: s.ID,
		TurnIndex: int64(s.turnIndex),
		Seq:       0,
		Type:      "prompt",
		Data:      string(promptData),
	}); err != nil {
		slog.Error("persist prompt event failed", "session_id", s.ID, "error", err)
	}
	s.mu.Lock()
	s.seqInTurn = 1
	s.mu.Unlock()

	s.broadcastState(StateRunning)

	if len(attachments) == 0 {
		if err := s.cliSess.Query(prompt); err != nil {
			if stErr := s.setState(StateFailed); stErr != nil {
				slog.Error("state transition failed", "session_id", s.ID, "error", stErr)
			}
			return err
		}
		return nil
	}

	blocks, err := toContentBlocks(attachments)
	if err != nil {
		if stErr := s.setState(StateFailed); stErr != nil {
			slog.Error("state transition failed", "session_id", s.ID, "error", stErr)
		}
		return fmt.Errorf("parse attachments: %w", err)
	}
	if err := s.cliSess.QueryWithContent(prompt, blocks...); err != nil {
		if stErr := s.setState(StateFailed); stErr != nil {
			slog.Error("state transition failed", "session_id", s.ID, "error", stErr)
		}
		return err
	}
	return nil
}

// toContentBlocks converts frontend attachments (data URLs) to claudecli ContentBlocks.
func toContentBlocks(attachments []QueryAttachment) ([]claudecli.ContentBlock, error) {
	blocks := make([]claudecli.ContentBlock, 0, len(attachments))
	for _, a := range attachments {
		mediaType, data, err := parseDataUrl(a.DataUrl)
		if err != nil {
			return nil, fmt.Errorf("attachment %q: %w", a.Name, err)
		}
		if strings.HasPrefix(mediaType, "image/") {
			blocks = append(blocks, claudecli.ImageBlock(mediaType, data))
		} else {
			blocks = append(blocks, claudecli.DocumentBlock(mediaType, data))
		}
	}
	return blocks, nil
}

// parseDataUrl extracts the media type and decoded bytes from a data URL.
func parseDataUrl(dataUrl string) (mediaType string, data []byte, err error) {
	// Format: data:<mediaType>;base64,<data>
	if !strings.HasPrefix(dataUrl, "data:") {
		return "", nil, fmt.Errorf("not a data URL")
	}
	rest := dataUrl[5:]
	semi := strings.Index(rest, ";")
	if semi < 0 {
		return "", nil, fmt.Errorf("missing ;base64 separator")
	}
	mediaType = rest[:semi]
	after := rest[semi+1:]
	if !strings.HasPrefix(after, "base64,") {
		return "", nil, fmt.Errorf("not base64-encoded")
	}
	b64 := after[7:]
	data, err = base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", nil, fmt.Errorf("decode base64: %w", err)
	}
	return mediaType, data, nil
}

// startEventLoop reads events from the claudecli-go session, persists them
// to the database, and broadcasts them to all project WebSocket clients.
// Signals eventLoopDone on exit. Exits on context cancellation or channel close.
func (s *Session) startEventLoop() {
	go func() {
		defer close(s.eventLoopDone)

		events := s.cliSess.Events()
		watchdog := time.NewTimer(watchdogWarnAfter)
		defer watchdog.Stop()
		var warned bool

		for {
			select {
			case <-s.ctx.Done():
				return
			case event, ok := <-events:
				if !ok {
					s.mu.Lock()
					st := s.state
					s.mu.Unlock()
					// Only transition to done on clean channel close.
					// Skip if already in a terminal state (failed/stopped/done)
					// so we don't mask the real reason the session ended.
					if st != StateDone && st != StateFailed && st != StateStopped {
						if err := s.setState(StateDone); err != nil {
							slog.Error("state transition failed", "session_id", s.ID, "error", err)
						}
					}
					return
				}
				watchdog.Reset(watchdogWarnAfter)
				warned = false
				s.safeProcessEvent(event)
			case <-watchdog.C:
				s.mu.Lock()
				st := s.state
				s.mu.Unlock()
				if st != StateRunning {
					watchdog.Reset(watchdogWarnAfter)
					continue
				}
				if !warned {
					warned = true
					s.broadcast("session.event", map[string]any{
						"sessionId": s.ID,
						"event": WireErrorEvent{
							Type:    "error",
							Message: "session may be unresponsive — no activity for 2 minutes",
							Fatal:   false,
						},
					})
					watchdog.Reset(watchdogFailAfter - watchdogWarnAfter)
					continue
				}
				slog.Error("watchdog timeout, marking session failed", "session_id", s.ID)
				s.setState(StateFailed)
				return
			}
		}
	}()
}

// safeProcessEvent wraps processEvent with panic recovery so a single
// malformed event can't kill the event loop goroutine.
func (s *Session) safeProcessEvent(event claudecli.Event) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic in processEvent", "session_id", s.ID, "panic", r)
			s.broadcast("session.event", map[string]any{
				"sessionId": s.ID,
				"event": WireErrorEvent{
					Type:    "error",
					Message: fmt.Sprintf("internal error processing event: %v", r),
					Fatal:   false,
				},
			})
		}
	}()
	s.processEvent(event)
}

// processEvent handles a single event from the CLI session.
func (s *Session) processEvent(event claudecli.Event) {
	// Capture Claude session ID from InitEvent.
	if initEv, ok := event.(*claudecli.InitEvent); ok {
		s.mu.Lock()
		if s.claudeSessionID == "" && initEv.SessionID != "" {
			s.claudeSessionID = initEv.SessionID
			if err := s.queries.UpdateClaudeSessionID(context.Background(), store.UpdateClaudeSessionIDParams{
				ClaudeSessionID: sqlNullString(initEv.SessionID),
				ID:              s.ID,
			}); err != nil {
				slog.Error("persist claude session ID failed", "session_id", s.ID, "error", err)
			}
			slog.Debug("captured claude session ID", "session_id", s.ID, "claude_session_id", initEv.SessionID)
		}
		s.mu.Unlock()
		return
	}

	wireEvent := ToWireEvent(event)
	if wireEvent == nil {
		return
	}

	// Rate limit and stream events are transient — broadcast only, skip DB.
	switch wireEvent.(type) {
	case WireRateLimitEvent, WireStreamEvent:
		s.broadcast("session.event", map[string]any{
			"sessionId": s.ID,
			"event":     wireEvent,
		})
		return
	}

	// Persist to DB. Truncate large tool results to keep DB lean —
	// the full content is still broadcast to connected clients above.
	s.mu.Lock()
	seq := s.seqInTurn
	turnIdx := s.turnIndex
	s.seqInTurn++
	s.mu.Unlock()

	dbEvent := wireEvent
	if tr, ok := wireEvent.(WireToolResultEvent); ok {
		// Truncate large text blocks for DB; image blocks pass through.
		text := toolResultText(tr.Content)
		if len(text) > maxToolResultDBSize {
			truncated := text[:toolResultKeepHead] + "\n...[truncated]...\n" + text[len(text)-toolResultKeepTail:]
			blocks := make([]WireContentBlock, 0, len(tr.Content))
			replaced := false
			for _, b := range tr.Content {
				if b.Type == "text" && !replaced {
					blocks = append(blocks, WireContentBlock{Type: "text", Text: truncated})
					replaced = true
				} else if b.Type != "text" {
					blocks = append(blocks, b)
				}
			}
			tr.Content = blocks
			dbEvent = tr
		}
	}

	if data, err := json.Marshal(dbEvent); err == nil {
		typed, _ := wireEvent.(interface{ WireType() string })
		if err := s.queries.InsertEvent(context.Background(), store.InsertEventParams{
			SessionID: s.ID,
			TurnIndex: int64(turnIdx),
			Seq:       int64(seq),
			Type:      typed.WireType(),
			Data:      string(data),
		}); err != nil {
			slog.Error("persist event failed", "session_id", s.ID, "type", typed.WireType(), "error", err)
		}
	}

	// Broadcast to all project clients.
	s.broadcast("session.event", map[string]any{
		"sessionId": s.ID,
		"event":     wireEvent,
	})

	if _, ok := event.(*claudecli.ResultEvent); ok {
		if err := s.setState(StateIdle); err != nil {
			slog.Error("state transition failed", "session_id", s.ID, "error", err)
		}
	}

	if errEv, ok := event.(*claudecli.ErrorEvent); ok && errEv.Fatal {
		if err := s.setState(StateFailed); err != nil {
			slog.Error("state transition failed", "session_id", s.ID, "error", err)
		}
	}
}

func (s *Session) broadcastState(state State) {
	payload := map[string]any{
		"sessionId": s.ID,
		"state":     string(state),
		"connected": true,
	}
	if s.workDir != "" && (state == StateIdle || state == StateDone) {
		if dirty, err := gitops.HasUncommittedChanges(s.workDir); err == nil {
			payload["hasDirtyWorktree"] = dirty
			payload["hasUncommitted"] = dirty
		}
		s.enrichGitStatus(payload)
	}
	s.mu.Lock()
	if s.worktreeMerged {
		payload["worktreeMerged"] = true
	}
	if s.gitOperation != "" {
		payload["gitOperation"] = s.gitOperation
	}
	s.mu.Unlock()
	s.broadcast("session.state", payload)
}

// enrichGitStatus adds commitsAhead and branchMissing to a broadcast payload.
func (s *Session) enrichGitStatus(payload map[string]any) {
	ctx := context.Background()
	row, err := s.queries.GetSession(ctx, s.ID)
	if err != nil {
		return
	}
	branch := nullStr(row.WorktreeBranch)
	if branch == "" || row.WorktreeMerged != 0 {
		return
	}
	project, err := s.queries.GetProject(ctx, s.ProjectID)
	if err != nil {
		return
	}
	if !gitops.BranchExists(project.Path, branch) {
		payload["branchMissing"] = true
		return
	}
	if ahead, err := gitops.CommitsAhead(project.Path, branch); err == nil {
		payload["commitsAhead"] = ahead
	}
	if behind, err := gitops.CommitsBehind(project.Path, branch); err == nil {
		payload["commitsBehind"] = behind
	}
}

// PermissionMode returns the current permission mode string.
func (s *Session) PermissionMode() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.permissionMode
}

// AutoApprove returns whether automatic tool approval is enabled.
func (s *Session) AutoApprove() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.autoApprove
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
	s.mu.Unlock()
	return nil
}

// SetAutoApprove enables or disables automatic tool approval.
func (s *Session) SetAutoApprove(enabled bool) {
	s.mu.Lock()
	s.autoApprove = enabled
	s.mu.Unlock()
}

// Interrupt stops the current generation without killing the session.
// The event loop will receive a ResultEvent and transition back to idle.
func (s *Session) Interrupt() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != StateRunning {
		return fmt.Errorf("session is %s, not %s", string(s.state), string(StateRunning))
	}
	if s.cliSess == nil {
		return ErrNotLive
	}
	s.cliSess.Interrupt()
	return nil
}

// SetModel changes the model for this session. Only allowed when idle.
func (s *Session) SetModel(model string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == StateRunning {
		return fmt.Errorf("cannot change model while running")
	}
	if s.cliSess == nil {
		return ErrNotLive
	}
	s.cliSess.SetModel(resolveModel(model))
	return nil
}

// handleToolPermission is the callback for claudecli WithCanUseTool.
// Blocks until the user resolves the approval or Close() cancels all pending approvals.
// claudecli-go runs this in a goroutine and also selects on ctx.Done(), so even if
// this blocks, the SDK will unblock on context cancellation.
func (s *Session) handleToolPermission(toolName string, input json.RawMessage) (*claudecli.PermissionResponse, error) {
	s.mu.Lock()
	bypass := (s.autoApprove && s.permissionMode != "plan") || toolName == "EnterPlanMode"
	s.mu.Unlock()
	if bypass {
		return &claudecli.PermissionResponse{Allow: true}, nil
	}

	approvalID := uuid.New().String()
	ch := make(chan *claudecli.PermissionResponse, 1)

	pa := &pendingApproval{
		id:       approvalID,
		toolName: toolName,
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

	slog.Debug("tool permission requested", "session_id", s.ID, "tool", toolName, "approval_id", approvalID)

	s.broadcast("session.tool-permission", map[string]any{
		"sessionId":  s.ID,
		"approvalId": approvalID,
		"toolName":   toolName,
		"input":      input,
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

	s.broadcast("session.user-question", map[string]any{
		"sessionId":  s.ID,
		"questionId": questionID,
		"questions":  questions,
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
		return nil
	default:
		return fmt.Errorf("question %s already resolved", questionID)
	}
}

// Close gracefully shuts down the claudecli-go session.
// Cancels the context to unblock callbacks, closes the CLI session to stop the
// process and close the Events channel, then waits for the event loop to exit.
func (s *Session) Close() {
	// Capture state before close — the dying CLI process may race and
	// persist StateFailed via processEvent before we regain control.
	s.mu.Lock()
	stateBeforeClose := s.state
	s.mu.Unlock()

	s.cancelCtx()

	// Close CLI session to stop the process and close Events channel.
	s.mu.Lock()
	cli := s.cliSess
	s.cliSess = nil
	s.mu.Unlock()
	if cli != nil {
		cli.Close()
	}

	// Wait for event loop goroutine to finish.
	select {
	case <-s.eventLoopDone:
	case <-time.After(eventLoopShutdownTimeout):
		slog.Warn("event loop did not stop in time", "session_id", s.ID)
	}

	// Safety net: drain any pending approvals/questions that weren't
	// cleared by ctx cancellation.
	s.mu.Lock()
	for id, pa := range s.pendingApprovals {
		select {
		case pa.ch <- &claudecli.PermissionResponse{Allow: false, DenyMessage: "session closed"}:
		default:
		}
		delete(s.pendingApprovals, id)
	}
	for id, pq := range s.pendingQuestions {
		select {
		case pq.ch <- nil:
		default:
		}
		delete(s.pendingQuestions, id)
	}

	// Interrupted work → stopped. Idle sessions stay idle (nothing was
	// interrupted, the user just hasn't sent a followup yet). Terminal
	// states are preserved as-is.
	finalState := stateBeforeClose
	switch stateBeforeClose {
	case StateRunning, StateMerging:
		finalState = StateStopped
	}
	s.state = finalState
	s.mu.Unlock()

	// Persist to DB — overwrites any spurious "failed" set by the dying
	// CLI process during shutdown.
	if err := s.queries.UpdateSessionState(context.Background(), store.UpdateSessionStateParams{
		State: string(finalState),
		ID:    s.ID,
	}); err != nil {
		slog.Error("persist final state on close failed", "session_id", s.ID, "state", finalState, "error", err)
	}

	// Notify clients so they don't show stale state until next refresh.
	s.broadcastState(finalState)
}

// MarkDone transitions the session to StateDone.
func (s *Session) MarkDone() error {
	return s.setState(StateDone)
}

// MarkMerged sets the worktreeMerged flag on a live session.
func (s *Session) MarkMerged() {
	s.mu.Lock()
	s.worktreeMerged = true
	s.mu.Unlock()
}
