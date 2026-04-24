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
	"github.com/google/uuid"
	"github.com/mdjarv/agentique/backend/internal/gitops"
	"github.com/mdjarv/agentique/backend/internal/store"
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

	// Tool result truncation thresholds for DB storage.
	maxToolResultDBSize = 10_000
	toolResultKeepHead  = 4_000
	toolResultKeepTail  = 1_000

	// Debounce window for mid-turn git status refresh after write-tool results.
	gitRefreshDebounce = 500 * time.Millisecond
)

// Watchdog thresholds. Declared as vars so tests can shorten them without
// waiting minutes of real time.
//
//   - thinkingWarnAfter / thinkingFailAfter apply to ActivityThinking/Idle
//     (no tool in flight). Event silence signals a genuine model stall.
//   - toolLivenessInterval is the poll cadence during ActivityAwaitingToolResult.
//     No event-silence timeout applies; the failure signal is the CLI process dying.
//   - toolStallWarnAfter emits an informational broadcast if stdout stops moving
//     for this long during tool execution (process alive but nothing happening).
var (
	thinkingWarnAfter    = 2 * time.Minute
	thinkingFailAfter    = 5 * time.Minute
	toolLivenessInterval = 60 * time.Second
	toolStallWarnAfter   = 10 * time.Minute
)

// sessionGitState groups git/worktree-related fields of a Session.
// Protected by the owning Session's mu.
type sessionGitState struct {
	workDir         string
	gitVersion      int64
	gitRefreshTimer *time.Timer // debounce timer for mid-turn git refresh
	gitStatus       branchStatusQuerier
	worktreeMerged  bool
	gitOperation    string
}

// sessionChannelState groups channel messaging fields of a Session.
// Protected by the owning Session's mu.
type sessionChannelState struct {
	agentMessageCallbacks map[string]func(senderID, targetName, content, msgType string) error // keyed by channelID
	onSpawnWorkers        func(senderID string, req SpawnWorkersRequest) error
	onAuthorizeSpawn      func(senderID string, req SpawnWorkersRequest) (SpawnDecision, string)
	onDissolveChannel     func(senderID string) error
}

// sessionPersonaState groups persona/team fields of a Session.
// Protected by the owning Session's mu.
type sessionPersonaState struct {
	personaQuerier PersonaQuerier
	teamContext    *sessionTeamContext
}

// Session wraps a single claudecli-go interactive session.
type Session struct {
	ID        string
	ProjectID string

	ctx        context.Context
	cancelCtx  context.CancelFunc
	mu         sync.Mutex
	state      State
	cliSess    CLISession
	queryCount int
	pipeline   *EventPipeline
	queries    sessionQueries
	broadcast  func(pushType string, payload any)
	approvalState
	toolInterceptors map[string]toolInterceptor
	completedAt      string // ISO8601 timestamp or "" if not completed
	eventLoopDone    chan struct{}
	stateChangedCh   chan struct{} // buffered(1), signaled on state transitions

	// activityState mirrors the CLI's authoritative activity state, driven by
	// CLIStateChangeEvent. Watchdog behavior depends on it:
	//   ActivityThinking / ActivityIdle → event-silence timeout (model stall).
	//   ActivityAwaitingToolResult     → process liveness + stdout-stall check.
	// Protected by mu.
	activityState claudecli.ActivityState

	// toolStallWarned avoids repeatedly broadcasting the same stdout-stall
	// warning while a single tool remains stuck. Reset on activity state change.
	// Protected by mu.
	toolStallWarned bool

	// lastToolProgress is the most recent ToolProgressEvent from claudecli-go.
	// Used to enrich stall warnings with the running tool's name and elapsed time.
	// Protected by mu.
	lastToolProgress *claudecli.ToolProgressEvent

	git     sessionGitState
	channel sessionChannelState
	persona sessionPersonaState

	db *sql.DB // for transactional writes

	// Browser support: port allocated for Chrome's remote debugging.
	browserPort int
}

