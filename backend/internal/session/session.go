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

// truncate returns the first n bytes of s, appending "..." if truncated.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
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

	// Debounce window for mid-turn git status refresh after write-tool results.
	gitRefreshDebounce = 500 * time.Millisecond
)

// Session wraps a single claudecli-go interactive session.
type Session struct {
	ID        string
	ProjectID string

	ctx              context.Context
	cancelCtx        context.CancelFunc
	mu               sync.Mutex
	state            State
	cliSess          CLISession
	queryCount       int
	pipeline         *EventPipeline
	queries          sessionQueries
	broadcast        func(pushType string, payload any)
	pendingApprovals map[string]*pendingApproval
	pendingQuestions map[string]*pendingQuestion
	autoApproveMode string // "manual", "auto", "fullAuto"
	permissionMode string
	worktreeMerged bool
	completedAt    string // ISO8601 timestamp or "" if not completed
	gitOperation   string
	workDir        string
	gitVersion      int64
	eventLoopDone   chan struct{}
	gitRefreshTimer  *time.Timer // debounce timer for mid-turn git refresh
	onAgentMessage   func(senderID, targetName, content string) error
	onSpawnWorkers   func(senderID string, req SpawnWorkersRequest) error
}


type sessionParams struct {
	id                string
	projectID         string
	cliSess           CLISession
	queries           sessionQueries
	broadcast         func(pushType string, payload any)
	turnIndex         int
	workDir           string
	initialGitVersion int64
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
		pendingApprovals: make(map[string]*pendingApproval),
		pendingQuestions: make(map[string]*pendingQuestion),
		permissionMode:   "default",
		workDir:          p.workDir,
		gitVersion:       p.initialGitVersion,
		eventLoopDone:    make(chan struct{}),
	}
	s.pipeline = NewEventPipeline(PipelineConfig{
		SessionID:        p.id,
		InitialTurnIndex: p.turnIndex,
		Sink: EventSink{
			Persist: func(turnIndex, seq int, wireType string, data []byte) {
				if err := p.queries.InsertEvent(context.Background(), store.InsertEventParams{
					SessionID: p.id,
					TurnIndex: int64(turnIndex),
					Seq:       int64(seq),
					Type:      wireType,
					Data:      string(data),
				}); err != nil {
					slog.Error("persist event failed", "session_id", p.id, "type", wireType, "error", err)
				}
			},
			Broadcast: p.broadcast,
		},
		OnClaudeSessionID: func(id string) {
			if err := p.queries.UpdateClaudeSessionID(context.Background(), store.UpdateClaudeSessionIDParams{
				ClaudeSessionID: sqlNullString(id),
				ID:              p.id,
			}); err != nil {
				slog.Error("persist claude session ID failed", "session_id", p.id, "error", err)
			}
		},
		OnPlanTransition: s.transitionPlanMode,
		OnExitPlanMode: func(input json.RawMessage) {
			s.mu.Lock()
			aam := s.autoApproveMode
			s.mu.Unlock()
			if aam == "fullAuto" {
				s.transitionPlanMode("default")
			} else {
				go s.requestPlanReview(input)
			}
		},
		OnWriteToolResult: s.scheduleGitRefresh,
		OnTurnComplete: func() {
			if err := s.setState(StateIdle); err != nil {
				slog.Error("state transition failed", "session_id", s.ID, "error", err)
			}
		},
		OnFatalError: func(err error) {
			if stErr := s.setState(StateFailed); stErr != nil {
				slog.Error("state transition failed", "session_id", s.ID, "error", stErr)
			}
		},
	})
	s.broadcastState(StateIdle)
	if s.cliSess != nil {
		s.startEventLoop()
	}
	return s
}

// setCLISession attaches a connected CLI session and starts the event loop.
// Used when the Session must be created before Connect() so the permission callback
// can capture the Session reference.
func (s *Session) setCLISession(cliSess CLISession) {
	s.mu.Lock()
	s.cliSess = cliSess
	s.mu.Unlock()
	s.startEventLoop()
}

// ClaudeSessionID returns the Claude CLI session ID, if available.
func (s *Session) ClaudeSessionID() string {
	return s.pipeline.ClaudeSessionID()
}

// QueryCount returns the number of queries sent to this session.
func (s *Session) QueryCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.queryCount
}

