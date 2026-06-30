package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mdjarv/agentique/backend/internal/store"
)

// Discussion groups are an Odysseus-style multi-persona deliberation: N personas,
// each a real tool-capable session, take turns discussing a prompt. After each
// turn the persona's reply is cross-injected into the others as a "[Name]: …"
// line, so the personas engage with each other rather than answering in isolation.
// The user drives every round; there is no moderator or auto-synthesis.
//
// Design: docs/discussion-groups.md. This file is the server-side orchestrator —
// pure plumbing over QuerySession (append + run a turn), the per-session
// turn-complete hook (capture the reply text), and SendChannelMessage (mirror
// each contribution to the channel timeline, which is the merged transcript the
// frontend renders).

// discussionTurnTimeout bounds a single persona's turn so a hung provider can't
// stall a round forever. Generous because capable models at high effort can run
// many minutes per turn.
const discussionTurnTimeout = 20 * time.Minute

// DiscussionMode controls speaking order within a round.
type DiscussionMode string

const (
	// DiscussionRoundRobin runs personas one at a time in a per-round shuffled
	// order, so later speakers see earlier speakers' replies.
	DiscussionRoundRobin DiscussionMode = "round-robin"
	// DiscussionParallel runs all personas concurrently against the same
	// pre-round transcript; they see each other only on the next round.
	DiscussionParallel DiscussionMode = "parallel"
)

// DiscussionScope controls the worktree binding for a group.
type DiscussionScope string

const (
	// DiscussionScopeWebOnly personas web-search and reason; no repo worktree.
	DiscussionScopeWebOnly DiscussionScope = "web-only"
	// DiscussionScopeRepoBacked personas share one worktree on a throwaway
	// group branch; writers' edits land there, never on the user's main tree.
	DiscussionScopeRepoBacked DiscussionScope = "repo-backed"
)

// DiscussionPersonaSpec describes one participant of a discussion group.
type DiscussionPersonaSpec struct {
	AgentProfileID string // bound profile (carries model/effort/system-prompt additions)
	Name           string
	Model          string // optional override of the profile's model
	Effort         string // optional override of the profile's effort
	WriteAccess    bool   // may edit files / run state-changing commands
	NoNamePrefix   bool   // render without a "[Name]:" prefix (the Razor style)
}

// StartDiscussionParams configures a new discussion-group run.
type StartDiscussionParams struct {
	ProjectID  string
	GroupName  string
	Mode       DiscussionMode
	Scope      DiscussionScope
	AutoCommit bool // commit writer turns on the shared group branch
	Personas   []DiscussionPersonaSpec
	Prompt     string // opening-round prompt
}

// DiscussionInfo is the wire view of a discussion returned to the frontend.
type DiscussionInfo struct {
	ChannelID      string   `json:"channelId"`
	ProjectID      string   `json:"projectId"`
	GroupName      string   `json:"groupName"`
	Mode           string   `json:"mode"`
	Scope          string   `json:"scope"`
	Round          int      `json:"round"`
	Running        bool     `json:"running"`
	WorktreeBranch string   `json:"worktreeBranch,omitempty"`
	Personas       []string `json:"personas"`
}

// personaContribution is one persona's reply, kept in the shared transcript for
// cross-injection into peers.
type personaContribution struct {
	name string
	text string
}

// discussionParticipant is the live state for one persona in a running group.
type discussionParticipant struct {
	// runtime drives this persona's turns — a dbSessionPersona (repo-backed,
	// full session) or a sessionlessPersona (web-only, raw CLI subprocess).
	runtime personaRuntime
	// senderType / senderID attribute this persona's channel messages: a
	// repo-backed persona posts as "session" with its session id; a web-only
	// persona posts as "persona" with its agent_profile id (it has no session).
	senderType   string
	senderID     string
	name         string
	noNamePrefix bool
	writeAccess  bool
	// lastSeen is the index into Discussion.transcript up to which this persona
	// has already been shown peer contributions (so each turn injects only the
	// delta since it last spoke). hasSpoken gates the one-time etiquette preamble.
	lastSeen  int
	hasSpoken bool
}