// PersonaQuerier runs persona queries. Decoupled from persona.Service to avoid
// import cycle (session -> persona -> session).
type PersonaQuerier interface {
	QueryForSession(ctx context.Context, profileName, teamID, askerProfileID, askerName, question string) (string, error)
}

// sessionTeamContext holds the team state needed for AskTeammate resolution.
type sessionTeamContext struct {
	agentProfileID   string
	agentProfileName string
	// teammates maps lowercase name → (profileID, teamID) for AskTeammate lookup.
	teammates map[string]teammateRef
}

type teammateRef struct {
	profileID string
	teamID    string
}

type sessionParams struct {
	id                    string
	projectID             string
	model                 string
	db                    *sql.DB // for transactions
	cliSess               CLISession
	queries               sessionQueries
	broadcast             func(pushType string, payload any)
	turnIndex             int
	workDir               string
	initialGitVersion     int64
	broadcastInitialState bool // true for Resume (frontend has session), false for Create (session.created carries state)
	gitStatus             branchStatusQuerier
}

func newSession(p sessionParams) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Session{
		ID:             p.id,
		ProjectID:      p.projectID,
		ctx:            ctx,
		cancelCtx:      cancel,
		state:          StateIdle,
		db:             p.db,
		cliSess:        p.cliSess,
		queries:        p.queries,
		broadcast:      p.broadcast,
		approvalState:  newApprovalState(),
		eventLoopDone:  make(chan struct{}),
		stateChangedCh: make(chan struct{}, 1),
		activityState:  claudecli.ActivityIdle,
		git: sessionGitState{
			workDir:    p.workDir,
			gitVersion: p.initialGitVersion,
			gitStatus:  p.gitStatus,
		},
	}
	allow := func(json.RawMessage) (*claudecli.PermissionResponse, error) {
		return &claudecli.PermissionResponse{Allow: true}, nil
	}
	s.toolInterceptors = map[string]toolInterceptor{
		// Both legacy stdio and new HTTP MCP tool names map to the same handler.
		// Only one MCP server is active per session, so only one name will fire.
		ChannelSendMessageTool:   s.interceptSendMessage,
		AgentiqueSendMessageTool: s.interceptSendMessage,
		"AskTeammate":            s.interceptAskTeammate,
		"ExitPlanMode":           allow,
		// Dev URL tools on HTTP MCP: auto-allow so they execute via /mcp.
		AgentiqueAcquireDevURLTool: allow,
		AgentiqueReleaseDevURLTool: allow,
		AgentiqueListDevURLsTool:   allow,
		// Session rename is self-affecting and reversible — safe to auto-allow.
		AgentiqueSetSessionNameTool: allow,
	}
	s.pipeline = NewEventPipeline(buildPipelineConfig(s, p))
	if p.broadcastInitialState {
		s.broadcastState(StateIdle)
	}
	if s.cliSess != nil {
		s.startEventLoop()
	}
	return s
}

