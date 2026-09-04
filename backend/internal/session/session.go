package session

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
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

// cliCapabilityInterruptCancelQueued is the protocol token a provider CLI
// advertises when its interrupt honors "also drop the queue". agentkit passes
// provider capability tokens through verbatim, so this is claudecli's constant;
// a provider that advertises nothing simply never matches and falls back.
const cliCapabilityInterruptCancelQueued = claudecli.CapabilityInterruptCancelQueued

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

// optStr returns nil for the empty string, so a wire field tagged omitempty is
// absent rather than present-and-blank. Used where "not set" is a distinct
// answer the client reads (see SessionInfo.UnseenCompletedAt).
func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
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

	mu         sync.Mutex
	state      State
	queryCount int

	// repoRoots is the set of directories already handed to the CLI via
	// RegisterRepoRoot. The control request is not idempotent, so this is what
	// keeps a repeated channel-context refresh from erroring on every teammate
	// worktree it has already registered. Guarded by mu; nil until first use.
	repoRoots map[string]struct{}

	// lastActiveAt is the wall-clock time of the last turn start / state
	// transition. For an idle session it is "idle since"; the idle-eviction
	// sweep measures now-lastActiveAt against the configured TTL. Stamped in
	// newSession, validateAndPrepareQuery (turn commit), and setState. Guarded
	// by mu.
	lastActiveAt time.Time
	// evicting is set by beginIdleEvict to atomically claim an idle session for
	// resource-reclaiming eviction; validateAndPrepareQuery refuses while it is
	// set, so a turn can never start on a session being torn down. Guarded by mu.
	evicting bool
	// evictedAt mirrors the sessions.evicted_at column for the one snapshot that
	// has to carry it: the `stopped` push the eviction itself broadcasts, which
	// buildLocalSnapshot builds from memory. Set between the claim and the stop
	// that discards this session, so it is "" for the whole of a normal life.
	// Guarded by mu. See idle_evict.go.
	evictedAt string
	pipeline  *EventPipeline
	// meter measures the live context window on demand so the frontend's
	// meter survives compaction. Signal-driven, never polled; see
	// context_meter.go. Immutable after newSession.
	meter          *contextMeter
	queries        sessionQueries
	broadcast      func(pushType string, payload any)
	archivedAt     string        // ISO8601 timestamp, or "" when the user has not filed it away
	stateChangedCh chan struct{} // buffered(1), signaled on state transitions

	// unseenCompletedAt mirrors the sessions.unseen_completed_at column so a
	// snapshot built from memory (buildLocalSnapshot) says the same thing as
	// one built from the row (GitService.buildSnapshot). The column is
	// authoritative and is always written before this is updated, so the
	// mirror can lag a write but never lead one. Guarded by mu. See unseen.go.
	unseenCompletedAt string

	// lastOriginTurn / lastOriginKind record which turn the most recent
	// QueryOrigin belongs to, so the turn-end seam can ask "was the turn that
	// just ended a schedule fire?" — the outcome itself does not carry it, and
	// schedule attention is its own channel. Turn starts are serialized by
	// queryMu and wait for the previous completion to drain, so a completion
	// always describes the most recently started turn; the index is compared
	// anyway, and a mismatch falls back to "user", the marking case.
	// Guarded by mu.
	lastOriginTurn int
	lastOriginKind string

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

	// surfacedApprovalID / surfacedQuestionID are the runtime prompts the UI is
	// currently showing. A prompt can leave the runtime's queue with no user
	// answer (withdrawal), which has no reply path to broadcast a resolution on
	// — these let handlePendingChange notice the disappearance and clear the
	// banner. Guarded by mu; only ever written under pendingMu.
	surfacedApprovalID string
	surfacedQuestionID string

	// pendingMu serializes PendingChangeEvent handling. The handlers run on
	// unordered goroutines, so without it two could observe the pending state
	// in either order and leave the surfaced ids describing a state that has
	// already passed. Always acquired before mu, never the reverse.
	pendingMu sync.Mutex

	git     sessionGitState
	channel sessionChannelState
	persona sessionPersonaState

	db *sql.DB // for transactional writes

	// Browser support: port allocated for Chrome's remote debugging.
	browserPort int

	// onEnsureBrowser, when wired (browser support available), launches the
	// session's Chrome on demand. Called from handlePendingChange before a
	// browser tool runs so the agent's lazily-connecting Playwright MCP has a
	// live CDP endpoint to attach to. Guarded by mu.
	onEnsureBrowser func() error

	// recallFn, when wired by the Manager, returns a task-relevant memory recall block
	// to prepend to a turn plus the fact ids it surfaced. It fires every turn (not just
	// the first), passing recalledIDs so each turn injects only newly-relevant facts —
	// delta recall that follows the conversation. recalledIDs accumulates every id ever
	// surfaced this session. Both guarded by mu.
	recallFn    func(ctx context.Context, prompt string, exclude map[string]struct{}) (string, []string)
	recalledIDs map[string]struct{}

	// onComplete, when wired by the Manager, fires once per clean completion
	// (runtime StateDone — "conversation complete") so the brain can learn from the
	// finished transcript without the session being deleted. Async, best-effort; nil
	// disables it. Guarded by mu.
	onComplete func()

	// onIdle, when wired by the Manager, fires on every runtime →Idle
	// transition (async). The scheduler uses it to deliver queued runs at the
	// next idle boundary instead of waiting for the following tick. Guarded
	// by mu.
	onIdle func()

	// turnReg resolves turn completions to the callers that started the turns
	// (discussion orchestrator, scheduler), keyed by turn index. Created once
	// per Session object; Close resolves open subscriptions with a synthetic
	// SessionClosed outcome. See turn_registry.go.
	turnReg *turnRegistry

	// queryMu serializes turn starts (validate → persist → runtime Query).
	// Without it two concurrent Query callers can both pass validation during
	// the async Idle→Running gap; the loser is refused inside the runtime
	// *after* its prompt row and running-state were persisted, corrupting the
	// session with an orphan turn and a spurious StateFailed.
	queryMu sync.Mutex
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
	provider          string
	onCLIVersion      func(provider, version string)
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
		lastActiveAt:       time.Now(),
		db:                 p.db,
		queries:            p.queries,
		broadcast:          p.broadcast,
		stateChangedCh:     make(chan struct{}, 1),
		autoApproveMode:    "manual",
		permissionMode:     "default",
		syntheticApprovals: make(map[string]*syntheticApproval),
		turnReg:            newTurnRegistry(),
		git: sessionGitState{
			workDir:    p.workDir,
			gitVersion: p.initialGitVersion,
			gitStatus:  p.gitStatus,
		},
	}
	s.meter = newContextMeter(p.id, s.queryContextUsage, s.emitContextUsage)
	s.pipeline = NewEventPipeline(buildPipelineConfig(s, p))
	return s
}

