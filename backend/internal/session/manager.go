package session

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"

	"github.com/allbin/agentkit/devurls"
	"github.com/allbin/agentkit/eventbus"
	"github.com/allbin/agentkit/runtime"
	"github.com/allbin/agentkit/sqliteops"
	claudecli "github.com/allbin/claudecli-go"
	"github.com/google/uuid"
	"github.com/mdjarv/agentique/backend/internal/mcphttp"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// CreateParams holds the parameters for creating a new session.
type CreateParams struct {
	ID              string // optional; generated if empty
	ProjectID       string
	Name            string
	WorkDir         string
	WorktreePath    string
	WorktreeBranch  string
	WorktreeBaseSHA string
	Model           string
	PlanMode        bool
	AutoApproveMode string
	Effort          string
	MaxBudget       float64
	MaxTurns        int
	Projects              []ProjectInfo
	BehaviorPresets       BehaviorPresets
	ChannelPreambles      []*ChannelPreambleInfo
	TeamPreambles         []*TeamPreambleInfo
	AgentProfileID        string
	ParentSessionID       string   // optional: lead session that spawned this worker
	MCPConfigs            []string // inline JSON or file paths for --mcp-config
	BrowserEnabled        bool
	SystemPromptAdditions string // from persona config; appended to the session preamble
}

// Manager manages the lifecycle of agentique sessions, wrapping a runtime.Manager
// for the underlying Claude CLI lifecycle and adding persistence, channel
// routing, dev URL / MCP token cleanup.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session

	rt             *runtime.Manager
	connWrap       *capturingConnector
	db             *sql.DB
	queries        managerQueries
	broadcaster    eventbus.Broadcaster
	gitStatus      branchStatusQuerier
	GlobalPreamble string

	// HTTP MCP integration: set via SetMCPHTTP. When mcpTokens is nil the
	// manager falls back to the legacy stdio mcp-channel transport.
	mcpTokens      *mcphttp.TokenStore
	mcpInternalURL string
	devURLs        *devurls.Store
}

// NewManager creates a new session manager backed by the given runtime CLI connector.
func NewManager(db *sql.DB, queries managerQueries, broadcaster eventbus.Broadcaster, connector runtime.CLIConnector) *Manager {
	m := &Manager{
		sessions:    make(map[string]*Session),
		db:          db,
		queries:     queries,
		broadcaster: broadcaster,
		gitStatus:   RealBranchStatusQuerier(),
	}
	m.connWrap = &capturingConnector{inner: connector}
	m.rt = runtime.NewManager(m.connWrap, runtime.WithOnTerminated(func(_ context.Context, sessionID string) {
		m.releaseSessionResources(sessionID)
	}))
	return m
}

// capturingConnector wraps the configured runtime.CLIConnector and stashes
// each connected CLISession on a buffer. agentique.Manager then snaps the
// most recent CLISession into the agentique Session right after rt.Create or
// rt.Resume returns. This is needed because runtime keeps the CLISession
// private and Underlying() returns nil for test mocks; agentique needs
// direct CLI access for "silent" mid-conversation injections (channel
// context, pending delivery replay) that must bypass the pipeline.
type capturingConnector struct {
	inner runtime.CLIConnector

	mu       sync.Mutex
	captured []runtime.CLISession
}

func (c *capturingConnector) Connect(ctx context.Context, opts ...claudecli.Option) (runtime.CLISession, error) {
	cli, err := c.inner.Connect(ctx, opts...)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.captured = append(c.captured, cli)
	c.mu.Unlock()
	return cli, nil
}

// pop returns the most recently connected CLISession (LIFO). agentique
// Manager calls this immediately after a successful runtime Connect so the
// pairing is unambiguous despite the CLIConnector API not carrying a
// session ID.
func (c *capturingConnector) pop() runtime.CLISession {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.captured) == 0 {
		return nil
	}
	idx := len(c.captured) - 1
	cli := c.captured[idx]
	c.captured = c.captured[:idx]
	return cli
}