// Discussion is the live orchestration state for one running group, keyed in
// Service.discussions by channel ID.
type Discussion struct {
	ChannelID string
	ProjectID string
	GroupName string
	Mode      DiscussionMode
	Scope     DiscussionScope

	projectPath    string
	worktreePath   string // "" for web-only
	worktreeBranch string
	scratchDir     string // web-only: per-discussion os.MkdirTemp root; "" for repo-backed

	mu           sync.Mutex
	participants []*discussionParticipant
	transcript   []personaContribution
	round        int
	running      bool // a round is currently executing
}

func (d *Discussion) info() DiscussionInfo {
	d.mu.Lock()
	defer d.mu.Unlock()
	names := make([]string, len(d.participants))
	for i, p := range d.participants {
		names[i] = p.name
	}
	return DiscussionInfo{
		ChannelID:      d.ChannelID,
		ProjectID:      d.ProjectID,
		GroupName:      d.GroupName,
		Mode:           string(d.Mode),
		Scope:          string(d.Scope),
		Round:          d.round,
		Running:        d.running,
		WorktreeBranch: d.worktreeBranch,
		Personas:       names,
	}
}

// StartDiscussion creates a channel, spins up one session per persona (sharing a
// single worktree for repo-backed groups), runs the opening round asynchronously,
// and returns immediately. Persona contributions stream to the channel timeline.
func (s *Service) StartDiscussion(ctx context.Context, p StartDiscussionParams) (DiscussionInfo, error) {
	if len(p.Personas) < 2 {
		return DiscussionInfo{}, fmt.Errorf("a discussion needs at least 2 personas")
	}
	if p.Mode == "" {
		p.Mode = DiscussionRoundRobin
	}
	if p.Scope == "" {
		p.Scope = DiscussionScopeWebOnly
	}

	webOnly := p.Scope == DiscussionScopeWebOnly

	// Repo-backed discussions are project-scoped (shared worktree needs the
	// project path). Web-only discussions are project-less and sessionless — no
	// GetProject, no worktree, no agentique sessions rows.
	var project store.Project
	if !webOnly {
		var err error
		project, err = s.queries.GetProject(ctx, p.ProjectID)
		if err != nil {
			return DiscussionInfo{}, fmt.Errorf("get project: %w", err)
		}
	}

	// Web-only channels carry no project (channels.project_id is NULL since
	// migration 038); their lifecycle events fan out on the empty/global topic.
	channelProjectID := ""
	if !webOnly {
		channelProjectID = p.ProjectID
	}
	ch, err := s.CreateChannel(ctx, channelProjectID, p.GroupName)
	if err != nil {
		return DiscussionInfo{}, fmt.Errorf("create channel: %w", err)
	}

	d := &Discussion{
		ChannelID:   ch.ID,
		ProjectID:   channelProjectID,
		GroupName:   p.GroupName,
		Mode:        p.Mode,
		Scope:       p.Scope,
		projectPath: project.Path,
	}

	// Provision ONE shared worktree for repo-backed groups; every persona binds
	// its CWD to it. The orchestrator owns this tree (removed once on dissolve).
	var sharedDir string
	if !webOnly {
		branch := "group-" + shortID(ch.ID)
		sharedDir = s.worktree.WorktreePath(project.Name, branch)
		if err := s.worktree.ProvisionWorktree(ctx, project.Path, branch, sharedDir); err != nil {
			_ = s.DissolveChannel(ctx, ch.ID)
			return DiscussionInfo{}, fmt.Errorf("provision shared worktree: %w", err)
		}
		d.worktreePath = sharedDir
		d.worktreeBranch = branch
	}

	// Web-only personas share one throwaway scratch dir as their CWD — no repo,
	// no project. They are read-only (no writers) so they never conflict in it.
	if webOnly {
		scratch, err := os.MkdirTemp("", "agentique-discussion-"+shortID(ch.ID)+"-")
		if err != nil {
			_ = s.DissolveChannel(ctx, ch.ID)
			return DiscussionInfo{}, fmt.Errorf("create scratch dir: %w", err)
		}
		d.scratchDir = scratch
	}

	for _, spec := range p.Personas {
		var part *discussionParticipant
		var perr error
		if webOnly {
			part, perr = s.startWebOnlyPersona(ctx, d, ch.ID, spec)
		} else {
			part, perr = s.startRepoBackedPersona(ctx, ch.ID, p, spec, sharedDir)
		}
		if perr != nil {
			slog.Warn("discussion: persona start failed", "name", spec.Name, "scope", p.Scope, "error", perr)
			continue
		}
		d.participants = append(d.participants, part)
	}

	if len(d.participants) < 2 {
		s.teardownDiscussion(ctx, d, false)
		return DiscussionInfo{}, fmt.Errorf("fewer than 2 personas could be started")
	}

	s.discussions.Store(ch.ID, d)
	go s.runRound(context.Background(), d, p.Prompt)
	return d.info(), nil
}