// buildPipelineConfig constructs the PipelineConfig for a session's event pipeline.
func buildPipelineConfig(s *Session, p sessionParams) PipelineConfig {
	return PipelineConfig{
		SessionID:        p.id,
		Model:            p.model,
		InitialTurnIndex: p.turnIndex,
		Sink: EventSink{
			Persist: func(turnIndex, seq int, wireType string, data []byte) {
				if err := store.RetryWrite(func() error {
					return p.queries.InsertEvent(context.Background(), store.InsertEventParams{
						SessionID: p.id,
						TurnIndex: int64(turnIndex),
						Seq:       int64(seq),
						Type:      wireType,
						Data:      string(data),
					})
				}); err != nil {
					slog.Error("persist event failed", "session_id", p.id, "type", wireType, "error", err)
				}
			},
			Broadcast: p.broadcast,
		},
		OnClaudeSessionID: func(id string) {
			if err := store.RetryWrite(func() error {
				return p.queries.UpdateClaudeSessionID(context.Background(), store.UpdateClaudeSessionIDParams{
					ClaudeSessionID: sqlNullString(id),
					ID:              p.id,
				})
			}); err != nil {
				slog.Error("persist claude session ID failed", "session_id", p.id, "error", err)
			}
		},
		OnPlanTransition: s.transitionPlanMode,
		OnExitPlanMode: func(input json.RawMessage) {
			s.mu.Lock()
			aam := s.autoApproveMode
			s.mu.Unlock()
			if aam == "fullAuto" || aam == "auto" {
				s.transitionPlanMode("default")
			} else {
				go s.requestPlanReview(input)
			}
		},
		OnSendMessage: func(toolUseID, targetName, content, msgType string) {
			s.mu.Lock()
			cbs := make(map[string]func(string, string, string, string) error, len(s.channel.agentMessageCallbacks))
			for k, v := range s.channel.agentMessageCallbacks {
				cbs[k] = v
			}
			s.mu.Unlock()
			if len(cbs) == 0 {
				slog.Debug("pipeline: SendMessage ignored, no channel callback",
					"session_id", s.ID, "target", targetName)
				return
			}
			for chID, cb := range cbs {
				if err := cb(s.ID, targetName, content, msgType); err == nil {
					return
				} else if !strings.Contains(err.Error(), "no channel member named") {
					slog.Warn("pipeline: SendMessage routing failed",
						"session_id", s.ID, "channel", chID, "target", targetName, "error", err)
					return
				}
			}
			slog.Warn("pipeline: SendMessage target not found in any channel",
				"session_id", s.ID, "target", targetName)
		},
		OnWriteToolResult: s.scheduleGitRefresh,
		OnTurnComplete: func() {
			if s.State() != StateIdle {
				if err := s.setState(StateIdle); err != nil {
					slog.Error("state transition failed", "session_id", s.ID, "error", err)
				}
			}
		},
		OnFatalError: func(err error) {
			if stErr := s.setState(StateFailed); stErr != nil {
				slog.Error("state transition failed", "session_id", s.ID, "error", stErr)
			}
		},
		OnActivityEvent: func(wireEvent any) {
			item := ActivityItem{
				Kind:      "event",
				SourceID:  s.ID,
				CreatedAt: time.Now().UTC().Format("2006-01-02T15:04:05.000"),
			}
			// Fetch session name from DB (low-frequency: only result/error events).
			if dbSess, err := s.queries.GetSession(context.Background(), s.ID); err == nil {
				item.SourceName = dbSess.Name
			}
			switch e := wireEvent.(type) {
			case WireResultEvent:
				item.ItemID = fmt.Sprintf("ev-%d", time.Now().UnixMilli())
				item.EventType = "result"
			case WireErrorEvent:
				item.ItemID = fmt.Sprintf("ev-%d", time.Now().UnixMilli())
				item.Content = truncate(e.Content, 200)
				item.EventType = "error"
			default:
				return
			}
			s.broadcast("project.activity-item", item)
		},
	}
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

// BrowserPort returns the allocated Chrome debugging port for this session.
func (s *Session) BrowserPort() int { return s.browserPort }

// SetBrowserPort stores the allocated Chrome debugging port.
func (s *Session) SetBrowserPort(port int) { s.browserPort = port }

// ReconnectMCP asks Claude Code to reconnect a named MCP server (fire-and-forget).
func (s *Session) ReconnectMCP(serverName string) error {
	s.mu.Lock()
	cli := s.cliSess
	s.mu.Unlock()
	if cli == nil {
		return fmt.Errorf("session not connected")
	}
	return cli.ReconnectMCPServer(serverName)
}

// ReconnectMCPWait reconnects a named MCP server and blocks until it reports ready
// (or the timeout expires). Use this when the caller needs to know the server is
// available before proceeding (e.g. injecting a prompt that references its tools).
func (s *Session) ReconnectMCPWait(serverName string, timeout time.Duration) error {
	s.mu.Lock()
	cli := s.cliSess
	s.mu.Unlock()
	if cli == nil {
		return fmt.Errorf("session not connected")
	}
	return cli.ReconnectMCPServerWait(serverName, timeout)
}

// QueryCount returns the number of queries sent to this session.
func (s *Session) QueryCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.queryCount
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
	messageID := uuid.New().String()
	s.pipeline.PushPendingMessage(messageID)
	wireEvent := WireUserMessageEvent{Type: "user_message", Content: prompt, MessageID: messageID, Attachments: attachments}
	if data, err := json.Marshal(wireEvent); err == nil {
		if err := store.RetryWrite(func() error {
			return s.queries.InsertEvent(context.Background(), store.InsertEventParams{
				SessionID: s.ID,
				TurnIndex: int64(turnIdx),
				Seq:       int64(seq),
				Type:      "user_message",
				Data:      string(data),
			})
		}); err != nil {
			slog.Error("persist user_message event failed", "session_id", s.ID, "error", err)
		}
	}
	s.broadcast("session.event", PushSessionEvent{SessionID: s.ID, Event: wireEvent})
	return nil
}

