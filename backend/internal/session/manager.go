package session

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	claudecli "github.com/allbin/claudecli-go"
	"github.com/mdjarv/agentique/backend/internal/devurls"
	"github.com/mdjarv/agentique/backend/internal/mcphttp"
	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/google/uuid"
)

// Broadcaster sends push messages to all WebSocket clients for a project.
type Broadcaster interface {
	Broadcast(projectID, pushType string, payload any)
}

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
	MCPConfigs            []string // inline JSON or file paths for --mcp-config
	BrowserEnabled        bool
	SystemPromptAdditions string // from persona config; appended to the session preamble
}

// Manager manages the lifecycle of claudecli-go sessions.
type Manager struct {
	mu             sync.Mutex
	sessions       map[string]*Session
	db             *sql.DB
	queries        managerQueries
	broadcaster    Broadcaster
	connector      CLIConnector
	gitStatus      branchStatusQuerier
	GlobalPreamble string // extra system prompt appended to every session

	// HTTP MCP integration: set via SetMCPHTTP. When mcpTokens is nil the
	// manager falls back to the legacy stdio mcp-channel transport.
	mcpTokens   *mcphttp.TokenStore
	mcpInternalURL string
	devURLs     *devurls.Store
}

// NewManager creates a new session manager.
func NewManager(db *sql.DB, queries managerQueries, broadcaster Broadcaster, connector CLIConnector) *Manager {
	return &Manager{
		sessions:    make(map[string]*Session),
		db:          db,
		queries:     queries,
		broadcaster: broadcaster,
		connector:   connector,
		gitStatus:   RealBranchStatusQuerier(),
	}
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
func (m *Manager) devURLsPreamble() string {
	if m.devURLs == nil {
		return ""
	}
	if len(m.devURLs.Slots()) == 0 {
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


// Create starts a new claudecli-go session, persists metadata to DB, and returns the session.
func (m *Manager) Create(_ context.Context, params CreateParams) (*Session, error) {
	id := params.ID
	if id == "" {
		id = uuid.New().String()
	}

	// Build Session first (without cliSess) so the permission callback can capture it.
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
	// Set auto-approve and permission mode before connecting so the callback has it immediately.
	sess.SetAutoApproveMode(params.AutoApproveMode)
	sess.mu.Lock()
	sess.permissionMode = permMode
	sess.mu.Unlock()

	model := resolveModel(params.Model)
	connectOpts := []claudecli.Option{
		claudecli.WithWorkDir(params.WorkDir),
		claudecli.WithModel(model),
		claudecli.WithCanUseTool(sess.handleToolPermission),
		claudecli.WithUserInput(sess.handleUserInput),
		claudecli.WithIncludePartialMessages(),
		claudecli.WithReplayUserMessages(),
		claudecli.WithAppendSystemPrompt(buildPreamble(id, params.WorktreeBranch, params.Projects, params.BehaviorPresets, params.ChannelPreambles, params.TeamPreambles, m.GlobalPreamble, params.BrowserEnabled, params.SystemPromptAdditions) + m.devURLsPreamble()),
	}
	if params.Name != "" {
		connectOpts = append(connectOpts, claudecli.WithSessionName(params.Name))
	}
	if effort := resolveEffort(params.Effort); effort != "" {
		connectOpts = append(connectOpts, claudecli.WithEffort(effort))
	}
	if params.MaxBudget > 0 {
		connectOpts = append(connectOpts, claudecli.WithMaxBudget(params.MaxBudget))
	}
	if params.MaxTurns > 0 {
		connectOpts = append(connectOpts, claudecli.WithMaxTurns(params.MaxTurns))
	}
	// Always pass permission mode explicitly to override user's Claude Code settings.
	connectOpts = append(connectOpts, claudecli.WithPermissionMode(claudecli.PermissionMode(permMode)))
	// Merge all MCP configs into a single call (WithMCPConfig overwrites, not appends).
	connectOpts = append(connectOpts, claudecli.WithMCPConfig(m.buildMCPConfigs(id, params.MCPConfigs)...))

	// Use background context: the CLI process must outlive the WS connection
	// that triggered session creation. The WS conn context cancels on
	// disconnect (e.g. page refresh), which would SIGTERM the CLI process.
	cliSess, err := m.connector.Connect(context.Background(), connectOpts...)
	if err != nil {
		return nil, err
	}

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
		State:          string(StateIdle),
		Model:          params.Model,
		PermissionMode: permMode,
		AutoApproveMode: params.AutoApproveMode,
		Effort:          params.Effort,
		MaxBudget:       params.MaxBudget,
		MaxTurns:        int64(params.MaxTurns),
		BehaviorPresets: params.BehaviorPresets.String(),
		AgentProfileID: sql.NullString{
			String: params.AgentProfileID,
			Valid:  params.AgentProfileID != "",
		},
	})
	if dbErr != nil {
		cliSess.Close()
		return nil, dbErr
	}

	sess.setCLISession(cliSess)

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
	Projects          []ProjectInfo
	BehaviorPresets   BehaviorPresets
	ChannelPreambles  []*ChannelPreambleInfo
	TeamPreambles     []*TeamPreambleInfo
	ExtraPreamble         string   // appended to system prompt (e.g. fresh-worktree notice)
	MCPConfigs            []string // inline JSON or file paths for --mcp-config
	BrowserEnabled        bool
	SystemPromptAdditions string // from persona config; appended to session preamble
}

// Resume reconnects to an existing Claude session using WithResume().
func (m *Manager) Resume(_ context.Context, p ResumeParams) (*Session, error) {
	m.mu.Lock()
	if _, ok := m.sessions[p.SessionID]; ok {
		m.mu.Unlock()
		return nil, fmt.Errorf("session %s is already live", p.SessionID)
	}
	m.mu.Unlock()

	// Continue turn numbering from where we left off.
	maxTurn, _ := m.queries.MaxTurnIndex(context.Background(), p.SessionID)
	turnIndex := int(maxTurn)

	// Build Session first (without cliSess) so the permission callback can capture it.
	sess := newSession(sessionParams{
		id:                    p.SessionID,
		projectID:             p.ProjectID,
		model:                 p.Model,
		db:                    m.db,
		queries:               m.queries,
		broadcast:             m.broadcastFunc(p.ProjectID),
		turnIndex:             turnIndex,
		workDir:               p.WorkDir,
		initialGitVersion:     p.InitialGitVersion,
		broadcastInitialState: true, // frontend already has this session, needs state push
		gitStatus:             m.gitStatus,
	})
	sess.mu.Lock()
	sess.queryCount = turnIndex + 1
	sess.autoApproveMode = p.AutoApproveMode
	if p.PermissionMode != "" {
		sess.permissionMode = p.PermissionMode
	}
	sess.mu.Unlock()
	sess.pipeline.SetClaudeSessionID(p.ClaudeSessionID)

	connectOpts := []claudecli.Option{
		claudecli.WithWorkDir(p.WorkDir),
		claudecli.WithModel(resolveModel(p.Model)),
		claudecli.WithCanUseTool(sess.handleToolPermission),
		claudecli.WithUserInput(sess.handleUserInput),
		claudecli.WithIncludePartialMessages(),
		claudecli.WithReplayUserMessages(),
		claudecli.WithResume(p.ClaudeSessionID),
		claudecli.WithAppendSystemPrompt(buildPreamble(p.SessionID, p.WorktreeBranch, p.Projects, p.BehaviorPresets, p.ChannelPreambles, p.TeamPreambles, m.GlobalPreamble, p.BrowserEnabled, p.SystemPromptAdditions) + m.devURLsPreamble() + p.ExtraPreamble),
	}
	if p.Name != "" {
		connectOpts = append(connectOpts, claudecli.WithSessionName(p.Name))
	}
	if effort := resolveEffort(p.Effort); effort != "" {
		connectOpts = append(connectOpts, claudecli.WithEffort(effort))
	}
	if p.MaxBudget > 0 {
		connectOpts = append(connectOpts, claudecli.WithMaxBudget(p.MaxBudget))
	}
	if p.MaxTurns > 0 {
		connectOpts = append(connectOpts, claudecli.WithMaxTurns(p.MaxTurns))
	}
	// Always pass permission mode explicitly to override user's Claude Code settings.
	resumePermMode := p.PermissionMode
	if resumePermMode == "" {
		resumePermMode = "default"
	}
	connectOpts = append(connectOpts, claudecli.WithPermissionMode(claudecli.PermissionMode(resumePermMode)))
	connectOpts = append(connectOpts, claudecli.WithMCPConfig(m.buildMCPConfigs(p.SessionID, p.MCPConfigs)...))

	// Use background context: the CLI process must outlive the WS connection
	// that triggered the resume. See Create() for rationale.
	cliSess, err := m.connector.Connect(context.Background(), connectOpts...)
	if err != nil {
		return nil, err
	}

	if err := m.queries.UpdateSessionState(context.Background(), store.UpdateSessionStateParams{
		State: string(StateIdle),
		ID:    p.SessionID,
	}); err != nil {
		slog.Error("persist session state on resume failed", "session_id", p.SessionID, "error", err)
	}

	sess.setCLISession(cliSess)

	m.mu.Lock()
	m.sessions[p.SessionID] = sess
	m.mu.Unlock()

	slog.Info("session resumed", "session_id", p.SessionID, "claude_session_id", p.ClaudeSessionID)
	return sess, nil
}

// Reconnect starts a fresh CLI process for an existing session (no --resume).
// Used after ResetConversation clears the claude_session_id.
func (m *Manager) Reconnect(_ context.Context, p ResumeParams) (*Session, error) {
	m.mu.Lock()
	if _, ok := m.sessions[p.SessionID]; ok {
		m.mu.Unlock()
		return nil, fmt.Errorf("session %s is already live", p.SessionID)
	}
	m.mu.Unlock()

	sess := newSession(sessionParams{
		id:                    p.SessionID,
		projectID:             p.ProjectID,
		model:                 p.Model,
		db:                    m.db,
		queries:               m.queries,
		broadcast:             m.broadcastFunc(p.ProjectID),
		turnIndex:             -1, // fresh conversation starts from turn 0
		workDir:               p.WorkDir,
		initialGitVersion:     p.InitialGitVersion,
		broadcastInitialState: true,
		gitStatus:             m.gitStatus,
	})
	sess.mu.Lock()
	sess.autoApproveMode = p.AutoApproveMode
	if p.PermissionMode != "" {
		sess.permissionMode = p.PermissionMode
	}
	sess.mu.Unlock()

	connectOpts := []claudecli.Option{
		claudecli.WithWorkDir(p.WorkDir),
		claudecli.WithModel(resolveModel(p.Model)),
		claudecli.WithCanUseTool(sess.handleToolPermission),
		claudecli.WithUserInput(sess.handleUserInput),
		claudecli.WithIncludePartialMessages(),
		claudecli.WithReplayUserMessages(),
		claudecli.WithAppendSystemPrompt(buildPreamble(p.SessionID, p.WorktreeBranch, p.Projects, p.BehaviorPresets, p.ChannelPreambles, p.TeamPreambles, m.GlobalPreamble, p.BrowserEnabled, p.SystemPromptAdditions) + m.devURLsPreamble() + p.ExtraPreamble),
	}
	if p.Name != "" {
		connectOpts = append(connectOpts, claudecli.WithSessionName(p.Name))
	}
	if effort := resolveEffort(p.Effort); effort != "" {
		connectOpts = append(connectOpts, claudecli.WithEffort(effort))
	}
	if p.MaxBudget > 0 {
		connectOpts = append(connectOpts, claudecli.WithMaxBudget(p.MaxBudget))
	}
	if p.MaxTurns > 0 {
		connectOpts = append(connectOpts, claudecli.WithMaxTurns(p.MaxTurns))
	}
	permMode := p.PermissionMode
	if permMode == "" {
		permMode = "default"
	}
	connectOpts = append(connectOpts, claudecli.WithPermissionMode(claudecli.PermissionMode(permMode)))
	connectOpts = append(connectOpts, claudecli.WithMCPConfig(m.buildMCPConfigs(p.SessionID, p.MCPConfigs)...))

	cliSess, err := m.connector.Connect(context.Background(), connectOpts...)
	if err != nil {
		return nil, err
	}

	if err := m.queries.UpdateSessionState(context.Background(), store.UpdateSessionStateParams{
		State: string(StateIdle),
		ID:    p.SessionID,
	}); err != nil {
		slog.Error("persist session state on reconnect failed", "session_id", p.SessionID, "error", err)
	}

	sess.setCLISession(cliSess)

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
	if sess != nil {
		sess.Close()
	}
	m.releaseSessionResources(id)
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

	if sess != nil {
		sess.Close()
	}

	m.releaseSessionResources(id)

	return store.RetryWrite(func() error {
		return m.queries.UpdateSessionState(context.Background(), store.UpdateSessionStateParams{
			State: string(StateStopped),
			ID:    id,
		})
	})
}

// releaseSessionResources frees any dev URL lease and revokes the MCP bearer
// token for the given session. Safe to call multiple times (idempotent).
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
// Call once at startup before accepting connections — these are leftovers from
// a previous server run that didn't shut down cleanly.
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

// CloseAll gracefully closes all live sessions with a per-session timeout.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.sessions = make(map[string]*Session)
	m.mu.Unlock()

	slog.Info("closing all sessions", "count", len(sessions))

	var wg sync.WaitGroup
	for _, s := range sessions {
		wg.Add(1)
		go func(s *Session) {
			defer wg.Done()
			done := make(chan struct{})
			go func() {
				s.Close()
				close(done)
			}()
			timer := time.NewTimer(5 * time.Second)
			defer timer.Stop()
			select {
			case <-done:
			case <-timer.C:
				slog.Warn("session close timed out", "session_id", s.ID)
			}
		}(s)
	}
	wg.Wait()
}

// buildMCPConfigs prepends the agentique MCP server config (HTTP if a token
// store is wired, stdio mcp-channel otherwise) to any additional MCP configs.
// Returns a single slice suitable for a single WithMCPConfig call.
//
// Per-session token is minted on each call when HTTP transport is active.
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
		m.broadcaster.Broadcast(projectID, pushType, payload)
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
// Passes the name through directly (e.g. "opus[1m]") since claudecli.Model is a string type.
func resolveModel(name string) claudecli.Model {
	if name == "" {
		return claudecli.ModelOpus
	}
	return claudecli.Model(name)
}