// SetBranchStatusQuerier overrides the default git status querier (for testing).
func (m *Manager) SetBranchStatusQuerier(q branchStatusQuerier) { m.gitStatus = q }

// SetMCPHTTP wires the HTTP MCP token store and the internal URL spawned
// Claude subprocesses use to reach /mcp. When tokens or url is empty the
// manager falls back to the stdio mcp-channel transport.
func (m *Manager) SetMCPHTTP(tokens *mcphttp.TokenStore, internalURL string) {
	m.mcpTokens = tokens
	m.mcpInternalURL = internalURL
}

// SetDevURLStore wires the dev URL lease store so leases can be released on
// session destroy.
func (m *Manager) SetDevURLStore(store *devurls.Store) { m.devURLs = store }

// devURLsPreamble returns a system-prompt section documenting the dev URL
// capability when at least one slot is configured. Empty string otherwise.
func (m *Manager) devURLsPreamble(ctx context.Context) string {
	if m.devURLs == nil {
		return ""
	}
	if len(m.devURLs.Slots(ctx)) == 0 {
		return ""
	}
	return preambleDevURLs
}

const preambleDevURLs = `

## Public Dev URL

You can expose any local HTTP service to a public HTTPS URL via the ` + "`AcquireDevUrl`" + ` MCP tool.

- Returns ` + "`{url, port, publicHost}`" + ` — bind your service to the returned port on ` + "`127.0.0.1`" + ` (or ` + "`0.0.0.0`" + `) and it is reachable at the URL.
- URLs are TLS-terminated with a valid Let's Encrypt certificate, so features requiring HTTPS (passkeys/WebAuthn, secure cookies, service workers, camera/mic, etc.) work.
- Use for: web UI iteration with hot reload, demoing running output to the user, exposing an API you want to hit from an external client, any "I need the user to click through this" moment.
- Slots are shared across sessions. Call ` + "`ListDevUrls`" + ` first if you suspect contention; call ` + "`ReleaseDevUrl`" + ` when done. Slots auto-release when the session ends.
`