// startRepoBackedPersona creates a full agentique session for a persona, binds it
// to the shared worktree, and joins it to the channel. This is the pre-refactor
// path, factored behind personaRuntime unchanged.
func (s *Service) startRepoBackedPersona(ctx context.Context, channelID string, p StartDiscussionParams, spec DiscussionPersonaSpec, sharedDir string) (*discussionParticipant, error) {
	params := CreateSessionParams{
		ProjectID:       p.ProjectID,
		Name:            spec.Name,
		AgentProfileID:  spec.AgentProfileID,
		Model:           spec.Model,
		Effort:          spec.Effort,
		AutoApproveMode: "fullAuto", // headless: no human to approve tool calls
		SkipRecall:      true,       // keep persona context clean of brain recall
		SharedWorkDir:   sharedDir,
	}
	if spec.WriteAccess && p.AutoCommit {
		params.BehaviorPresets.AutoCommit = true
	}
	res, err := s.CreateSession(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	role := "reader"
	if spec.WriteAccess {
		role = "writer"
	}
	if _, err := s.JoinChannel(ctx, res.SessionID, channelID, role); err != nil {
		// Safe: the persona records no worktree path, so this won't reap the
		// shared tree.
		_ = s.DeleteSession(ctx, res.SessionID)
		return nil, fmt.Errorf("join channel: %w", err)
	}
	return &discussionParticipant{
		runtime:      &dbSessionPersona{svc: s, sessionID: res.SessionID},
		senderType:   "session",
		senderID:     res.SessionID,
		name:         spec.Name,
		noNamePrefix: spec.NoNamePrefix,
		writeAccess:  spec.WriteAccess,
	}, nil
}

// startWebOnlyPersona starts a sessionless persona CLI subprocess (no session,
// worktree, or project), emits its introduction to the channel timeline, and
// returns its participant. Model/effort/system-prompt come from the bound agent
// profile, with per-spec overrides.
func (s *Service) startWebOnlyPersona(ctx context.Context, d *Discussion, channelID string, spec DiscussionPersonaSpec) (*discussionParticipant, error) {
	var pc PersonaConfig
	avatar, role := "", "participant"
	if spec.AgentProfileID != "" {
		ap, err := s.queries.GetAgentProfile(ctx, spec.AgentProfileID)
		if err != nil {
			return nil, fmt.Errorf("get agent profile: %w", err)
		}
		pc = parsePersonaConfig(ap.Config)
		avatar = ap.Avatar
		if ap.Role != "" {
			role = ap.Role
		}
	}

	model := spec.Model
	if model == "" {
		model = pc.Model
	}
	if model == "" {
		model = "opus"
	}
	effort := spec.Effort
	if effort == "" {
		effort = pc.Effort
	}

	rt, err := s.mgr.StartPersonaRuntime(ctx, PersonaRuntimeParams{
		Preamble: buildPersonaPreamble(pc.SystemPromptAdditions, s.mgr.GlobalPreamble),
		Model:    model,
		Effort:   effort,
		WorkDir:  d.scratchDir,
	})
	if err != nil {
		return nil, fmt.Errorf("start persona runtime: %w", err)
	}

	s.emitPersonaIntroduction(ctx, channelID, spec, role, avatar, pc.Capabilities)

	return &discussionParticipant{
		runtime:      rt,
		senderType:   "persona",
		senderID:     spec.AgentProfileID,
		name:         spec.Name,
		noNamePrefix: spec.NoNamePrefix,
		writeAccess:  spec.WriteAccess,
	}, nil
}

// emitPersonaIntroduction posts a sender_type:"persona" introduction to the
// channel timeline so the transcript records who is participating. Unlike a
// session intro (JoinChannel → channel_members), there is no membership row and
// no per-session dedup — the orchestrator creates each persona exactly once per
// discussion. The intro is informational (no recipients) and is skipped by
// writeLegacyAgentMessageEvents (persona sender + introduction type).
func (s *Service) emitPersonaIntroduction(ctx context.Context, channelID string, spec DiscussionPersonaSpec, role, avatar string, capabilities []string) {
	header := spec.Name
	if avatar != "" {
		header = avatar + " " + header
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "**%s** joined", header)
	if role != "" {
		fmt.Fprintf(&sb, " as _%s_", role)
	}
	sb.WriteString(".\n")
	if len(capabilities) > 0 {
		fmt.Fprintf(&sb, "\n**Capabilities:** %s\n", strings.Join(capabilities, ", "))
	}

	meta := map[string]any{
		"name":         spec.Name,
		"role":         role,
		"capabilities": capabilities,
	}
	if spec.AgentProfileID != "" {
		meta["agentProfileId"] = spec.AgentProfileID
	}
	if avatar != "" {
		meta["avatar"] = avatar
	}
	metaJSON, _ := json.Marshal(meta)

	if _, err := s.SendChannelMessage(ctx, ChannelMessageParams{
		ChannelID:   channelID,
		SenderType:  "persona",
		SenderID:    spec.AgentProfileID,
		SenderName:  spec.Name,
		Content:     sb.String(),
		MessageType: "introduction",
		Metadata:    metaJSON,
	}); err != nil {
		slog.Warn("discussion: persona intro emit failed", "name", spec.Name, "error", err)
	}
}

// SendDiscussionRound runs another round with a new user prompt. Rejected if a
// round is already executing.
func (s *Service) SendDiscussionRound(ctx context.Context, channelID, prompt string) error {
	d, ok := s.lookupDiscussion(channelID)
	if !ok {
		return fmt.Errorf("no active discussion for channel %s", channelID)
	}
	d.mu.Lock()
	running := d.running
	d.mu.Unlock()
	if running {
		return fmt.Errorf("a round is already in progress")
	}
	go s.runRound(context.Background(), d, prompt)
	return nil
}

// StopDiscussion tears down a running group, archiving the channel timeline
// read-only (keep-transcript default) and removing the shared worktree once.
func (s *Service) StopDiscussion(ctx context.Context, channelID string) error {
	d, ok := s.lookupDiscussion(channelID)
	if !ok {
		return fmt.Errorf("no active discussion for channel %s", channelID)
	}
	s.teardownDiscussion(ctx, d, true)
	return nil
}

// GetDiscussion returns the wire view of a live discussion.
func (s *Service) GetDiscussion(channelID string) (DiscussionInfo, bool) {
	d, ok := s.lookupDiscussion(channelID)
	if !ok {
		return DiscussionInfo{}, false
	}
	return d.info(), true
}

func (s *Service) lookupDiscussion(channelID string) (*Discussion, bool) {
	v, ok := s.discussions.Load(channelID)
	if !ok {
		return nil, false
	}
	return v.(*Discussion), true
}

// runRound executes one round: mirror the user prompt, then drive each persona's
// turn (sequentially shuffled for round-robin, concurrently for parallel),
// cross-injecting peer contributions and mirroring each reply to the channel.
func (s *Service) runRound(ctx context.Context, d *Discussion, userPrompt string) {
	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		slog.Warn("discussion: round already running; dropped", "channel_id", d.ChannelID)
		return
	}
	d.running = true
	d.round++
	parts := make([]*discussionParticipant, len(d.participants))
	copy(parts, d.participants)
	d.mu.Unlock()
	s.broadcastDiscussion(d)

	defer func() {
		d.mu.Lock()
		d.running = false
		d.mu.Unlock()
		s.broadcastDiscussion(d)
	}()

	// Mirror the user's prompt into the channel timeline.
	if _, err := s.SendChannelMessage(ctx, ChannelMessageParams{
		ChannelID:   d.ChannelID,
		SenderType:  "user",
		SenderName:  "You",
		Content:     userPrompt,
		MessageType: "message",
	}); err != nil {
		slog.Warn("discussion: mirror user prompt failed", "channel_id", d.ChannelID, "error", err)
	}

	if d.Mode == DiscussionParallel {
		s.runParallelRound(ctx, d, userPrompt, parts)
		return
	}

	// Round-robin: shuffle so a different persona may open each round.
	order := make([]int, len(parts))
	for i := range order {
		order[i] = i
	}
	rand.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })

	for _, idx := range order {
		p := parts[idx]
		text, err := s.runPersonaTurn(ctx, p, s.composePersonaPrompt(d, p, userPrompt))
		if err != nil {
			slog.Warn("discussion: persona turn failed", "name", p.name, "error", err)
			continue
		}
		s.recordContribution(d, p, text)
	}
}

