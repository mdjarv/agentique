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

	"github.com/allbin/agentkit/runtime"
	"github.com/allbin/agentkit/sqliteops"
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

// syntheticApproval is an agentique-side pending approval that doesn't pass
// through the runtime approval pump. Used for the plan-review dance (where
// agentique synthesizes an approval after the pipeline observes ExitPlanMode
// in the event stream) and for SpawnWorkers (where the interceptor returns
// after a user prompt resolves).
type syntheticApproval struct {
	id       string
	toolName string
	input    json.RawMessage
	ch       chan *runtime.Decision
}

const (
	// Tool result truncation thresholds for DB storage.
	maxToolResultDBSize = 10_000
	toolResultKeepHead  = 4_000
	toolResultKeepTail  = 1_000

	// Debounce window for mid-turn git status refresh after write-tool results.
	gitRefreshDebounce = 500 * time.Millisecond
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

// Session is an agentique-side session. It wraps a runtime.Session and adds
// persistence, git/worktree state, channel and persona context, and a small
// pool of synthetic approvals (plan-review and spawn UI prompts) that don't
// pass through the runtime approval pump.
type Session struct {
	ID        string
	ProjectID string

	rt *runtime.Session // runtime owns lifecycle/watchdog/approval pump

	// cli is the raw CLISession runtime is driving. We keep a reference so
	// agentique can inject silent channel-context / pending-delivery messages
	// straight to the CLI, bypassing both runtime's state-check and the
	// pipeline. Captured by Manager via the capturing connector.
	cli runtime.CLISession

	ctx       context.Context
	cancelCtx context.CancelFunc

	mu             sync.Mutex
	state          State
	queryCount     int
	pipeline       *EventPipeline
	queries        sessionQueries
	broadcast      func(pushType string, payload any)
	completedAt    string // ISO8601 timestamp or "" if not completed
	stateChangedCh chan struct{} // buffered(1), signaled on state transitions

	// pendingMessages buffers user messages sent while a turn is running on a
	// provider without native mid-turn injection (codex). Flushed as a fresh
	// turn at the next idle boundary. Guarded by mu. See QueuePendingMessage /
	// flushPendingMessages. Always empty for providers with native mid-turn
	// send (claude), which inject straight into the current turn.
	pendingMessages []pendingMessage

	// approval/permission state. Auto-approve mode and permission mode are
	// kept locally in addition to runtime so we can drive agentique's
	// "auto" safe-tool bypass logic and persist permission mode changes.
	autoApproveMode    string // "manual", "auto", "fullAuto"
	permissionMode     string // "default", "plan", "acceptEdits"
	syntheticApprovals map[string]*syntheticApproval

	git     sessionGitState
	channel sessionChannelState
	persona sessionPersonaState

	db *sql.DB // for transactional writes

	// Browser support: port allocated for Chrome's remote debugging.
	browserPort int

	// recallFn, when wired by the Manager, returns a one-time task-relevant memory
	// recall block to prepend to this session's first turn (query-relevant recall).
	// recallInjected gates it to a single lookup per live session instance — create
	// or resume — so cost stays bounded. Both guarded by mu.
	recallFn       func(ctx context.Context, prompt string) string
	recallInjected bool
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

// sessionParams collects the inputs to newSession. Internal to the session
// package — Manager wires real values from CreateParams / ResumeParams.
type sessionParams struct {
	id                string
	projectID         string
	model             string
	db                *sql.DB
	queries           sessionQueries
	broadcast         func(pushType string, payload any)
	turnIndex         int
	workDir           string
	initialGitVersion int64
	gitStatus         branchStatusQuerier
}

// newSession constructs an agentique Session shell. The runtime.Session is
// attached afterward via setRuntime once Manager has connected the CLI; this
// preserves the constraint that interceptors / broadcast hooks reference the
// final Session pointer.
func newSession(p sessionParams) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Session{
		ID:                 p.id,
		ProjectID:          p.projectID,
		ctx:                ctx,
		cancelCtx:          cancel,
		state:              StateIdle,
		db:                 p.db,
		queries:            p.queries,
		broadcast:          p.broadcast,
		stateChangedCh:     make(chan struct{}, 1),
		autoApproveMode:    "manual",
		permissionMode:     "default",
		syntheticApprovals: make(map[string]*syntheticApproval),
		git: sessionGitState{
			workDir:    p.workDir,
			gitVersion: p.initialGitVersion,
			gitStatus:  p.gitStatus,
		},
	}
	s.pipeline = NewEventPipeline(buildPipelineConfig(s, p))
	return s
}

// setRuntime attaches a runtime.Session and the underlying CLISession to this
// agentique session. Manager calls this after a successful runtime Create /
// Resume / Reconnect.
func (s *Session) setRuntime(rt *runtime.Session, cli runtime.CLISession) {
	s.mu.Lock()
	s.rt = rt
	s.cli = cli
	s.mu.Unlock()
}

// directSendMessage injects prompt directly into the underlying CLISession,
// bypassing both the runtime state-check and the agentique pipeline. Used for
// silent channel-context injection and pending-delivery replay.
func (s *Session) directSendMessage(prompt string) error {
	s.mu.Lock()
	cli := s.cli
	s.mu.Unlock()
	if cli == nil {
		return ErrNotLive
	}
	return cli.SendMessage(context.Background(), prompt)
}

// agentiqueInterceptors returns the tool interceptor map used at runtime
// session construction. Handlers return *runtime.Decision directly — no
// conversion shim from claudecli's permission response shape is needed.
func (s *Session) agentiqueInterceptors() map[string]runtime.ToolInterceptor {
	allow := func(_ context.Context, _ json.RawMessage) (*runtime.Decision, error) {
		return &runtime.Decision{Allow: true}, nil
	}
	intercept := func(fn func(json.RawMessage) (*runtime.Decision, error)) runtime.ToolInterceptor {
		return func(_ context.Context, input json.RawMessage) (*runtime.Decision, error) {
			return fn(input)
		}
	}
	return map[string]runtime.ToolInterceptor{
		ChannelSendMessageTool:      intercept(s.interceptSendMessage),
		AgentiqueSendMessageTool:    intercept(s.interceptSendMessage),
		"AskTeammate":               intercept(s.interceptAskTeammate),
		"ExitPlanMode":              allow,
		AgentiqueAcquireDevURLTool:  allow,
		AgentiqueReleaseDevURLTool:  allow,
		AgentiqueListDevURLsTool:    allow,
		AgentiqueSetSessionNameTool: allow,
		AgentiqueMemoryAddTool:      allow,
		AgentiqueMemorySearchTool:   allow,
		AgentiqueMemoryFlagTool:     allow,
	}
}

// buildPipelineConfig constructs the PipelineConfig for a session's event pipeline.
func buildPipelineConfig(s *Session, p sessionParams) PipelineConfig {
	return PipelineConfig{
		SessionID:        p.id,
		Model:            p.model,
		InitialTurnIndex: p.turnIndex,
		Sink: EventSink{
			Persist: func(turnIndex, seq int, wireType string, data []byte) {
				if err := sqliteops.RetryWrite(func() error {
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
			if err := sqliteops.RetryWrite(func() error {
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
			// Runtime drives Running→Idle from ResultEvent; agentique just
			// observes via the broadcast hook. Nothing to do here.
		},
		OnFatalError: func(err error) {
			// Runtime doesn't observe Fatal ErrorEvents — agentique's pipeline
			// is the only place that classifies them. Mirror to StateFailed.
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

// ClaudeSessionID returns the Claude CLI session ID, if available.
func (s *Session) ClaudeSessionID() string {
	return s.pipeline.ClaudeSessionID()
}

// BrowserPort returns the allocated Chrome debugging port for this session.
func (s *Session) BrowserPort() int { return s.browserPort }

// SetBrowserPort stores the allocated Chrome debugging port.
func (s *Session) SetBrowserPort(port int) { s.browserPort = port }

// claudeUnderlying is satisfied by the agentkit claude adapter's CLISession.
// It surfaces the *claudecli.Session so agentique can reach claude-specific
// features the runtime contract does not proxy (live MCP reconnect).
type claudeUnderlying interface {
	Underlying() *claudecli.Session
}

// claudeSession returns the underlying *claudecli.Session when the active
// provider is the claude adapter; nil otherwise.
func (s *Session) claudeSession() *claudecli.Session {
	s.mu.Lock()
	cli := s.cli
	s.mu.Unlock()
	if u, ok := cli.(claudeUnderlying); ok {
		return u.Underlying()
	}
	return nil
}

// ReconnectMCP asks Claude Code to reconnect a named MCP server (fire-and-forget).
// Only supported when the session's provider is "claude".
func (s *Session) ReconnectMCP(serverName string) error {
	cli := s.claudeSession()
	if cli == nil {
		return fmt.Errorf("session not connected or provider does not support MCP reconnect")
	}
	return cli.ReconnectMCPServer(serverName)
}

// ReconnectMCPWait reconnects a named MCP server and blocks until it reports
// ready (or the timeout expires).
func (s *Session) ReconnectMCPWait(serverName string, timeout time.Duration) error {
	cli := s.claudeSession()
	if cli == nil {
		return fmt.Errorf("session not connected or provider does not support MCP reconnect")
	}
	return cli.ReconnectMCPServerWait(serverName, timeout)
}

// QueryCount returns the number of queries sent to this session.
func (s *Session) QueryCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.queryCount
}

// SendMessage injects a user message mid-turn via the runtime SendMessage API.
// Only valid while the session is Running.
func (s *Session) SendMessage(prompt string, attachments []QueryAttachment) error {
	s.mu.Lock()
	rt := s.rt
	s.mu.Unlock()
	if rt == nil {
		return ErrNotLive
	}

	turnIdx, seq := s.pipeline.AllocSeq()

	atts, err := toRuntimeAttachments(attachments)
	if err != nil {
		return fmt.Errorf("parse attachments: %w", err)
	}
	if err := rt.SendMessage(context.Background(), prompt, atts...); err != nil {
		return err
	}

	messageID := uuid.New().String()
	s.pipeline.PushPendingMessage(messageID)
	wireEvent := WireUserMessageEvent{Type: "user_message", Content: prompt, MessageID: messageID, Attachments: attachments}
	if data, err := json.Marshal(wireEvent); err == nil {
		if err := sqliteops.RetryWrite(func() error {
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
	s.broadcastSessionEvent(wireEvent)
	return nil
}

// pendingMessage is a user message buffered during a running turn for providers
// that lack native mid-turn injection. Replayed as a fresh turn at the next
// idle boundary by flushPendingMessages.
type pendingMessage struct {
	id          string
	prompt      string
	attachments []QueryAttachment
}

// supportsNativeMidTurn reports whether the live provider can inject a message
// into the running turn itself (claude). When false (codex), agentique emulates
// the feature by buffering the message and replaying it as a fresh turn at the
// next idle boundary — see QueuePendingMessage / flushPendingMessages.
func (s *Session) supportsNativeMidTurn() bool {
	s.mu.Lock()
	cli := s.cli
	s.mu.Unlock()
	if cli == nil {
		return false
	}
	return cli.Capabilities().MidTurnSendMessage
}

// QueuePendingMessage buffers a user message sent while the session is running
// on a provider without native mid-turn injection. The message is echoed to the
// UI immediately as a transient "queued" bubble and replayed as a fresh turn
// when the session next goes idle. Returns false if the session is no longer
// running, in which case the caller should send it as a new turn instead. The
// state check and append are atomic against flushPendingMessages so a turn that
// completes concurrently can't strand the message.
//
// The echo is intentionally not persisted: the durable record is the prompt
// written when flushPendingMessages replays it via Query. As with claude's
// native mid-turn buffer, a server restart before the flush drops the queued
// message — it was never accepted by the provider.
func (s *Session) QueuePendingMessage(prompt string, attachments []QueryAttachment) bool {
	messageID := uuid.New().String()
	s.mu.Lock()
	if s.state != StateRunning {
		s.mu.Unlock()
		return false
	}
	s.pendingMessages = append(s.pendingMessages, pendingMessage{id: messageID, prompt: prompt, attachments: attachments})
	s.mu.Unlock()

	wireEvent := WireUserMessageEvent{Type: "user_message", Content: prompt, MessageID: messageID, Attachments: attachments, Queued: true}
	s.broadcastSessionEvent(wireEvent)
	return true
}

// broadcastSessionEvent broadcasts a session.event for this session. All
// Session-originated session.event emissions (mid-turn echoes, queued echoes,
// runtime-bridge errors) route through the pipeline's single serialized emitter
// so the per-session wire sequence the frontend tracks stays gap-free and
// correctly ordered across emission sites and goroutines.
func (s *Session) broadcastSessionEvent(wireEvent any) {
	if s.pipeline != nil {
		s.pipeline.EmitSessionEvent(wireEvent)
		return
	}
	// No pipeline (not expected for a live session): emit unsequenced; the
	// frontend skips gap/dedup checks for seq 0.
	s.broadcast("session.event", PushSessionEvent{SessionID: s.ID, Event: wireEvent})
}

// flushPendingMessages replays buffered mid-turn messages as a single fresh turn
// once the session is idle. Called from the runtime state-change bridge on every
// transition into StateIdle; a no-op when the queue is empty (the common case,
// and the only case for providers with native mid-turn injection). Buffered
// messages are coalesced into one prompt so delivery is a single turn — this
// sidesteps races between per-message turns and matches the UI, which clears the
// whole queued-preview set when the replayed turn starts.
func (s *Session) flushPendingMessages() {
	s.mu.Lock()
	if len(s.pendingMessages) == 0 || s.state != StateIdle {
		s.mu.Unlock()
		return
	}
	queued := s.pendingMessages
	s.pendingMessages = nil
	s.mu.Unlock()

	prompt, attachments := coalescePending(queued)
	if err := s.Query(context.Background(), prompt, attachments); err != nil {
		slog.Error("flush pending messages failed", "session_id", s.ID, "error", err)
		// Don't lose the user's input — requeue at the front for the next idle
		// transition (e.g. after a resume).
		s.mu.Lock()
		s.pendingMessages = append(queued, s.pendingMessages...)
		s.mu.Unlock()
	}
}

// coalescePending joins buffered messages into a single prompt and attachment
// set, preserving FIFO order.
func coalescePending(msgs []pendingMessage) (string, []QueryAttachment) {
	if len(msgs) == 1 {
		return msgs[0].prompt, msgs[0].attachments
	}
	prompts := make([]string, len(msgs))
	var atts []QueryAttachment
	for i, m := range msgs {
		prompts[i] = m.prompt
		atts = append(atts, m.attachments...)
	}
	return strings.Join(prompts, "\n\n"), atts
}

// Query sends a prompt (with optional images) to the Claude session and starts streaming events.
// recallTimeout bounds the one-time recall lookup so a slow or hung vector backend
// can never stall the first turn (reliability-first): on timeout we inject nothing.
const recallTimeout = 3 * time.Second

// SetRecallFn wires the one-time task-relevant memory recall callback. The Manager
// binds the project; the Session fires it once, on its first turn. nil disables it.
func (s *Session) SetRecallFn(fn func(ctx context.Context, prompt string) string) {
	s.mu.Lock()
	s.recallFn = fn
	s.mu.Unlock()
}

// injectRecall prepends a one-time, task-relevant memory recall block to the first
// turn's prompt. It fires at most once per live session instance (create or resume),
// bounding cost to a single lookup. Best-effort: a disabled brain, a slow/failed
// recall, or zero relevant facts returns the prompt unchanged.
func (s *Session) injectRecall(prompt string) string {
	s.mu.Lock()
	fn := s.recallFn
	if fn == nil || s.recallInjected {
		s.mu.Unlock()
		return prompt
	}
	s.recallInjected = true // bound cost: one lookup per instance, even on miss/failure
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), recallTimeout)
	defer cancel()
	block := fn(ctx, prompt)
	if strings.TrimSpace(block) == "" {
		return prompt
	}
	return block + "\n\n" + prompt
}

func (s *Session) Query(_ context.Context, prompt string, attachments []QueryAttachment) error {
	rt, wasCompleted, wasMerged, err := s.validateAndPrepareQuery()
	if err != nil {
		return err
	}

	// Inject task-relevant recall only after validation passes, so a rejected
	// query doesn't consume the one-shot. The augmented prompt is persisted, sent
	// to the model, and broadcast — so the recalled facts are visible in the
	// transcript and seen by the agent.
	prompt = s.injectRecall(prompt)

	turnIndex := s.pipeline.AdvanceTurn()
	s.persistQueryStart(turnIndex, wasCompleted, wasMerged, prompt, attachments)

	turnPayload := PushTurnStarted{SessionID: s.ID, Prompt: prompt}
	if len(attachments) > 0 {
		turnPayload.Attachments = attachments
	}
	s.broadcast("session.turn-started", turnPayload)

	atts, berr := toRuntimeAttachments(attachments)
	if berr != nil {
		return fmt.Errorf("parse attachments: %w", berr)
	}
	queryErr := rt.Query(context.Background(), prompt, atts...)
	if queryErr != nil {
		if stErr := s.setState(StateFailed); stErr != nil {
			slog.Error("state transition failed", "session_id", s.ID, "error", stErr)
		}
		return queryErr
	}
	return nil
}

// validateAndPrepareQuery checks the runtime is connected and preserves prior
// flags (completed, merged) for cleanup. Runtime drives the Idle→Running
// transition; agentique just resets transient flags and bumps queryCount.
func (s *Session) validateAndPrepareQuery() (rt *runtime.Session, wasCompleted, wasMerged bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rt == nil {
		return nil, false, false, ErrNotLive
	}
	// Refuse StateRunning (already querying) and StateMerging (a git op holds
	// the worktree) — starting a turn during a merge/rebase would write the
	// worktree concurrently with the git op.
	if s.state == StateRunning || s.state == StateMerging {
		return nil, false, false, fmt.Errorf("session %s: cannot Query in state %s", s.ID, s.state)
	}
	s.queryCount++
	wasCompleted = s.completedAt != ""
	s.completedAt = ""
	wasMerged = s.git.worktreeMerged
	s.git.worktreeMerged = false
	return s.rt, wasCompleted, wasMerged, nil
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

	var headSHA string
	if wasMerged {
		if project, pErr := s.queries.GetProject(context.Background(), s.ProjectID); pErr == nil {
			headSHA, _ = gitops.HeadCommitHash(project.Path)
		}
	}

	txErr := sqliteops.RetryWrite(func() error {
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

// toRuntimeAttachments converts frontend QueryAttachments (data URLs) into the
// provider-neutral runtime.Attachment shape consumed by Session.Query /
// Session.SendMessage.
func toRuntimeAttachments(attachments []QueryAttachment) ([]runtime.Attachment, error) {
	if len(attachments) == 0 {
		return nil, nil
	}
	atts := make([]runtime.Attachment, 0, len(attachments))
	for _, a := range attachments {
		mediaType, data, err := parseDataUrl(a.DataUrl)
		if err != nil {
			return nil, fmt.Errorf("attachment %q: %w", a.Name, err)
		}
		kind := runtime.AttachmentDocument
		if strings.HasPrefix(mediaType, "image/") {
			kind = runtime.AttachmentImage
		}
		atts = append(atts, runtime.Attachment{Kind: kind, MediaType: mediaType, Data: data})
	}
	return atts, nil
}

// parseDataUrl extracts the media type and decoded bytes from a data URL.
func parseDataUrl(dataUrl string) (mediaType string, data []byte, err error) {
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

// Interrupt stops the current generation without killing the session.
// Pending approvals and questions are torn down so the UI doesn't keep
// banners visible after the runtime has forgotten about them.
func (s *Session) Interrupt() error {
	s.mu.Lock()
	rt := s.rt
	s.mu.Unlock()
	if rt == nil {
		return ErrNotLive
	}

	// Snapshot runtime pending IDs before rt.Interrupt() drops them.
	var rtApprovalID, rtQuestionID string
	if rtA, rtQ := rt.PendingState(); rtA != nil || rtQ != nil {
		if rtA != nil {
			rtApprovalID = rtA.ID
		}
		if rtQ != nil {
			rtQuestionID = rtQ.ID
		}
	}

	if err := rt.Interrupt(context.Background()); err != nil {
		return err
	}

	// Drain agentique-side synthetic approvals (plan-review, spawn UI
	// prompts). These live on the Session, not in the runtime, so an
	// interrupt does not clear them automatically.
	syntheticIDs := s.drainSyntheticApprovals("session interrupted")

	for _, id := range syntheticIDs {
		s.broadcast("session.approval-resolved", PushApprovalResolved{SessionID: s.ID, ApprovalID: id})
	}
	if rtApprovalID != "" {
		s.broadcast("session.approval-resolved", PushApprovalResolved{SessionID: s.ID, ApprovalID: rtApprovalID})
	}
	if rtQuestionID != "" {
		s.broadcast("session.question-resolved", PushQuestionResolved{SessionID: s.ID, QuestionID: rtQuestionID})
	}
	return nil
}

// drainSyntheticApprovals denies and removes every pending agentique-side
// synthetic approval (non-blocking channel send mirrors Close). Returns the
// drained IDs so callers can broadcast resolution events; Close skips the
// broadcast because the session itself is going away.
func (s *Session) drainSyntheticApprovals(reason string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.syntheticApprovals) == 0 {
		return nil
	}
	ids := make([]string, 0, len(s.syntheticApprovals))
	for id, sa := range s.syntheticApprovals {
		select {
		case sa.ch <- &runtime.Decision{Allow: false, DenyMessage: reason}:
		default:
		}
		delete(s.syntheticApprovals, id)
		ids = append(ids, id)
	}
	return ids
}

// SetModel changes the model for this session. Only allowed when idle.
func (s *Session) SetModel(model string) error {
	s.mu.Lock()
	rt := s.rt
	s.mu.Unlock()
	if rt == nil {
		return ErrNotLive
	}
	return rt.SetModel(model)
}

// Close gracefully tears down the session (CLI process, event loop, pending
// approvals). Manager.Stop / Evict are the normal entry points; Close is
// exposed for direct callers (tests, CloseAll cleanup).
func (s *Session) Close() {
	s.cancelCtx()
	s.stopGitRefreshTimer()
	s.pipeline.StopPulseTimer()

	s.mu.Lock()
	rt := s.rt
	s.rt = nil
	s.cli = nil
	s.mu.Unlock()
	if rt != nil {
		_ = rt.Close()
	}

	// Drain synthetic approvals — runtime drains its own. No broadcast: the
	// session is going away so subscribers will not act on it.
	_ = s.drainSyntheticApprovals("session closed")
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
	return s.state, s.rt != nil, s.git.worktreeMerged, s.completedAt, s.git.gitOperation
}

// PendingState returns a snapshot of any pending approval/question, preferring
// agentique's synthetic approvals (plan-review, spawn UI prompt) and falling
// back to the runtime approval pump.
func (s *Session) PendingState() (*WirePendingApproval, *WirePendingQuestion) {
	s.mu.Lock()
	var approval *WirePendingApproval
	for _, sa := range s.syntheticApprovals {
		approval = &WirePendingApproval{
			ApprovalID: sa.id,
			ToolName:   sa.toolName,
			Input:      append(json.RawMessage(nil), sa.input...),
		}
		break
	}
	rt := s.rt
	s.mu.Unlock()

	var question *WirePendingQuestion
	if rt != nil {
		rtA, rtQ := rt.PendingState()
		if approval == nil && rtA != nil {
			approval = &WirePendingApproval{
				ApprovalID: rtA.ID,
				ToolName:   rtA.ToolName,
				Input:      rtA.Input,
			}
		}
		if rtQ != nil {
			qs := make([]WireQuestion, len(rtQ.Questions))
			for i, q := range rtQ.Questions {
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
			question = &WirePendingQuestion{QuestionID: rtQ.ID, Questions: qs}
		}
	}
	return approval, question
}

// injectMessageOrQuery delivers prompt to a live session by writing directly
// to the underlying CLISession. Bypasses both runtime's state-check and
// agentique's pipeline so the injected text doesn't surface as a user_message
// event in the receiving session's transcript. Used by channel context
// injection and pending delivery replay.
func injectMessageOrQuery(sess *Session, prompt string) error {
	if sess == nil {
		return ErrNotLive
	}
	return sess.directSendMessage(prompt)
}