// Create starts a new claudecli-go session via runtime, persists metadata to DB, and returns the session.
func (m *Manager) Create(ctx context.Context, params CreateParams) (*Session, error) {
	id := params.ID
	if id == "" {
		id = uuid.New().String()
	}

	sess := newSession(sessionParams{
		id:        id,
		projectID: params.ProjectID,
		model:     params.Model,
		db:        m.db,
		queries:   m.queries,
		broadcast: m.broadcastFunc(params.ProjectID),
		turnIndex: -1, // first Query() will increment to 0
		workDir:   params.WorkDir,
		gitStatus: m.gitStatus,
	})

	permMode := "default"
	if params.PlanMode {
		permMode = "plan"
	}
	autoMode := normalizeAutoApprove(params.AutoApproveMode)
	sess.mu.Lock()
	sess.permissionMode = permMode
	sess.autoApproveMode = autoMode
	sess.mu.Unlock()

	preamble := buildPreamble(id, params.WorktreeBranch, params.Projects, params.BehaviorPresets, params.ChannelPreambles, params.TeamPreambles, m.GlobalPreamble, params.BrowserEnabled, params.SystemPromptAdditions) + m.devURLsPreamble(context.Background())

	mcpConfigs := m.buildMCPConfigs(id, params.MCPConfigs)

	connectExtra := []claudecli.Option{
		claudecli.WithIncludePartialMessages(),
		claudecli.WithReplayUserMessages(),
	}
	if params.Name != "" {
		connectExtra = append(connectExtra, claudecli.WithSessionName(params.Name))
	}

	rtSess, err := m.rt.Create(ctx, runtime.CreateParams{
		SessionID:      id,
		WorkDir:        params.WorkDir,
		Preamble:       preamble,
		Model:          resolveModel(params.Model),
		PlanMode:       runtime.PlanMode(permMode),
		AutoApprove:    runtimeAutoApproveMode(autoMode),
		Effort:         resolveEffort(params.Effort),
		MaxBudget:      params.MaxBudget,
		MaxTurns:       params.MaxTurns,
		MCPConfigs:     mcpConfigs,
		ConnectOptions: connectExtra,
		SessionOptions: []runtime.SessionOption{
			runtime.WithBroadcast(makeBroadcastHook(sess)),
			runtime.WithInterceptors(sess.agentiqueInterceptors()),
		},
	})
	if err != nil {
		return nil, err
	}
	sess.setRuntime(rtSess, m.connWrap.pop())

	_, dbErr := m.queries.CreateSession(context.Background(), store.CreateSessionParams{
		ID:        id,
		ProjectID: params.ProjectID,
		Name:      params.Name,
		WorkDir:   params.WorkDir,
		WorktreePath: sql.NullString{
			String: params.WorktreePath,
			Valid:  params.WorktreePath != "",
		},
		WorktreeBranch: sql.NullString{
			String: params.WorktreeBranch,
			Valid:  params.WorktreeBranch != "",
		},
		WorktreeBaseSha: sql.NullString{
			String: params.WorktreeBaseSHA,
			Valid:  params.WorktreeBaseSHA != "",
		},
		State:           string(StateIdle),
		Model:           params.Model,
		PermissionMode:  permMode,
		AutoApproveMode: autoMode,
		Effort:          params.Effort,
		MaxBudget:       params.MaxBudget,
		MaxTurns:        int64(params.MaxTurns),
		BehaviorPresets: params.BehaviorPresets.String(),
		AgentProfileID: sql.NullString{
			String: params.AgentProfileID,
			Valid:  params.AgentProfileID != "",
		},
		ParentSessionID: sql.NullString{
			String: params.ParentSessionID,
			Valid:  params.ParentSessionID != "",
		},
	})
	if dbErr != nil {
		_ = m.rt.Stop(context.Background(), id)
		return nil, dbErr
	}

	m.mu.Lock()
	m.sessions[id] = sess
	m.mu.Unlock()

	return sess, nil
}

// ResumeParams holds the parameters for resuming an existing session.
type ResumeParams struct {
	SessionID         string
	ClaudeSessionID   string
	ProjectID         string
	Name              string
	WorkDir           string
	WorktreeBranch    string
	Model             string
	PermissionMode    string
	AutoApproveMode   string
	Effort            string
	MaxBudget         float64
	MaxTurns          int
	InitialGitVersion int64
	Projects              []ProjectInfo
	BehaviorPresets       BehaviorPresets
	ChannelPreambles      []*ChannelPreambleInfo
	TeamPreambles         []*TeamPreambleInfo
	ExtraPreamble         string   // appended to system prompt (e.g. fresh-worktree notice)
	MCPConfigs            []string // inline JSON or file paths for --mcp-config
	BrowserEnabled        bool
	SystemPromptAdditions string // from persona config; appended to session preamble
}