// queryContextUsage measures the live context window against the provider's
// current transcript. ErrNotLive while the session has no runtime (evicted,
// resuming, closing) — transient, so the meter stays armed.
func (s *Session) queryContextUsage(ctx context.Context) (*runtime.ContextUsage, error) {
	s.mu.Lock()
	rt := s.rt
	s.mu.Unlock()
	if rt == nil {
		return nil, ErrNotLive
	}
	return rt.ContextUsage(ctx)
}

// emitContextUsage publishes a live measurement. It routes through the
// pipeline's serialized emitter like every other off-loop session.event so the
// frontend's wire sequence stays gap-free.
func (s *Session) emitContextUsage(ev WireContextUsageEvent) {
	s.broadcastSessionEvent(ev)
}

// setRuntime attaches a runtime.Session and the underlying CLISession to this
// agentique session. Manager calls this after a successful runtime Create /
// Resume / Reconnect.
func (s *Session) setRuntime(rt *runtime.Session, cli runtime.CLISession) {
	s.mu.Lock()
	s.rt = rt
	s.cli = cli
	// A new CLI process holds none of the previous one's workspace roots, so
	// drop the set rather than let it suppress re-registration after a
	// resume/reconnect (or an idle-evict round trip).
	s.repoRoots = nil
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
	m := map[string]runtime.ToolInterceptor{
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
		AgentiqueMemoryUsedTool:     allow,
		AgentiqueSuggestPromptTool:  allow,
		AgentiqueScheduleCreateTool: allow,
		AgentiqueScheduleReportTool: allow,
		AgentiqueScheduleNextTool:   allow,
	}
	// fullAuto (runtime.AutoApproveAll) short-circuits the approval pump, so the
	// lazy Chrome launch in handlePendingChange never runs for it. Register a
	// pre-dispatch launch interceptor on every browser tool — the one hook that
	// still fires under AutoApproveAll — so Chrome is up before @playwright/mcp
	// attaches over CDP. See Session.interceptBrowserTool / browserToolNames.
	for _, name := range browserToolNames {
		m[browserToolPrefix+name] = intercept(s.interceptBrowserTool)
	}
	return m
}