// Query sends a prompt (with optional images) to the Claude session and starts streaming events.
// The caller's ctx is NOT used — all DB writes and CLI operations use background
// contexts so they complete even if the triggering WS connection disconnects.
func (s *Session) Query(_ context.Context, prompt string, attachments []QueryAttachment) error {
	cli, wasCompleted, wasMerged, err := s.validateAndTransition()
	if err != nil {
		return err
	}

	turnIndex := s.pipeline.AdvanceTurn()
	s.persistQueryStart(turnIndex, wasCompleted, wasMerged, prompt, attachments)
	s.broadcastState(StateRunning)

	turnPayload := PushTurnStarted{SessionID: s.ID, Prompt: prompt}
	if len(attachments) > 0 {
		turnPayload.Attachments = attachments
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

// validateAndTransition atomically checks the state transition, updates in-memory
// state, and returns the CLI session plus prior flags needed for DB cleanup.
func (s *Session) validateAndTransition() (cli CLISession, wasCompleted, wasMerged bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = validateTransition(s.state, StateRunning, s.ID); err != nil {
		return nil, false, false, err
	}
	if s.cliSess == nil {
		return nil, false, false, fmt.Errorf("session %s: CLI session not connected", s.ID)
	}
	s.state = StateRunning
	s.queryCount++
	wasCompleted = s.completedAt != ""
	s.completedAt = ""
	wasMerged = s.git.worktreeMerged
	s.git.worktreeMerged = false
	return s.cliSess, wasCompleted, wasMerged, nil
}

// persistQueryStart writes the running state, resets completed/merged flags in the
// database, and persists the prompt as seq 0 of the new turn.
// All writes are wrapped in a single transaction for atomicity.
func (s *Session) persistQueryStart(turnIndex int, wasCompleted, wasMerged bool, prompt string, attachments []QueryAttachment) {
	promptPayload := map[string]any{"prompt": prompt}
	if len(attachments) > 0 {
		promptPayload["attachments"] = attachments
	}
	promptData, err := json.Marshal(promptPayload)
	if err != nil {
		slog.Error("marshal prompt failed", "session_id", s.ID, "error", err)
		return
	}

	// Resolve head SHA outside the transaction (it's a git operation, not a DB op).
	var headSHA string
	if wasMerged {
		if project, pErr := s.queries.GetProject(context.Background(), s.ProjectID); pErr == nil {
			headSHA, _ = gitops.HeadCommitHash(project.Path)
		}
	}

	txErr := store.RetryWrite(func() error {
		return store.RunInTx(s.db, func(q *store.Queries) error {
			ctx := context.Background()
			if err := q.UpdateSessionState(ctx, store.UpdateSessionStateParams{
				State: string(StateRunning),
				ID:    s.ID,
			}); err != nil {
				return err
			}
			if wasCompleted {
				if err := q.UnsetSessionCompleted(ctx, s.ID); err != nil {
					return err
				}
			}
			if wasMerged {
				if err := q.UnsetWorktreeMerged(ctx, s.ID); err != nil {
					return err
				}
				if headSHA != "" {
					if err := q.UpdateWorktreeBaseSHA(ctx, store.UpdateWorktreeBaseSHAParams{
						WorktreeBaseSha: sql.NullString{String: headSHA, Valid: true},
						ID:              s.ID,
					}); err != nil {
						return err
					}
				}
			}
			return q.InsertEvent(ctx, store.InsertEventParams{
				SessionID: s.ID,
				TurnIndex: int64(turnIndex),
				Seq:       0,
				Type:      "prompt",
				Data:      string(promptData),
			})
		})
	})
	if txErr != nil {
		slog.Error("persist query start failed", "session_id", s.ID, "error", txErr)
	}
	s.pipeline.SetSeq(1)
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
		watchdog := time.NewTimer(thinkingWarnAfter)
		defer watchdog.Stop()
		var warned bool
		var lastEventType string
		var lastExit *claudecli.CLIExitEvent
		lastEventTime := time.Now()

		for {
			select {
			case <-s.ctx.Done():
				return
			case event, ok := <-events:
				if !ok {
					s.mu.Lock()
					st := s.state
					s.mu.Unlock()
					// Skip if already in a terminal state so we don't mask the real reason.
					if st == StateDone || st == StateFailed || st == StateStopped {
						return
					}
					// If the CLI died while actively running, treat as unexpected failure.
					// Prefer the CLIExitEvent's reason (landed immediately before close)
					// for an actionable fatal message.
					if st == StateRunning {
						msg := formatExitEvent(lastExit)
						slog.Error("CLI process died while running",
							"session_id", s.ID,
							"message", msg,
						)
						s.broadcast("session.event", PushSessionEvent{
							SessionID: s.ID,
							Event: WireErrorEvent{
								Type:    "error",
								Content: msg,
								Fatal:   true,
							},
						})
						if err := s.setState(StateFailed); err != nil {
							slog.Error("state transition failed", "session_id", s.ID, "error", err)
						}
						return
					}
					// Clean close from idle/merging — transition to done.
					// Set completedAt in memory first so broadcastState includes it.
					s.MarkCompleted()
					if err := s.setState(StateDone); err != nil {
						slog.Error("state transition failed", "session_id", s.ID, "error", err)
						return
					}
					// Persist completedAt to DB only after successful transition.
					if err := s.queries.SetSessionCompleted(context.Background(), s.ID); err != nil {
						slog.Error("persist session completed failed", "session_id", s.ID, "error", err)
					}
					return
				}
				lastEventType = fmt.Sprintf("%T", event)
				lastEventTime = time.Now()
				if ex, ok := event.(*claudecli.CLIExitEvent); ok {
					lastExit = ex
				}
				nextInterval := s.observeActivity(event)
				watchdog.Reset(nextInterval)
				warned = false
				s.safeProcessEvent(event)
			case <-watchdog.C:
				s.mu.Lock()
				st := s.state
				waitingForUser := len(s.pendingApprovals) > 0 || len(s.pendingQuestions) > 0
				act := s.activityState
				s.mu.Unlock()

				if st != StateRunning || waitingForUser {
					watchdog.Reset(thinkingWarnAfter)
					continue
				}

				if act == claudecli.ActivityAwaitingToolResult {
					// Tool executing: event silence is expected. Fail only if the CLI
					// process has died, or if a stdout-stall probe reveals the read
					// loop is wedged (process alive but not responding to pings).
					if !cliAlive(s.cliSess) {
						slog.Error("watchdog: CLI process not alive while awaiting tool result", "session_id", s.ID)
						s.broadcast("session.event", PushSessionEvent{
							SessionID: s.ID,
							Event: WireErrorEvent{
								Type:    "error",
								Content: "CLI process exited while a tool was running",
								Fatal:   true,
							},
						})
						if err := s.setState(StateFailed); err != nil {
							slog.Error("state transition failed", "session_id", s.ID, "error", err)
						}
						return
					}
					if s.maybeHandleToolStall() == toolStallFatal {
						if err := s.setState(StateFailed); err != nil {
							slog.Error("state transition failed", "session_id", s.ID, "error", err)
						}
						return
					}
					watchdog.Reset(toolLivenessInterval)
					continue
				}

				// Thinking / Idle: event-silence timeout applies.
				sinceLastEvent := time.Since(lastEventTime)
				if sinceLastEvent < thinkingWarnAfter {
					// Timer fired but the window hasn't actually elapsed (possible
					// when a previous reset races with a tick). Re-arm for remainder.
					watchdog.Reset(thinkingWarnAfter - sinceLastEvent)
					continue
				}
				if !warned {
					warned = true
					slog.Warn("watchdog: no activity",
						"session_id", s.ID,
						"last_event_type", lastEventType,
						"since_last_event", sinceLastEvent.Round(time.Second),
					)
					s.broadcast("session.event", PushSessionEvent{
						SessionID: s.ID,
						Event: WireErrorEvent{
							Type:    "error",
							Content: "session may be unresponsive — no activity for 2 minutes",
							Fatal:   false,
						},
					})
					watchdog.Reset(thinkingFailAfter - thinkingWarnAfter)
					continue
				}
				slog.Error("watchdog timeout, marking session failed",
					"session_id", s.ID,
					"last_event_type", lastEventType,
					"since_last_event", sinceLastEvent.Round(time.Second),
				)
				s.setState(StateFailed)
				return
			}
		}
	}()
}

// observeActivity syncs the local activity-state mirror from an incoming event
// and returns the watchdog interval appropriate for the resulting state.
//
// The authoritative signal is claudecli-go's CLIStateChangeEvent, emitted
// immediately before the triggering event. This replaces earlier tool_use /
// tool_result inference, which was correct but duplicated work the CLI wrapper
// now does for us.
func (s *Session) observeActivity(event claudecli.Event) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch e := event.(type) {
	case *claudecli.CLIStateChangeEvent:
		if e.State != s.activityState {
			s.toolStallWarned = false
			s.lastToolProgress = nil
		}
		s.activityState = e.State
	case *claudecli.ToolProgressEvent:
		s.lastToolProgress = e
	}
	if s.activityState == claudecli.ActivityAwaitingToolResult {
		return toolLivenessInterval
	}
	return thinkingWarnAfter
}