// Resume reconnects to an existing Claude session using runtime.Manager.Resume.
func (m *Manager) Resume(ctx context.Context, p ResumeParams) (*Session, error) {
	m.mu.Lock()
	if _, ok := m.sessions[p.SessionID]; ok {
		m.mu.Unlock()
		return nil, fmt.Errorf("session %s is already live", p.SessionID)
	}
	m.mu.Unlock()

	maxTurn, _ := m.queries.MaxTurnIndex(context.Background(), p.SessionID)
	turnIndex := int(maxTurn)

	sess := newSession(sessionParams{
		id:                p.SessionID,
		projectID:         p.ProjectID,
		model:             p.Model,
		db:                m.db,
		queries:           m.queries,
		broadcast:         m.broadcastFunc(p.ProjectID),
		turnIndex:         turnIndex,
		workDir:           p.WorkDir,
		initialGitVersion: p.InitialGitVersion,
		gitStatus:         m.gitStatus,
	})

	permMode := p.PermissionMode
	if permMode == "" {
		permMode = "default"
	}
	autoMode := normalizeAutoApprove(p.AutoApproveMode)

	sess.mu.Lock()
	sess.queryCount = turnIndex + 1
	sess.permissionMode = permMode
	sess.autoApproveMode = autoMode
	sess.mu.Unlock()
	sess.pipeline.SetClaudeSessionID(p.ClaudeSessionID)

	preamble := buildPreamble(p.SessionID, p.WorktreeBranch, p.Projects, p.BehaviorPresets, p.ChannelPreambles, p.TeamPreambles, m.GlobalPreamble, p.BrowserEnabled, p.SystemPromptAdditions) + m.devURLsPreamble(context.Background()) + p.ExtraPreamble

	mcpConfigs := m.buildMCPConfigs(p.SessionID, p.MCPConfigs)

	connectExtra := []claudecli.Option{
		claudecli.WithIncludePartialMessages(),
		claudecli.WithReplayUserMessages(),
	}
	if p.Name != "" {
		connectExtra = append(connectExtra, claudecli.WithSessionName(p.Name))
	}

	rtSess, err := m.rt.Resume(ctx, runtime.ResumeParams{
		SessionID:       p.SessionID,
		ClaudeSessionID: p.ClaudeSessionID,
		WorkDir:         p.WorkDir,
		Preamble:        preamble,
		Model:           resolveModel(p.Model),
		PlanMode:        runtime.PlanMode(permMode),
		AutoApprove:     runtimeAutoApproveMode(autoMode),
		Effort:          resolveEffort(p.Effort),
		MaxBudget:       p.MaxBudget,
		MaxTurns:        p.MaxTurns,
		MCPConfigs:      mcpConfigs,
		ConnectOptions:  connectExtra,
		SessionOptions: []runtime.SessionOption{
			runtime.WithBroadcast(makeBroadcastHook(sess)),
			runtime.WithInterceptors(sess.agentiqueInterceptors()),
		},
	})
	if err != nil {
		return nil, err
	}
	sess.setRuntime(rtSess, m.connWrap.pop())

	if err := m.queries.UpdateSessionState(context.Background(), store.UpdateSessionStateParams{
		State: string(StateIdle),
		ID:    p.SessionID,
	}); err != nil {
		slog.Error("persist session state on resume failed", "session_id", p.SessionID, "error", err)
	}

	m.mu.Lock()
	m.sessions[p.SessionID] = sess
	m.mu.Unlock()

	slog.Info("session resumed", "session_id", p.SessionID, "claude_session_id", p.ClaudeSessionID)
	return sess, nil
}