// GitVersion returns the current git version counter.
func (s *Session) GitVersion() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gitVersion
}

// SendMessage injects a user message mid-turn via the CLI's SendMessage API.
// The CLI processes it at the next safe boundary (between tool calls) and folds
// the response into the current turn's ResultEvent.
func (s *Session) SendMessage(prompt string, attachments []QueryAttachment) error {
	s.mu.Lock()
	if s.state != StateRunning {
		s.mu.Unlock()
		return fmt.Errorf("session %s: cannot send message in state %s", s.ID, string(s.state))
	}
	cli := s.cliSess
	if cli == nil {
		s.mu.Unlock()
		return ErrNotLive
	}
	s.mu.Unlock()

	turnIdx, seq := s.pipeline.AllocSeq()

	// Send to CLI.
	if len(attachments) > 0 {
		blocks, err := toContentBlocks(attachments)
		if err != nil {
			return fmt.Errorf("parse attachments: %w", err)
		}
		if err := cli.SendMessageWithContent(prompt, blocks...); err != nil {
			return err
		}
	} else {
		if err := cli.SendMessage(prompt); err != nil {
			return err
		}
	}

	// Persist + broadcast as a user_message event within the current turn.
	wireEvent := WireUserMessageEvent{Type: "user_message", Content: prompt, Attachments: attachments}
	if data, err := json.Marshal(wireEvent); err == nil {
		if err := s.queries.InsertEvent(context.Background(), store.InsertEventParams{
			SessionID: s.ID,
			TurnIndex: int64(turnIdx),
			Seq:       int64(seq),
			Type:      "user_message",
			Data:      string(data),
		}); err != nil {
			slog.Error("persist user_message event failed", "session_id", s.ID, "error", err)
		}
	}
	s.broadcast("session.event", map[string]any{
		"sessionId": s.ID,
		"event":     wireEvent,
	})
	return nil
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
	cli := s.cliSess
	if cli == nil {
		s.mu.Unlock()
		return fmt.Errorf("session %s: CLI session not connected", s.ID)
	}
	s.state = StateRunning
	s.queryCount++
	wasCompleted := s.completedAt != ""
	s.completedAt = ""
	s.mu.Unlock()

	turnIndex := s.pipeline.AdvanceTurn()

	// Persist running state to DB so it survives server restarts.
	if err := s.queries.UpdateSessionState(context.Background(), store.UpdateSessionStateParams{
		State: string(StateRunning),
		ID:    s.ID,
	}); err != nil {
		slog.Error("persist running state failed", "session_id", s.ID, "error", err)
	}
	if wasCompleted {
		if err := s.queries.UnsetSessionCompleted(context.Background(), s.ID); err != nil {
			slog.Error("persist session uncompleted failed", "session_id", s.ID, "error", err)
		}
	}

	// Persist prompt (and images) as seq 0 of the new turn.
	promptPayload := map[string]any{"prompt": prompt}
	if len(attachments) > 0 {
		promptPayload["attachments"] = attachments
	}
	promptData, err := json.Marshal(promptPayload)
	if err != nil {
		slog.Error("marshal prompt failed", "session_id", s.ID, "error", err)
	}
	if err := s.queries.InsertEvent(context.Background(), store.InsertEventParams{
		SessionID: s.ID,
		TurnIndex: int64(turnIndex),
		Seq:       0,
		Type:      "prompt",
		Data:      string(promptData),
	}); err != nil {
		slog.Error("persist prompt event failed", "session_id", s.ID, "error", err)
	}
	s.pipeline.SetSeq(1)

	s.broadcastState(StateRunning)

	// Notify frontend to create the turn entry. Essential for backend-initiated
	// turns (queue drain) where the frontend didn't call submitQuery locally.
	turnPayload := map[string]any{
		"sessionId": s.ID,
		"prompt":    prompt,
	}
	if len(attachments) > 0 {
		turnPayload["attachments"] = attachments
	}
	s.broadcast("session.turn-started", turnPayload)

	if len(attachments) == 0 {
		if err := cli.Query(prompt); err != nil {
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
	if err := cli.QueryWithContent(prompt, blocks...); err != nil {
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
	s.mu.Lock()
	cli := s.cliSess
	s.mu.Unlock()
	events := cli.Events()

	go func() {
		defer close(s.eventLoopDone)
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
				waitingForUser := len(s.pendingApprovals) > 0 || len(s.pendingQuestions) > 0
				s.mu.Unlock()
				if st != StateRunning || waitingForUser {
					watchdog.Reset(watchdogWarnAfter)
					continue
				}
				if !warned {
					warned = true
					s.broadcast("session.event", map[string]any{
						"sessionId": s.ID,
						"event": WireErrorEvent{
							Type:    "error",
							Content: "session may be unresponsive — no activity for 2 minutes",
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

// safeProcessEvent wraps pipeline.ProcessEvent with panic recovery so a single
// malformed event can't kill the event loop goroutine.
func (s *Session) safeProcessEvent(event claudecli.Event) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic in processEvent", "session_id", s.ID, "panic", r)
			s.broadcast("session.event", map[string]any{
				"sessionId": s.ID,
				"event": WireErrorEvent{
					Type:    "error",
					Content: fmt.Sprintf("internal error processing event: %v", r),
					Fatal:   false,
				},
			})
		}
	}()
	s.pipeline.ProcessEvent(event)
}

func (s *Session) broadcastState(state State) {
	snap := s.buildLocalSnapshot(state)
	s.broadcast("session.state", snap)
}

// buildLocalSnapshot constructs a GitSnapshot from the session's own state.
// Used by the Session itself (e.g. on running→idle transitions).
func (s *Session) buildLocalSnapshot(state State) GitSnapshot {
	snap := GitSnapshot{
		SessionID: s.ID,
		State:     string(state),
		Connected: true,
	}

	s.mu.Lock()
	snap.WorktreeMerged = s.worktreeMerged
	snap.CompletedAt = s.completedAt
	snap.GitOperation = s.gitOperation
	s.gitVersion++
	snap.Version = s.gitVersion
	s.mu.Unlock()

	if s.workDir != "" && !snap.WorktreeMerged && (state == StateIdle || state == StateDone) {
		if dirty, err := gitops.HasUncommittedChanges(s.workDir); err == nil {
			snap.HasDirtyWorktree = dirty
			snap.HasUncommitted = dirty
		}
		s.enrichSnapshot(&snap)
	}

	return snap
}

// enrichSnapshot adds branch-level git info (ahead/behind, merge status) to a snapshot.
func (s *Session) enrichSnapshot(snap *GitSnapshot) {
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
		snap.BranchMissing = true
		return
	}
	if ahead, err := gitops.CommitsAhead(project.Path, branch); err == nil {
		snap.CommitsAhead = ahead
	}
	if behind, err := gitops.CommitsBehind(project.Path, branch); err == nil {
		snap.CommitsBehind = behind
	}
	result, mergeErr := gitops.MergeTreeCheck(project.Path, branch)
	if mergeErr != nil {
		snap.MergeStatus = "unknown"
	} else if result.Clean {
		snap.MergeStatus = "clean"
	} else {
		snap.MergeStatus = "conflicts"
		snap.MergeConflictFiles = result.ConflictFiles
	}
}

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
	// Re-evaluate pending approvals under the new permission mode.
	var resolved []string
	for _, pa := range s.pendingApprovals {
		if shouldBypassPermission(s.autoApproveMode, mode, pa.toolName) {
			select {
			case pa.ch <- &claudecli.PermissionResponse{Allow: true}:
				resolved = append(resolved, pa.id)
			default:
			}
		}
	}
	s.mu.Unlock()

	for _, id := range resolved {
		s.broadcast("session.approval-auto-resolved", map[string]any{
			"sessionId":  s.ID,
			"approvalId": id,
		})
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
	// Auto-resolve pending approvals that the new mode would bypass.
	var resolved []string
	for _, pa := range s.pendingApprovals {
		if shouldBypassPermission(mode, s.permissionMode, pa.toolName) {
			select {
			case pa.ch <- &claudecli.PermissionResponse{Allow: true}:
				resolved = append(resolved, pa.id)
			default:
			}
		}
	}
	s.mu.Unlock()

	for _, id := range resolved {
		s.broadcast("session.approval-auto-resolved", map[string]any{
			"sessionId":  s.ID,
			"approvalId": id,
		})
	}
}

// Interrupt stops the current generation without killing the session.
// The event loop will receive a ResultEvent and transition back to idle.
// Any queued messages are cleared — the ResultEvent drain will find an empty queue.
func (s *Session) Interrupt() error {
	s.mu.Lock()
	if s.state != StateRunning {
		s.mu.Unlock()
		return fmt.Errorf("session is %s, not %s", string(s.state), string(StateRunning))
	}
	if s.cliSess == nil {
		s.mu.Unlock()
		return ErrNotLive
	}
	s.cliSess.Interrupt()
	s.mu.Unlock()
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

	s.broadcast("session.permission-mode-changed", map[string]any{
		"sessionId":      s.ID,
		"permissionMode": mode,
	})
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

	s.broadcast("session.tool-permission", map[string]any{
		"sessionId":  s.ID,
		"approvalId": approvalID,
		"toolName":   "ExitPlanMode",
		"input":      input,
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
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		st := s.state
		s.mu.Unlock()
		if st == StateIdle || st == StateDone || st == StateStopped {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("session %s: timed out waiting for idle (state: %s)", s.ID, s.State())
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
// mode. Superset of planSafeCategories, adding meta and question tools.
var autoSafeCategories = map[string]bool{
	"file_read": true, // Read, Glob, Grep
	"web":       true, // WebSearch, WebFetch
	"agent":     true, // Agent, ExitWorktree
	"task":      true, // TodoWrite, TodoRead
	"meta":      true, // ToolSearch, Skill
	"question":  true, // AskUserQuestion
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
	// Intercept SendMessage tool for inter-agent messaging.
	if toolName == "SendMessage" {
		return s.interceptSendMessage(input)
	}

	// ExitPlanMode is always auto-approved here — the event pipeline's
	// requestPlanReview handles the user-facing plan approval flow.
	if toolName == "ExitPlanMode" {
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

// SetAgentMessageCallback sets the callback for routing SendMessage tool invocations.
func (s *Session) SetAgentMessageCallback(cb func(senderID, targetName, content string) error) {
	s.mu.Lock()
	s.onAgentMessage = cb
	s.mu.Unlock()
}


// SpawnWorkersRequest is the parsed body from SendMessage({to: "@spawn", ...}).
type SpawnWorkersRequest struct {
	TeamName string              `json:"teamName"`
	Workers  []SpawnWorkerEntry  `json:"workers"`
}

// SpawnWorkerEntry describes a single worker to spawn.
type SpawnWorkerEntry struct {
	Name   string `json:"name"`
	Role   string `json:"role"`
	Prompt string `json:"prompt"`
}

// SetSpawnWorkersCallback sets the callback for handling worker spawn requests.
func (s *Session) SetSpawnWorkersCallback(cb func(senderID string, req SpawnWorkersRequest) error) {
	s.mu.Lock()
	s.onSpawnWorkers = cb
	s.mu.Unlock()
}

// interceptSendMessage handles the SendMessage tool by routing it through the
// team messaging system. Returns a deny response with a success-like message
// so Claude thinks the message was delivered (v1 hack — proper tool result
// interception requires claudecli-go changes).
// Also intercepts "@spawn" target for worker delegation.
// parseSendMessageInput extracts the target and body from a SendMessage tool
// call. It accepts both "content" (our preamble examples) and "message" (the
// actual tool schema), and handles "message" being either a JSON string or a
// raw JSON object (for @spawn payloads).
func parseSendMessageInput(input json.RawMessage) (to, body string, err error) {
	var parsed struct {
		To      string          `json:"to"`
		Content string          `json:"content"`
		Message json.RawMessage `json:"message"`
	}
	if err := json.Unmarshal(input, &parsed); err != nil {
		return "", "", err
	}

	// Resolve the message body: prefer "content", fall back to "message".
	body = parsed.Content
	if body == "" && len(parsed.Message) > 0 {
		// "message" may be a JSON string or an object. Try to unquote as
		// string first; if that fails, use the raw JSON bytes directly.
		var msgStr string
		if json.Unmarshal(parsed.Message, &msgStr) == nil {
			body = msgStr
		} else {
			body = string(parsed.Message)
		}
	}

	return parsed.To, body, nil
}

func (s *Session) interceptSendMessage(input json.RawMessage) (*claudecli.PermissionResponse, error) {
	to, body, err := parseSendMessageInput(input)
	if err != nil {
		slog.Warn("SendMessage parse failed", "session_id", s.ID, "error", err, "raw_input", string(input))
		return &claudecli.PermissionResponse{
			Allow:       false,
			DenyMessage: fmt.Sprintf("Failed to parse SendMessage input: %v", err),
		}, nil
	}

	slog.Debug("SendMessage intercepted", "session_id", s.ID, "to", to, "body_len", len(body))

	// Intercept @spawn for worker delegation.
	if to == "@spawn" {
		return s.interceptSpawnWorkers(body)
	}

	s.mu.Lock()
	cb := s.onAgentMessage
	s.mu.Unlock()

	if cb == nil {
		return &claudecli.PermissionResponse{
			Allow:       false,
			DenyMessage: "SendMessage is not available — this session is not part of a team.",
		}, nil
	}

	if err := cb(s.ID, to, body); err != nil {
		return &claudecli.PermissionResponse{
			Allow:       false,
			DenyMessage: fmt.Sprintf("Message delivery failed: %v", err),
		}, nil
	}

	return &claudecli.PermissionResponse{
		Allow:       false,
		DenyMessage: fmt.Sprintf("Message delivered to %q successfully.", to),
	}, nil
}

// interceptSpawnWorkers handles SendMessage to "@spawn" — routes through the
// standard approval flow so the user can approve/deny worker creation.
func (s *Session) interceptSpawnWorkers(content string) (*claudecli.PermissionResponse, error) {
	var req SpawnWorkersRequest
	if err := json.Unmarshal([]byte(content), &req); err != nil {
		slog.Warn("spawn request parse failed",
			"session_id", s.ID,
			"error", err,
			"content_len", len(content),
			"content_preview", truncate(content, 200),
		)
		return &claudecli.PermissionResponse{
			Allow:       false,
			DenyMessage: fmt.Sprintf("Failed to parse spawn request: %v. Expected JSON with teamName and workers array.", err),
		}, nil
	}
	if len(req.Workers) == 0 {
		return &claudecli.PermissionResponse{
			Allow:       false,
			DenyMessage: "Spawn request must include at least one worker.",
		}, nil
	}

	s.mu.Lock()
	cb := s.onSpawnWorkers
	s.mu.Unlock()

	if cb == nil {
		return &claudecli.PermissionResponse{
			Allow:       false,
			DenyMessage: "Worker spawning is not available for this session.",
		}, nil
	}

	// Marshal the request as the tool input for the approval UI.
	inputJSON, _ := json.Marshal(req)

	approvalID := uuid.New().String()
	ch := make(chan *claudecli.PermissionResponse, 1)

	pa := &pendingApproval{
		id:       approvalID,
		toolName: "SpawnWorkers",
		input:    inputJSON,
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

	slog.Debug("spawn workers requested", "session_id", s.ID, "workers", len(req.Workers), "approval_id", approvalID)

	s.broadcast("session.tool-permission", map[string]any{
		"sessionId":  s.ID,
		"approvalId": approvalID,
		"toolName":   "SpawnWorkers",
		"input":      json.RawMessage(inputJSON),
	})

	select {
	case resp := <-ch:
		if !resp.Allow {
			return &claudecli.PermissionResponse{
				Allow:       false,
				DenyMessage: "User denied worker creation.",
			}, nil
		}
		// User approved — create the workers.
		if err := cb(s.ID, req); err != nil {
			return &claudecli.PermissionResponse{
				Allow:       false,
				DenyMessage: fmt.Sprintf("Worker creation failed: %v", err),
			}, nil
		}
		var names []string
		for _, w := range req.Workers {
			names = append(names, w.Name)
		}
		return &claudecli.PermissionResponse{
			Allow:       false,
			DenyMessage: fmt.Sprintf(
				"Successfully spawned %d workers: %s. They are working in separate worktrees "+
					"and will message you when done. Wait for their reports before synthesizing results.",
				len(req.Workers), strings.Join(names, ", ")),
		}, nil
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
	s.stopGitRefreshTimer()

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

// MarkDone transitions the session to StateDone and marks it completed.
func (s *Session) MarkDone() error {
	s.mu.Lock()
	s.completedAt = time.Now().UTC().Format(time.RFC3339)
	s.mu.Unlock()
	if err := s.queries.SetSessionCompleted(context.Background(), s.ID); err != nil {
		slog.Error("persist session completed failed", "session_id", s.ID, "error", err)
	}
	return s.setState(StateDone)
}

// MarkMerged sets the worktreeMerged flag on a live session.
func (s *Session) MarkMerged() {
	s.mu.Lock()
	s.worktreeMerged = true
	s.mu.Unlock()
}

// MarkCompleted sets the completedAt timestamp on a live session.
func (s *Session) MarkCompleted() {
	s.mu.Lock()
	s.completedAt = time.Now().UTC().Format(time.RFC3339)
	s.mu.Unlock()
}

// nextGitVersion returns a monotonically increasing version for this session.
// Must NOT be called under s.mu.
func (s *Session) nextGitVersion() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gitVersion++
	return s.gitVersion
}

// scheduleGitRefresh debounces a lightweight git status check during a running turn.
// Each call resets the timer; the check fires once after gitRefreshDebounce of quiet.
func (s *Session) scheduleGitRefresh() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.gitRefreshTimer != nil {
		s.gitRefreshTimer.Stop()
	}

	s.gitRefreshTimer = time.AfterFunc(gitRefreshDebounce, func() {
		s.mu.Lock()
		s.gitRefreshTimer = nil
		if s.state != StateRunning {
			s.mu.Unlock()
			return
		}
		workDir := s.workDir
		merged := s.worktreeMerged
		s.mu.Unlock()

		if workDir == "" || merged {
			return
		}

		dirty, err := gitops.HasUncommittedChanges(workDir)
		if err != nil {
			slog.Warn("mid-turn git status check failed", "session_id", s.ID, "error", err)
			return
		}

		s.broadcastMidTurnGitStatus(dirty)
	})
}

// broadcastMidTurnGitStatus sends a lightweight git snapshot with dirty/uncommitted
// state. Skips expensive branch-level checks (ahead/behind, merge status).
func (s *Session) broadcastMidTurnGitStatus(dirty bool) {
	s.mu.Lock()
	s.gitVersion++
	snap := GitSnapshot{
		SessionID:        s.ID,
		State:            string(s.state),
		Connected:        true,
		HasDirtyWorktree: dirty,
		HasUncommitted:   dirty,
		WorktreeMerged:   s.worktreeMerged,
		CompletedAt:      s.completedAt,
		GitOperation:     s.gitOperation,
		Version:          s.gitVersion,
	}
	s.mu.Unlock()

	s.broadcast("session.state", snap)
}

// stopGitRefreshTimer cancels any pending mid-turn git refresh.
func (s *Session) stopGitRefreshTimer() {
	s.mu.Lock()
	if s.gitRefreshTimer != nil {
		s.gitRefreshTimer.Stop()
		s.gitRefreshTimer = nil
	}
	s.mu.Unlock()
}

// liveState returns the current in-memory state fields needed for a GitSnapshot.
func (s *Session) liveState() (state State, connected bool, worktreeMerged bool, completedAt string, gitOperation string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state, s.cliSess != nil, s.worktreeMerged, s.completedAt, s.gitOperation
}

// PendingState returns a snapshot of any pending approval/question, or nil if none.
func (s *Session) PendingState() (*WirePendingApproval, *WirePendingQuestion) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var approval *WirePendingApproval
	for _, pa := range s.pendingApprovals {
		approval = &WirePendingApproval{
			ApprovalID: pa.id,
			ToolName:   pa.toolName,
			Input:      pa.input,
		}
		break
	}

	var question *WirePendingQuestion
	for _, pq := range s.pendingQuestions {
		qs := make([]WireQuestion, len(pq.questions))
		for i, q := range pq.questions {
			opts := make([]WireQuestionOption, len(q.Options))
			for j, o := range q.Options {
				opts[j] = WireQuestionOption{Label: o.Label, Description: o.Description}
			}
			qs[i] = WireQuestion{
				Question:    q.Question,
				Header:      q.Header,
				Options:     opts,
				MultiSelect: q.MultiSelect,
			}
		}
		question = &WirePendingQuestion{
			QuestionID: pq.id,
			Questions:  qs,
		}
		break
	}

	return approval, question
}