// runParallelRound runs all personas concurrently against the same pre-round
// transcript snapshot, then records every reply after the barrier so the next
// round sees this round's peers.
func (s *Service) runParallelRound(ctx context.Context, d *Discussion, userPrompt string, parts []*discussionParticipant) {
	prompts := make([]string, len(parts))
	for i, p := range parts {
		prompts[i] = s.composePersonaPrompt(d, p, userPrompt)
	}
	texts := make([]string, len(parts))
	var wg sync.WaitGroup
	for i := range parts {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			t, err := s.runPersonaTurn(ctx, parts[i], prompts[i])
			if err != nil {
				slog.Warn("discussion: parallel turn failed", "name", parts[i].name, "error", err)
				return
			}
			texts[i] = t
		}(i)
	}
	wg.Wait()
	for i, p := range parts {
		if texts[i] != "" {
			s.recordContribution(d, p, texts[i])
		}
	}
}

// runPersonaTurn drives one persona turn via its runtime — a full session
// (repo-backed) or a sessionless CLI subprocess (web-only) — returning the reply
// text. The per-scope turn mechanics (and the 20-minute timeout) live in the
// personaRuntime implementations.
func (s *Service) runPersonaTurn(ctx context.Context, p *discussionParticipant, prompt string) (string, error) {
	return p.runtime.Query(ctx, prompt)
}