// Reconnect starts a fresh CLI process for an existing session (no --resume).
// Used after ResetConversation clears the claude_session_id.
func (m *Manager) Reconnect(ctx context.Context, p ResumeParams) (*Session, error) {
	m.mu.Lock()
	if _, ok := m.sessions[p.SessionID]; ok {
		m.mu.Unlock()
		return nil, fmt.Errorf("session %s is already live", p.SessionID)
	}
	m.mu.Unlock()

	sess := newSession(sessionParams{
		id:                p.SessionID,
		projectID:         p.ProjectID,
		model:             p.Model,
		db:                m.db,
		queries:           m.queries,
		broadcast:         m.broadcastFunc(p.ProjectID),
		turnIndex:         -1, // fresh conversation
		workDir:           p.WorkDir,
		initialGitVersion: p.InitialGitVersion,
		gitStatus:         m.gitStatus,
	})

	permMode := p.PermissionMode
	if permMode == "" {
		permMode = "default"
	}
	autoMode := normalizeAutoApprove(p.AutoApproveMode)

	sess.mu.Lock()
	sess.permissionMode = permMode
	sess.autoApproveMode = autoMode
	sess.mu.Unlock()

	preamble := buildPreamble(p.SessionID, p.WorktreeBranch, p.Projects, p.BehaviorPresets, p.ChannelPreambles, p.TeamPreambles, m.GlobalPreamble, p.BrowserEnabled, p.SystemPromptAdditions) + m.devURLsPreamble(context.Background()) + p.ExtraPreamble

	mcpConfigs := m.buildMCPConfigs(p.SessionID, p.MCPConfigs)

	connectExtra := []claudecli.Option{
		claudecli.WithIncludePartialMessages(),
		claudecli.WithReplayUserMessages(),
	}
	if p.Name != "" {
		connectExtra = append(connectExtra, claudecli.WithSessionName(p.Name))
	}

	rtSess, err := m.rt.Create(ctx, runtime.CreateParams{
		SessionID:      p.SessionID,
		WorkDir:        p.WorkDir,
		Preamble:       preamble,
		Model:          resolveModel(p.Model),
		PlanMode:       runtime.PlanMode(permMode),
		AutoApprove:    runtimeAutoApproveMode(autoMode),
		Effort:         resolveEffort(p.Effort),
		MaxBudget:      p.MaxBudget,
		MaxTurns:       p.MaxTurns,
		MCPConfigs:     mcpConfigs,
		ConnectOptions: connectExtra,
		SessionOptions: []runtime.SessionOption{
			runtime.WithBroadcast(makeBroadcastHook(sess)),
			runtime.WithInterceptors(sess.agentiqueInterceptors()),
		},
	})
	if err != nil {
		return nil, err
	}
	sess.setRuntime(rtSess, m.connWrap.pop())

	if err := m.queries.UpdateSessionState(context.Background(), store.UpdateSessionStateParams{
		State: string(StateIdle),
		ID:    p.SessionID,
	}); err != nil {
		slog.Error("persist session state on reconnect failed", "session_id", p.SessionID, "error", err)
	}

	m.mu.Lock()
	m.sessions[p.SessionID] = sess
	m.mu.Unlock()

	slog.Info("session reconnected (fresh)", "session_id", p.SessionID)
	return sess, nil
}

// Get returns a live session by ID, or nil if not found.
func (m *Manager) Get(id string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}

// IsLive reports whether a session has a connected CLI process.
func (m *Manager) IsLive(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.sessions[id]
	return ok
}

// Evict removes a dead session from the in-memory map and closes it.
// Unlike Stop, it does not change the DB state.
func (m *Manager) Evict(id string) {
	m.mu.Lock()
	sess, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	_ = m.rt.Evict(context.Background(), id)
	if sess != nil {
		sess.Close()
	}
}

// Stop closes a live session and marks it as stopped in DB.
// Does not handle worktree cleanup — callers (Service) are responsible for that.
func (m *Manager) Stop(_ context.Context, id string) error {
	m.mu.Lock()
	sess, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()

	_ = m.rt.Stop(context.Background(), id)
	if sess != nil {
		sess.Close()
	}

	return sqliteops.RetryWrite(func() error {
		return m.queries.UpdateSessionState(context.Background(), store.UpdateSessionStateParams{
			State: string(StateStopped),
			ID:    id,
		})
	})
}

// releaseSessionResources frees any dev URL lease and revokes the MCP bearer
// token for the given session. Safe to call multiple times (idempotent). Wired
// to runtime.Manager via WithOnTerminated.
func (m *Manager) releaseSessionResources(sessionID string) {
	if m.devURLs != nil {
		if freed := m.devURLs.Release(sessionID); len(freed) > 0 {
			slog.Info("released dev URL slots on session end", "session_id", sessionID, "slots", freed)
		}
	}
	if m.mcpTokens != nil {
		m.mcpTokens.Revoke(sessionID)
	}
}

// ListByProject returns session metadata from DB.
func (m *Manager) ListByProject(ctx context.Context, projectID string) ([]store.Session, error) {
	sessions, err := m.queries.ListSessionsByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	m.overlayLiveStates(sessions)
	return sessions, nil
}