// toolStallResult is returned by maybeHandleToolStall. "fatal" means the
// watchdog escalated via Ping and confirmed the CLI's read loop is wedged —
// the caller should fail the session.
type toolStallResult int

const (
	toolStallNone   toolStallResult = iota // no stall detected or duplicate warning suppressed
	toolStallWarned                        // emitted a non-fatal info broadcast
	toolStallFatal                         // Ping failed; CLI readLoop is wedged
)

const watchdogPingTimeout = 10 * time.Second

// maybeHandleToolStall detects stdout stalls during tool execution and either
// emits an informational warning (process + readLoop alive) or escalates to a
// fatal signal (readLoop wedged, verified via Ping). Deduplicates repeat
// warnings for the same stall episode via toolStallWarned.
func (s *Session) maybeHandleToolStall() toolStallResult {
	info := s.cliSess.ProcessInfo()
	if info.LastStdoutAt.IsZero() {
		return toolStallNone
	}
	silent := time.Since(info.LastStdoutAt)
	if silent < toolStallWarnAfter {
		return toolStallNone
	}

	s.mu.Lock()
	if s.toolStallWarned {
		s.mu.Unlock()
		return toolStallNone
	}
	s.toolStallWarned = true
	progress := s.lastToolProgress
	s.mu.Unlock()

	// Escalation: probe the CLI's read loop. A healthy CLI answers even if
	// the running tool is wedged; if the ping fails, the readLoop is wedged
	// too — that's a zombie and the session cannot recover.
	if err := s.cliSess.Ping(watchdogPingTimeout); err != nil {
		slog.Error("watchdog: ping failed during stdout stall",
			"session_id", s.ID,
			"stdout_silent", silent.Round(time.Second),
			"error", err,
		)
		s.broadcast("session.event", PushSessionEvent{
			SessionID: s.ID,
			Event: WireErrorEvent{
				Type:    "error",
				Content: fmt.Sprintf("CLI read loop unresponsive (ping: %v) — session cannot recover", err),
				Fatal:   true,
			},
		})
		return toolStallFatal
	}

	content := fmt.Sprintf("tool still running — CLI stdout silent for %s", silent.Round(time.Second))
	if progress != nil && progress.ToolName != "" {
		content = fmt.Sprintf("tool %s still running (%s) — CLI stdout silent for %s",
			progress.ToolName, progress.Elapsed.Round(time.Second), silent.Round(time.Second))
	}
	slog.Warn("watchdog: stdout stalled while tool running",
		"session_id", s.ID,
		"stdout_silent", silent.Round(time.Second),
	)
	s.broadcast("session.event", PushSessionEvent{
		SessionID: s.ID,
		Event: WireErrorEvent{
			Type:    "error",
			Content: content,
			Fatal:   false,
		},
	})
	return toolStallWarned
}