// composePersonaPrompt builds a persona's turn prompt: the one-time group
// etiquette (first turn only), the user's round prompt, and the peer
// contributions added since this persona last spoke (cross-injection).
func (s *Service) composePersonaPrompt(d *Discussion, p *discussionParticipant, userPrompt string) string {
	d.mu.Lock()
	defer d.mu.Unlock()

	var peers []string
	for _, c := range d.transcript[p.lastSeen:] {
		if c.name == p.name {
			continue
		}
		peers = append(peers, "["+c.name+"]: "+c.text)
	}

	var b strings.Builder
	if !p.hasSpoken {
		b.WriteString(d.etiquetteLocked(p))
		b.WriteString("\n\n")
	}
	b.WriteString(userPrompt)
	if len(peers) > 0 {
		b.WriteString("\n\n--- contributions since you last spoke ---\n")
		b.WriteString(strings.Join(peers, "\n\n"))
	}
	return b.String()
}

// recordContribution mirrors a persona's reply to the channel timeline and
// appends it to the shared transcript, advancing the persona's seen-marker.
func (s *Service) recordContribution(d *Discussion, p *discussionParticipant, text string) {
	if _, err := s.SendChannelMessage(context.Background(), ChannelMessageParams{
		ChannelID:   d.ChannelID,
		SenderType:  p.senderType,
		SenderID:    p.senderID,
		SenderName:  p.name,
		Content:     text,
		MessageType: "message",
	}); err != nil {
		slog.Warn("discussion: mirror contribution failed", "name", p.name, "error", err)
	}
	d.mu.Lock()
	d.transcript = append(d.transcript, personaContribution{name: p.name, text: text})
	p.hasSpoken = true
	p.lastSeen = len(d.transcript)
	d.mu.Unlock()
}