// interceptBrowserTool launches the session's Chrome before a browser MCP tool
// dispatches in fullAuto mode. fullAuto maps to runtime.AutoApproveAll, which
// short-circuits agentkit's approval pump before PendingChangeEvent fires — so
// handlePendingChange's prefix-based lazy launch never runs in that mode. This
// interceptor, by contrast, runs inline on the CLI permission goroutine ahead of
// the AutoApproveAll short-circuit, making it the one pre-dispatch hook that
// still fires under fullAuto.
//
// Every other mode keeps mapping to runtime.AutoApproveOff and routes through the
// pump, where handlePendingChange launches Chrome by prefix; this interceptor
// deliberately no-ops there (returns nil to fall through) to avoid a redundant
// launch. On success it returns nil so the AutoApproveAll path still allows the
// call; on launch failure it denies with the actionable message rather than
// letting @playwright/mcp fail opaquely with a CDP ECONNREFUSED.
//
// Blocking is intentional: the tool genuinely cannot run until Chrome is up, and
// EnsureBrowser is idempotent and race-safe.
func (s *Session) interceptBrowserTool(_ json.RawMessage) (*runtime.Decision, error) {
	s.mu.Lock()
	fullAuto := s.autoApproveMode == "fullAuto"
	s.mu.Unlock()
	if !fullAuto {
		return nil, nil
	}
	if err := s.ensureBrowser(); err != nil {
		return &runtime.Decision{Allow: false, DenyMessage: err.Error()}, nil
	}
	return nil, nil
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
		OnResolvedModel: func(id string) {
			persistResolvedModel(p, id)
			p.broadcast("session.model-resolved", PushSessionModelResolved{
				SessionID:     p.id,
				ResolvedModel: id,
			})
		},
		OnCLIVersion: func(v string) {
			if p.onCLIVersion != nil {
				p.onCLIVersion(p.provider, v)
			}
		},
		OnPlanTransition: s.transitionPlanMode,
		OnExitPlanMode: func(input json.RawMessage) {
			s.mu.Lock()
			aam := s.autoApproveMode
			s.mu.Unlock()
			if planReviewRequired(aam) {
				go s.requestPlanReview(input)
			} else {
				s.transitionPlanMode("default")
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
		OnContextStale:    func() { s.meter.Refresh() },
		OnTurnComplete: func(outcome TurnOutcome) {
			// Runtime drives Running→Idle from ResultEvent; agentique observes
			// via the broadcast hook. The registry resolves whoever subscribed
			// to this turn (discussion orchestrator, scheduler); delivery is
			// buffered and never blocks the event loop.
			s.turnReg.Deliver(outcome)
			// A turn that produced a completion is news the operator has not
			// read yet. Off the event loop: the mark's snapshot re-reads git
			// status, and the pump behind this callback is the one carrying
			// the session's events. See unseen.go.
			go s.markUnseenCompletion(outcome.TurnIndex)
		},
		OnTurnAborted: func(outcome TurnOutcome) {
			// A fatal error closed the turn without a TurnCompletedEvent.
			// Resolve turn subscribers (scheduler, discussion) with the
			// failure — and nothing else: an aborted turn never completed,
			// so it sets no unseen mark (see unseen.go) and OnTurnComplete
			// keeps meaning "a completion arrived".
			s.turnReg.Deliver(outcome)
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
			if proj, err := s.queries.GetProject(context.Background(), s.ProjectID); err == nil {
				item.ProjectSlug = proj.Slug
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

// SetEnsureBrowserFunc wires the on-demand Chrome launch callback.
func (s *Session) SetEnsureBrowserFunc(fn func() error) {
	s.mu.Lock()
	s.onEnsureBrowser = fn
	s.mu.Unlock()
}

// ensureBrowser launches the session's Chrome on demand (no-op when browser
// support is unwired). Blocking — may provision a Chromium on first ever use.
func (s *Session) ensureBrowser() error {
	s.mu.Lock()
	fn := s.onEnsureBrowser
	s.mu.Unlock()
	if fn == nil {
		return nil
	}
	return fn()
}

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

// RegisterRepoRoot adds dir to the CLI's workspace roots mid-run — the runtime
// equivalent of /add-dir. WithAddDirs only applies at startup, so a directory
// that comes into existence after the session started (a teammate's worktree,
// created when the lead spawns workers) is otherwise unreachable without
// tearing the session down and losing its context.
//
// Only supported when the session's provider is "claude"; codex sessions get
// ErrRepoRootUnsupported, which callers are expected to ignore rather than log.
//
// Registration is tracked per session because the control request is not
// idempotent — the CLI rejects a directory it already holds. The set lives with
// the CLI process, so it resets on reconnect/resume, which is correct: the new
// CLI holds none of the old registrations.
func (s *Session) RegisterRepoRoot(dir string) error {
	// Already-registered wins over every other check: it is the common case on
	// a channel-context refresh, and it must stay a no-op regardless of what
	// the session's provider is now.
	s.mu.Lock()
	_, done := s.repoRoots[dir]
	s.mu.Unlock()
	if done {
		return nil
	}

	cli := s.claudeSession()
	if cli == nil {
		return ErrRepoRootUnsupported
	}

	// Called without the lock: this is a round-trip to the CLI, and the event
	// pump needs s.mu to keep draining while it is in flight.
	resolved, err := cli.RegisterRepoRoot(dir)
	if err != nil {
		return fmt.Errorf("register repo root %q: %w", dir, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.repoRoots == nil {
		s.repoRoots = make(map[string]struct{})
	}
	// Record both spellings. The CLI resolves relative paths against its own
	// working directory, so the value it returns is authoritative and is what a
	// later caller may hand us.
	s.repoRoots[dir] = struct{}{}
	s.repoRoots[resolved] = struct{}{}
	return nil
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

// recallTimeout bounds the one-time recall lookup so a slow or hung vector backend
// can never stall the first turn (reliability-first): on timeout we inject nothing.
const recallTimeout = 3 * time.Second

// SetRecallFn wires the one-time task-relevant memory recall callback. The Manager
// binds the project; the Session fires it once, on its first turn. nil disables it.
func (s *Session) SetRecallFn(fn func(ctx context.Context, prompt string, exclude map[string]struct{}) (string, []string)) {
	s.mu.Lock()
	s.recallFn = fn
	s.mu.Unlock()
}

// SetOnComplete wires the per-session completion callback fired once on a clean
// completion (runtime StateDone). The Manager binds the project/session; nil disables it.
func (s *Session) SetOnComplete(fn func()) {
	s.mu.Lock()
	s.onComplete = fn
	s.mu.Unlock()
}

// SetOnIdle wires the per-session idle callback fired on every runtime →Idle
// transition. The Manager binds the session id; nil disables it.
func (s *Session) SetOnIdle(fn func()) {
	s.mu.Lock()
	s.onIdle = fn
	s.mu.Unlock()
}

// SubscribeTurn registers for the outcome of a specific turn on this session.
// Prefer QueryWithOutcome, which makes the subscription atomic with the turn
// start; this exists for observers that learn the turn index out of band.
func (s *Session) SubscribeTurn(turnIndex int) <-chan TurnOutcome {
	return s.turnReg.Subscribe(turnIndex)
}

// injectRecall prepends a task-relevant memory recall block to the turn's prompt. It
// fires on every turn, passing the ids already surfaced this session so recall returns
// only what's newly relevant (delta) — the recall follows the conversation rather than
// being front-loaded once. Best-effort: a disabled brain, a slow/failed recall, a
// too-thin prompt, or nothing new returns the prompt unchanged.
func (s *Session) injectRecall(prompt string) string {
	s.mu.Lock()
	fn := s.recallFn
	if fn == nil {
		s.mu.Unlock()
		return prompt
	}
	if s.recalledIDs == nil {
		s.recalledIDs = make(map[string]struct{})
	}
	// Snapshot the seen-set so the recall call (which does I/O) doesn't read it under
	// lock or race a concurrent merge.
	exclude := make(map[string]struct{}, len(s.recalledIDs))
	for id := range s.recalledIDs {
		exclude[id] = struct{}{}
	}
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), recallTimeout)
	defer cancel()
	block, ids := fn(ctx, prompt, exclude)
	if strings.TrimSpace(block) == "" {
		return prompt
	}
	s.mu.Lock()
	for _, id := range ids {
		s.recalledIDs[id] = struct{}{}
	}
	s.mu.Unlock()
	return block + "\n\n" + prompt
}

// Query sends a prompt (with optional images) to the CLI session and starts
// streaming events.
func (s *Session) Query(ctx context.Context, prompt string, attachments []QueryAttachment) error {
	_, _, err := s.queryInternal(ctx, prompt, attachments, false, QueryOrigin{})
	return err
}

// QueryWithOutcome starts a fresh turn like Query and additionally returns the
// turn's index plus a single-delivery channel that resolves with the turn's
// outcome — or a synthetic SessionClosed if the session is torn down first.
// The subscription is registered under the same turn-start critical section,
// so it can neither miss a fast completion nor capture a neighbouring turn.
// A non-zero origin tags the persisted prompt row and the turn-started push.
func (s *Session) QueryWithOutcome(ctx context.Context, prompt string, attachments []QueryAttachment, origin QueryOrigin) (int, <-chan TurnOutcome, error) {
	return s.queryInternal(ctx, prompt, attachments, true, origin)
}

func (s *Session) queryInternal(_ context.Context, prompt string, attachments []QueryAttachment, subscribe bool, origin QueryOrigin) (int, <-chan TurnOutcome, error) {
	// Serialize turn starts end-to-end: every turn-start path (composer,
	// scheduler, pending-flush replay, plan-approval auto-fire) funnels here,
	// so validate → persist → runtime Query is atomic per session and a
	// concurrent caller is refused *before* anything was persisted.
	s.queryMu.Lock()
	defer s.queryMu.Unlock()

	// The runtime flips Idle and fires the state hook BEFORE the pipeline
	// processes the TurnCompletedEvent (agentkit finishTurn broadcasts the
	// StateChange from inside the completion's dispatch). Starting a turn in
	// that window would advance the turn counter under the unprocessed
	// completion and steal its outcome attribution. Wait for it to drain; the
	// timeout covers turns that never complete (fatal CLI death also closes
	// the turn via the pipeline's fatal-error branch).
	if !s.pipeline.WaitTurnClosed(5 * time.Second) {
		slog.Warn("turn start proceeding with previous completion unprocessed",
			"session_id", s.ID)
	}

	rt, wasArchived, wasMerged, err := s.validateAndPrepareQuery(origin)
	if err != nil {
		return 0, nil, err
	}

	// Inject task-relevant recall only after validation passes, so a rejected
	// query doesn't consume the one-shot. The augmented prompt is persisted, sent
	// to the model, and broadcast — so the recalled facts are visible in the
	// transcript and seen by the agent. Schedule-origin turns skip recall:
	// eviction between fires resets the per-session seen-set, so a loop would
	// re-inject the same facts every fire and inflate their `uses` counters
	// with no outcome signal (see QueryOrigin).
	if origin.Kind == "" {
		prompt = s.injectRecall(prompt)
	}

	turnIndex := s.pipeline.AdvanceTurn()
	s.recordTurnOrigin(turnIndex, origin)
	var outcome <-chan TurnOutcome
	if subscribe {
		outcome = s.turnReg.Subscribe(turnIndex)
	}
	s.persistQueryStart(turnIndex, wasArchived, wasMerged, prompt, attachments, origin)

	turnPayload := PushTurnStarted{SessionID: s.ID, Prompt: prompt, TurnIndex: turnIndex}
	if origin.Kind != "" {
		o := origin
		turnPayload.Origin = &o
	}
	if len(attachments) > 0 {
		turnPayload.Attachments = attachments
	}
	s.broadcast("session.turn-started", turnPayload)

	atts, berr := toRuntimeAttachments(attachments)
	if berr != nil {
		return 0, nil, fmt.Errorf("parse attachments: %w", berr)
	}
	queryErr := rt.Query(context.Background(), prompt, atts...)
	if queryErr != nil {
		// With turn starts serialized and the runtime state checked under the
		// same critical section, a refusal here is a genuine CLI failure —
		// not a lost race. No completion event will ever arrive for the turn
		// we just advanced, so close it here or it stays open forever.
		s.pipeline.CloseTurn()
		if stErr := s.setState(StateFailed); stErr != nil {
			slog.Error("state transition failed", "session_id", s.ID, "error", stErr)
		}
		return 0, nil, queryErr
	}
	return turnIndex, outcome, nil
}

// validateAndPrepareQuery checks the runtime is connected and preserves prior
// flags (completed, merged) for cleanup. Runtime drives the Idle→Running
// transition; agentique just resets transient flags and bumps queryCount.
// Schedule-origin turns are refused on a finished session under the same
// lock — Query would otherwise atomically UNSET completed/merged, so a fire
// racing the user's mark-done/merge would silently reopen the session. The
// scheduler's pre-delivery DB check cannot close that window; this can.
func (s *Session) validateAndPrepareQuery(origin QueryOrigin) (rt *runtime.Session, wasArchived, wasMerged bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rt == nil {
		return nil, false, false, ErrNotLive
	}
	if origin.Kind == "schedule" && (s.archivedAt != "" || s.git.worktreeMerged) {
		return nil, false, false, fmt.Errorf("session %s: %w", s.ID, ErrSessionFinished)
	}
	// Refuse a session claimed by the idle-eviction sweep — it is being torn
	// down and removed from the pool; the caller should lazy-resume a fresh one.
	// This is the same lock beginIdleEvict claims under, so the two are mutually
	// exclusive: whichever takes s.mu first wins.
	if s.evicting {
		return nil, false, false, ErrNotLive
	}
	// Refuse StateRunning (already querying) and StateMerging (a git op holds
	// the worktree) — starting a turn during a merge/rebase would write the
	// worktree concurrently with the git op.
	if s.state == StateRunning || s.state == StateMerging {
		return nil, false, false, fmt.Errorf("session %s: cannot Query in state %s: %w", s.ID, s.state, ErrBusy)
	}
	// The runtime's own state is authoritative for turn-in-flight: s.state
	// lags it (updated async via the broadcast hook), so a caller racing the
	// Idle→Running flip could pass the check above alone. Under queryMu this
	// closes the gap entirely — the winner's runtime transition is synchronous
	// inside rt.Query, so a loser always observes StateRunning here.
	if s.rt.State() == runtime.StateRunning {
		return nil, false, false, fmt.Errorf("session %s: cannot Query in state %s: %w", s.ID, StateRunning, ErrBusy)
	}
	s.queryCount++
	// Stamp activity at the turn-commit point. The runtime flips Idle→Running
	// asynchronously, so in-memory state can still read Idle during the gap;
	// refreshing lastActiveAt here guarantees a just-started turn is never seen
	// as idle-past-TTL by a concurrent eviction sweep.
	s.lastActiveAt = time.Now()
	wasArchived = s.archivedAt != ""
	s.archivedAt = ""
	wasMerged = s.git.worktreeMerged
	s.git.worktreeMerged = false
	return s.rt, wasArchived, wasMerged, nil
}

// persistQueryStart writes the running state, resets completed/merged flags in the
// database, and persists the prompt as seq 0 of the new turn.
// All writes are wrapped in a single transaction for atomicity.
func (s *Session) persistQueryStart(turnIndex int, wasArchived, wasMerged bool, prompt string, attachments []QueryAttachment, origin QueryOrigin) {
	promptPayload := map[string]any{"prompt": prompt}
	if len(attachments) > 0 {
		promptPayload["attachments"] = attachments
	}
	if origin.Kind != "" {
		promptPayload["origin"] = origin
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
			if wasArchived {
				if err := q.UnsetSessionArchived(ctx, s.ID); err != nil {
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
//
// cancelQueued makes stop mean stop. A bare interrupt aborts only the running
// turn: the provider starts the next queued command the instant it aborts, and
// agentique replays its own emulated queue at the following idle boundary — so
// the user watches work continue after pressing the button. Pass true from any
// user-facing stop; pass false only for an internal interrupt that is a step in
// some larger flow, where the queue is meant to survive.
func (s *Session) Interrupt(cancelQueued bool) error {
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

	// Take agentique's emulated queue (codex) BEFORE interrupting: the
	// interrupt drives the session to Idle, and flushPendingMessages replays
	// the queue on that very transition. Taking it afterwards is a lost race.
	// Restored on failure so a refused interrupt costs the user nothing.
	var takenPending []pendingMessage
	if cancelQueued {
		takenPending = s.takePendingMessages()
	}

	cancelledProviderQueue, err := s.interruptRuntime(rt, cancelQueued)
	if err != nil {
		s.restorePendingMessages(takenPending)
		return err
	}

	// Resolve the UI bubbles for everything the stop just dropped, so no
	// message is left rendering as pending forever.
	for _, m := range takenPending {
		s.broadcastSessionEvent(WireMessageDeliveryEvent{
			Type: "message_delivery", Status: "cancelled", MessageID: m.id,
		})
	}
	if cancelledProviderQueue {
		for _, id := range s.pipeline.CancelPendingMessages() {
			s.broadcastSessionEvent(WireMessageDeliveryEvent{
				Type: "message_delivery", Status: "cancelled", MessageID: id,
			})
		}
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

// interruptRuntime aborts the running turn and reports whether the provider's
// own queue was actually dropped.
//
// Two things can make that false even with cancelQueued set: an adapter with no
// queued-interrupt support at all (runtime.ErrNotSupported — testmode, codex),
// and a CLI old enough to ignore the flag, which answers with an empty receipt
// rather than an error. The second is indistinguishable from "nothing was
// queued" by the receipt alone, so it is settled by the advertised capability —
// never by comparing CLI version strings. Both fall back to a plain interrupt's
// behavior, and the caller must not then tell the UI anything was cancelled.
func (s *Session) interruptRuntime(rt *runtime.Session, cancelQueued bool) (cancelledQueue bool, err error) {
	if !cancelQueued {
		return false, rt.Interrupt(context.Background())
	}

	supported := s.pipeline.HasCLICapability(cliCapabilityInterruptCancelQueued)
	receipt, err := rt.InterruptWithQueued(context.Background(), supported)
	if errors.Is(err, runtime.ErrNotSupported) {
		return false, rt.Interrupt(context.Background())
	}
	if err != nil {
		return false, err
	}
	if !supported {
		slog.Debug("provider CLI cannot cancel queued work on interrupt; queue survives the stop",
			"session_id", s.ID)
		return false, nil
	}
	if receipt != nil {
		// StillQueued covers only id-stamped main-thread messages, and can
		// name ids agentique never sent (cron triggers, auto-resume
		// continuations) — it is a diagnostic, not a set to reconcile against.
		slog.Info("interrupt cancelled queued work",
			"session_id", s.ID,
			"cancelled", len(receipt.Cancelled),
			"still_queued", len(receipt.StillQueued),
		)
	}
	return true, nil
}

// takePendingMessages removes and returns agentique's emulated mid-turn queue
// (providers without native mid-turn injection). Returns nil when empty, which
// is the only case for claude.
func (s *Session) takePendingMessages() []pendingMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	taken := s.pendingMessages
	s.pendingMessages = nil
	return taken
}

// restorePendingMessages puts a taken queue back at the front, preserving FIFO
// order against anything queued in the meantime. Mirrors flushPendingMessages'
// own failure path.
func (s *Session) restorePendingMessages(msgs []pendingMessage) {
	if len(msgs) == 0 {
		return
	}
	s.mu.Lock()
	s.pendingMessages = append(msgs, s.pendingMessages...)
	s.mu.Unlock()
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
	s.meter.Stop()

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

	// Resolve open turn subscriptions with a synthetic SessionClosed outcome
	// so no subscriber (discussion orchestrator, scheduler) is left waiting
	// on a turn whose CLI is gone. The pipeline's turn goes with it: the CLI
	// is gone, so nothing will ever complete it.
	s.turnReg.Close()
	s.pipeline.CloseTurn()
}

// TurnInFlight reports whether this session is running a turn right now.
//
// Answered by the runtime, which is the only thing that knows: State() reports
// Idle for one dispatch before the completion that caused it is broadcast, and
// a caller that trusts it starts the next turn under an unprocessed completion.
// An evicted or never-started session has no runtime and is not running a turn.
func (s *Session) TurnInFlight() bool {
	s.mu.Lock()
	rt := s.rt
	s.mu.Unlock()
	return rt != nil && rt.TurnInFlight()
}

// Archive files the session away: it stamps archivedAt and nothing else.
//
// Deliberately no state transition. Archiving is a placement decision about the
// sidebar, not a claim about the CLI process — conflating the two is what let
// unarchive leave a session stranded in StateDone. What the process is doing is
// the runtime's story to tell; the caller (Service.ArchiveSession) releases an
// idle CLI separately, through the normal stop path, so the state that results
// is one that actually happened.
//
// Idempotent: an already-archived session keeps its original timestamp, so
// re-archiving never rewrites when the user filed it.
func (s *Session) Archive() error {
	s.mu.Lock()
	already := s.archivedAt != ""
	s.mu.Unlock()
	if already {
		return nil
	}

	// Persist before mutating memory: a failed write must not leave the session
	// presenting as archived until the next restart contradicts it.
	if err := s.queries.SetSessionArchived(context.Background(), s.ID); err != nil {
		return fmt.Errorf("persist session archived: %w", err)
	}

	s.mu.Lock()
	s.archivedAt = time.Now().UTC().Format(time.RFC3339)
	state := s.state
	s.mu.Unlock()

	s.broadcastState(state)
	return nil
}

// Unarchive clears archivedAt and broadcasts the change — the exact inverse of
// Archive, which is the point: there is one field to clear and no residue.
func (s *Session) Unarchive() error {
	if err := s.queries.UnsetSessionArchived(context.Background(), s.ID); err != nil {
		return fmt.Errorf("unset session archived: %w", err)
	}

	s.mu.Lock()
	s.archivedAt = ""
	state := s.state
	s.mu.Unlock()

	s.broadcastState(state)
	return nil
}

// MarkArchived stamps archivedAt on a live session without persisting or
// broadcasting — for callers that already wrote the row themselves (the merging
// dance, the local-session commit, post-resume flag replay). Pass the value the
// row carries so a resume doesn't rewrite when the user filed the session.
func (s *Session) MarkArchived(at string) {
	if at == "" {
		at = nowUTC()
	}
	s.mu.Lock()
	s.archivedAt = at
	s.mu.Unlock()
}

// liveState returns the current in-memory state fields needed for a GitSnapshot.
func (s *Session) liveState() (state State, connected bool, worktreeMerged bool, archivedAt string, gitOperation string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state, s.rt != nil, s.git.worktreeMerged, s.archivedAt, s.git.gitOperation
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