// formatExitEvent renders a user-facing description of why the CLI exited.
// Used to give the fatal broadcast an actionable message instead of the
// generic "CLI process exited unexpectedly".
func formatExitEvent(e *claudecli.CLIExitEvent) string {
	if e == nil {
		return "CLI process exited unexpectedly while running"
	}
	switch e.Reason {
	case claudecli.ExitReasonNormal:
		return "CLI process exited (clean)"
	case claudecli.ExitReasonKilled:
		if e.Signal != "" {
			return fmt.Sprintf("CLI process killed by %s", e.Signal)
		}
		return "CLI process killed"
	case claudecli.ExitReasonCrashed:
		msg := fmt.Sprintf("CLI process crashed (exit code %d)", e.ExitCode)
		if e.Err != nil {
			msg += fmt.Sprintf(": %v", e.Err)
		}
		return msg
	case claudecli.ExitReasonContextCanceled:
		return "CLI process terminated (context canceled)"
	}
	return fmt.Sprintf("CLI process exited (reason: %s, code: %d)", e.Reason, e.ExitCode)
}

// safeProcessEvent wraps pipeline.ProcessEvent with panic recovery so a single
// malformed event can't kill the event loop goroutine.
func (s *Session) safeProcessEvent(event claudecli.Event) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic in processEvent", "session_id", s.ID, "panic", r)
			s.broadcast("session.event", PushSessionEvent{
				SessionID: s.ID,
				Event: WireErrorEvent{
					Type:    "error",
					Content: fmt.Sprintf("internal error processing event: %v", r),
					Fatal:   false,
				},
			})
		}
	}()
	s.pipeline.ProcessEvent(event)
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
	s.pipeline.StopPulseTimer()

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
// If the session is already done (e.g., CLI exited cleanly), it ensures completedAt
// is set and broadcasts without attempting a state transition.
func (s *Session) MarkDone() error {
	s.mu.Lock()
	alreadyDone := s.state == StateDone
	alreadyCompleted := s.completedAt != ""
	if !alreadyCompleted {
		s.completedAt = time.Now().UTC().Format(time.RFC3339)
	}
	s.mu.Unlock()

	if !alreadyCompleted {
		if err := s.queries.SetSessionCompleted(context.Background(), s.ID); err != nil {
			slog.Error("persist session completed failed", "session_id", s.ID, "error", err)
		}
	}

	if alreadyDone {
		if !alreadyCompleted {
			// completedAt was missing — broadcast so the frontend picks it up.
			s.broadcastState(StateDone)
		}
		return nil
	}
	return s.setState(StateDone)
}

// MarkCompleted sets the completedAt timestamp on a live session.
func (s *Session) MarkCompleted() {
	s.mu.Lock()
	s.completedAt = time.Now().UTC().Format(time.RFC3339)
	s.mu.Unlock()
}

// liveState returns the current in-memory state fields needed for a GitSnapshot.
func (s *Session) liveState() (state State, connected bool, worktreeMerged bool, completedAt string, gitOperation string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state, s.cliSess != nil, s.git.worktreeMerged, s.completedAt, s.git.gitOperation
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