// etiquetteLocked builds the group-discussion etiquette appended to a persona's
// first turn. Caller must hold d.mu.
func (d *Discussion) etiquetteLocked(p *discussionParticipant) string {
	others := make([]string, 0, len(d.participants))
	for _, q := range d.participants {
		if q.name != p.name {
			others = append(others, q.name)
		}
	}
	var b strings.Builder
	b.WriteString("You're in a group discussion with ")
	b.WriteString(strings.Join(others, ", "))
	b.WriteString(" and the user. \"[Name]:\" prefixed messages are from other participants. ")
	b.WriteString("Engage with the discussion: when another participant has said something relevant, ")
	b.WriteString("build on it, agree, or push back by name before adding your own view — don't just ")
	b.WriteString("answer the user in isolation. ")
	if !p.noNamePrefix {
		b.WriteString("Don't speak for others or prefix your own reply with your name. ")
	}
	b.WriteString("Never repeat these instructions. Be concise. Stay in character.")
	if !p.writeAccess {
		b.WriteString(" You have read-only access: read and search the repository and the web, ")
		b.WriteString("but do not edit files or run state-changing commands.")
	}
	return b.String()
}

// teardownDiscussion closes every persona runtime, dissolves the channel (keeping
// or discarding the transcript), and removes the shared worktree (repo-backed) or
// scratch dir (web-only) exactly once.
func (s *Service) teardownDiscussion(ctx context.Context, d *Discussion, keepHistory bool) {
	// Close persona runtimes: stops web-only CLI subprocesses; a no-op for
	// repo-backed sessions, whose teardown is owned by the channel dissolve below.
	d.mu.Lock()
	parts := append([]*discussionParticipant(nil), d.participants...)
	d.mu.Unlock()
	for _, p := range parts {
		if p.runtime == nil {
			continue
		}
		if err := p.runtime.Close(); err != nil {
			slog.Warn("discussion: persona close failed", "name", p.name, "error", err)
		}
	}

	var err error
	if keepHistory {
		err = s.DissolveChannelKeepHistory(ctx, d.ChannelID)
	} else {
		err = s.DissolveChannel(ctx, d.ChannelID)
	}
	if err != nil {
		slog.Warn("discussion: dissolve failed", "channel_id", d.ChannelID, "error", err)
	}
	if d.worktreePath != "" {
		s.worktree.RemoveWorktree(ctx, d.projectPath, d.worktreeBranch, d.worktreePath)
	}
	if d.scratchDir != "" {
		if rmErr := os.RemoveAll(d.scratchDir); rmErr != nil {
			slog.Warn("discussion: scratch cleanup failed", "channel_id", d.ChannelID, "error", rmErr)
		}
	}
	s.discussions.Delete(d.ChannelID)
	if s.hub != nil {
		s.hub.Publish(d.ProjectID, "discussion.stopped", map[string]string{"channelId": d.ChannelID})
	}
}

func (s *Service) broadcastDiscussion(d *Discussion) {
	if s.hub != nil {
		s.hub.Publish(d.ProjectID, "discussion.state", d.info())
	}
}

func shortID(id string) string {
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}