// ListAll returns all sessions across all projects from DB.
func (m *Manager) ListAll(ctx context.Context) ([]store.Session, error) {
	sessions, err := m.queries.ListAllSessions(ctx)
	if err != nil {
		return nil, err
	}
	m.overlayLiveStates(sessions)
	return sessions, nil
}

// RecoverStaleSessions marks any sessions stuck in running/merging as stopped.
func (m *Manager) RecoverStaleSessions(ctx context.Context) {
	if err := m.queries.RecoverStaleSessions(ctx); err != nil {
		slog.Error("failed to recover stale sessions", "error", err)
	}
}

// overlayLiveStates replaces DB state with in-memory state for live sessions.
func (m *Manager) overlayLiveStates(sessions []store.Session) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range sessions {
		if live, ok := m.sessions[sessions[i].ID]; ok {
			sessions[i].State = string(live.State())
		}
	}
}

// CloseAll gracefully closes all live sessions.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	sessions := make(map[string]*Session, len(m.sessions))
	for id, s := range m.sessions {
		sessions[id] = s
	}
	m.sessions = make(map[string]*Session)
	m.mu.Unlock()

	slog.Info("closing all sessions", "count", len(sessions))
	m.rt.CloseAll(context.Background())
	for _, s := range sessions {
		s.Close()
	}
}

// buildMCPConfigs prepends the agentique MCP server config (HTTP if a token
// store is wired, stdio mcp-channel otherwise) to any additional MCP configs.
func (m *Manager) buildMCPConfigs(sessionID string, extra []string) []string {
	first := ChannelMCPConfig()
	if m.mcpTokens != nil && m.mcpInternalURL != "" {
		tok, err := m.mcpTokens.Mint(sessionID)
		if err != nil {
			slog.Warn("mcp token mint failed, falling back to stdio", "session_id", sessionID, "error", err)
		} else {
			first = AgentiqueMCPHTTPConfig(m.mcpInternalURL, tok)
		}
	}
	return append([]string{first}, extra...)
}

// combineMCPConfigs is the legacy entry point for tests/callers that lack a
// Manager reference. Prefer Manager.buildMCPConfigs in production.
func combineMCPConfigs(extra []string) []string {
	return append([]string{ChannelMCPConfig()}, extra...)
}

func (m *Manager) broadcastFunc(projectID string) func(string, any) {
	return func(pushType string, payload any) {
		m.broadcaster.Publish(projectID, pushType, payload)
	}
}

// resolveEffort maps a string effort level to a claudecli.EffortLevel constant.
// Returns empty string for unknown/empty values (CLI default).
func resolveEffort(level string) claudecli.EffortLevel {
	switch level {
	case "low":
		return claudecli.EffortLow
	case "medium":
		return claudecli.EffortMedium
	case "high":
		return claudecli.EffortHigh
	case "xhigh":
		return claudecli.EffortXHigh
	case "max":
		return claudecli.EffortMax
	default:
		return ""
	}
}

// resolveModel maps a string model name to a claudecli.Model.
func resolveModel(name string) claudecli.Model {
	if name == "" {
		return claudecli.ModelOpus
	}
	return claudecli.Model(name)
}

// normalizeAutoApprove validates and canonicalizes an auto-approve string.
func normalizeAutoApprove(mode string) string {
	switch mode {
	case "auto", "fullAuto":
		return mode
	default:
		return "manual"
	}
}

// runtimeAutoApproveMode maps agentique's auto-approve string to runtime's
// AutoApproveMode. fullAuto bypasses the entire pump in runtime; auto is
// driven by agentique's safe-tool logic via the broadcast hook.
func runtimeAutoApproveMode(mode string) runtime.AutoApproveMode {
	switch mode {
	case "fullAuto":
		return runtime.AutoApproveAll
	default:
		return runtime.AutoApproveOff
	}
}
