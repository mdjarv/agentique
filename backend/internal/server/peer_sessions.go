package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/mdjarv/agentique/backend/internal/assistant"
	"github.com/mdjarv/agentique/backend/internal/machine"
	"github.com/mdjarv/agentique/backend/internal/peer"
	"github.com/mdjarv/agentique/backend/internal/peerlink"
	"github.com/mdjarv/agentique/backend/internal/providers"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// The peer reader's clocks.
//
// Fresh is how long a machine's answer is used as-is. Stale is how long it is
// still served while a refresh runs behind it — a tool call is dead air on a
// live call, so an answer the server already holds beats a round trip to
// another machine. Past stale the rows describe nothing worth saying, and the
// read waits for the machine, but only for coldBudget: a machine that is
// asleep must cost a sentence ("zbook did not answer"), never a hung tool.
const (
	peerFreshFor    = 20 * time.Second
	peerStaleFor    = 5 * time.Minute
	peerColdBudget  = 2500 * time.Millisecond
	peerFetchBudget = 8 * time.Second
	// peerMaxBody bounds one remote list on the transitional read. A session
	// row is a couple of KB, so this is thousands of sessions.
	peerMaxBody = 8 << 20
	// peerMaxField bounds one remote string on a rune boundary, the same
	// discipline the voice call applies to the browser's world snapshot.
	peerMaxField = 200
)

// peerView is what the paired machines said, as of the last time they said it.
type peerView struct {
	// Rows are the unarchived sessions of every machine that answered.
	Rows []assistant.SessionRow
	// Unreachable names the machines that did not, so a list without them can
	// say so rather than read as "there is nothing there".
	Unreachable []string
}

// peerSource is the seam [assistantDirectory] reads paired machines through.
type peerSource interface {
	View(ctx context.Context) peerView
	// Projects is every paired machine's checkouts, with what this server may
	// do in each.
	Projects(ctx context.Context) []assistant.ProjectRow
	// Locate finds a session on a paired machine by id, from the same cached
	// answers the lists come from.
	Locate(ctx context.Context, sessionID string) (peerLocation, bool)
	// Remember records a session this server just created on a paired machine,
	// so it can be acted on before that machine's next list includes it.
	Remember(loc peerLocation)
	// Invalidate makes the next read of one machine ask it again.
	Invalidate(machineID string)
}

// peerLocation is a session on a paired machine, with the facts an action on
// it is judged by.
type peerLocation struct {
	Machine store.Machine
	Session peer.SessionWire
	Row     assistant.SessionRow
}

// peerSnapshot is one machine's answer, normalized to the peer surface's shape
// whichever read produced it.
type peerSnapshot struct {
	Sessions        []peer.SessionWire
	Projects        []peer.ProjectWire
	Reach           assistant.Reach
	AcceptsPolicies bool
}

// peerLister is the one call the reader needs from the peer client.
type peerLister interface {
	List(ctx context.Context, machineID string) (peer.SessionsResponse, error)
}

type peerFetchFunc func(ctx context.Context, m store.Machine) (peerSnapshot, error)

type peerEntry struct {
	snapshot  peerSnapshot
	err       error
	fetchedAt time.Time
	// inflight is closed when the running refresh lands; nil when none runs.
	inflight chan struct{}
}

// peerSessions reads the sessions of every paired machine, as this server.
//
// It exists because the browser was the only thing that ever asked a paired
// machine what it was running. The sidebar fans out to every machine over its
// own socket, so the operator sees a zbook session beside a local one; the
// assistant asks this server, which read its own database and nothing else,
// so the same session did not exist for it.
//
// It reads through the peer surface where the machine serves one, with the peer
// credential, and tags every row with what the owner allows ([assistant.Reach]).
// A machine on a release from before the peer surface is read the transitional
// way — its REST lists with the pairing bearer — and every row from it is
// [assistant.ReachPeerOld]: described, never acted on. That read goes away once
// no paired release predates the surface (docs/peers.md, contract).
type peerSessions struct {
	machines func(ctx context.Context) ([]store.Machine, error)
	localKey func(ctx context.Context) map[string]store.Project
	fetch    peerFetchFunc
	selfID   string
	now      func() time.Time
	// coldBudget is peerColdBudget; a field so a test need not wait it out.
	coldBudget time.Duration

	mu      sync.Mutex
	entries map[string]*peerEntry
	// recent holds sessions created here moments ago, until a list has them.
	recent map[string]recentLocation
}

type recentLocation struct {
	loc peerLocation
	at  time.Time
}

// recentFor is how long a remembered creation stands in for a list: past two
// refreshes the machine's own answer is the only one worth trusting.
const recentFor = 2 * peerFreshFor

// newPeerSessions builds the reader. It does no IO: the first read is what
// reaches out, so constructing it in a test touches nothing.
func newPeerSessions(queries *store.Queries, client *http.Client, link peerLister, selfID string) *peerSessions {
	return &peerSessions{
		machines: queries.ListMachines,
		localKey: func(ctx context.Context) map[string]store.Project {
			return localProjectsByRemote(ctx, queries)
		},
		fetch:      peerFetch(link, client),
		selfID:     selfID,
		now:        time.Now,
		coldBudget: peerColdBudget,
		entries:    make(map[string]*peerEntry),
		recent:     make(map[string]recentLocation),
	}
}

// peerFetch reads one machine: the peer surface first, the transitional REST
// read only when the machine's release has no peer surface.
func peerFetch(link peerLister, client *http.Client) peerFetchFunc {
	return func(ctx context.Context, m store.Machine) (peerSnapshot, error) {
		if link != nil {
			list, err := link.List(ctx, m.MachineID)
			switch {
			case err == nil:
				reach := assistant.ReachPeerOff
				if list.AcceptActions {
					reach = assistant.ReachPeer
				}
				return peerSnapshot{Sessions: list.Sessions, Projects: list.Projects, Reach: reach,
					AcceptsPolicies: list.AcceptActions && list.AcceptPolicies}, nil
			case !errors.Is(err, peerlink.ErrNoPeerSurface):
				return peerSnapshot{}, err
			}
		}
		return transitionalFetch(ctx, client, m)
	}
}

// legacySessionWire is the part of an older release's GET /api/sessions row the
// transitional read uses. Narrow on purpose: a field this server does not need
// must not be able to fail the decode.
type legacySessionWire struct {
	ID                string          `json:"id"`
	ProjectID         string          `json:"projectId"`
	Name              string          `json:"name"`
	State             string          `json:"state"`
	Model             string          `json:"model"`
	WorktreeBranch    string          `json:"worktreeBranch"`
	ArchivedAt        string          `json:"archivedAt"`
	UnseenCompletedAt *string         `json:"unseenCompletedAt"`
	PendingApproval   json.RawMessage `json:"pendingApproval"`
	PendingQuestion   json.RawMessage `json:"pendingQuestion"`
	LastQueryAt       string          `json:"lastQueryAt"`
	UpdatedAt         string          `json:"updatedAt"`
	CreatedAt         string          `json:"createdAt"`
}

type legacyProjectWire struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	RemoteURL string `json:"remote_url"`
}

// transitionalFetch reads an older release's REST lists with the pairing
// bearer. Reading is all it is ever used for.
func transitionalFetch(ctx context.Context, client *http.Client, m store.Machine) (peerSnapshot, error) {
	remote := machine.RemotePeer{BaseURL: m.BaseUrl, MachineID: m.MachineID, IdentityKey: m.IdentityKey, Token: m.Token}
	var sessions []legacySessionWire
	if err := machine.FetchRemoteJSON(ctx, client, remote, "/api/sessions", peerMaxBody, &sessions); err != nil {
		return peerSnapshot{}, err
	}
	snap := peerSnapshot{Reach: assistant.ReachPeerOld}
	for _, s := range sessions {
		wire := peer.SessionWire{
			ID: s.ID, ProjectID: s.ProjectID, Name: s.Name, State: s.State, Model: s.Model,
			WorktreeBranch: s.WorktreeBranch, ArchivedAt: s.ArchivedAt,
			PendingApproval: present(s.PendingApproval), PendingQuestion: present(s.PendingQuestion),
			LastQueryAt: s.LastQueryAt, UpdatedAt: s.UpdatedAt, CreatedAt: s.CreatedAt,
		}
		if s.UnseenCompletedAt != nil {
			wire.UnseenCompletedAt = *s.UnseenCompletedAt
		}
		snap.Sessions = append(snap.Sessions, wire)
	}
	// Projects only name the rows. A machine whose project list fails still
	// has sessions worth listing, so this failure is not the read's.
	var projects []legacyProjectWire
	if err := machine.FetchRemoteJSON(ctx, client, remote, "/api/projects", peerMaxBody, &projects); err != nil {
		slog.Debug("peer sessions: project list failed", "machine", m.MachineID, "error", err)
	}
	for _, p := range projects {
		snap.Projects = append(snap.Projects, peer.ProjectWire{ID: p.ID, Name: p.Name, Slug: p.Slug, RemoteURL: p.RemoteURL})
	}
	return snap, nil
}

// View implements peerSource.
func (p *peerSessions) View(ctx context.Context) peerView {
	peers, local := p.refreshed(ctx)
	var view peerView
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, m := range peers {
		snap, ok := p.usableLocked(m)
		if !ok {
			view.Unreachable = append(view.Unreachable, machineLabel(m))
			continue
		}
		view.Rows = append(view.Rows, peerRows(m, snap, local)...)
	}
	return view
}

// Projects implements peerSource.
func (p *peerSessions) Projects(ctx context.Context) []assistant.ProjectRow {
	peers, local := p.refreshed(ctx)
	p.mu.Lock()
	defer p.mu.Unlock()
	var rows []assistant.ProjectRow
	for _, m := range peers {
		snap, ok := p.usableLocked(m)
		if !ok {
			continue
		}
		rows = append(rows, peerProjectRows(m, snap, local)...)
	}
	return rows
}

// Remember implements peerSource.
func (p *peerSessions) Remember(loc peerLocation) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.recent == nil {
		p.recent = make(map[string]recentLocation)
	}
	p.recent[loc.Session.ID] = recentLocation{loc: loc, at: p.now()}
	if entry := p.entries[loc.Machine.MachineID]; entry != nil && entry.err == nil && !entry.fetchedAt.IsZero() {
		entry.fetchedAt = p.now().Add(-peerFreshFor - time.Second)
	}
}

// Locate implements peerSource.
func (p *peerSessions) Locate(ctx context.Context, sessionID string) (peerLocation, bool) {
	if sessionID == "" {
		return peerLocation{}, false
	}
	peers, local := p.refreshed(ctx)
	p.mu.Lock()
	defer p.mu.Unlock()
	if r, ok := p.recent[sessionID]; ok {
		if p.now().Sub(r.at) <= recentFor {
			return r.loc, true
		}
		delete(p.recent, sessionID)
	}
	for _, m := range peers {
		snap, ok := p.usableLocked(m)
		if !ok {
			continue
		}
		for _, s := range snap.Sessions {
			if s.ID != sessionID {
				continue
			}
			rows := peerRows(m, peerSnapshot{Sessions: []peer.SessionWire{s}, Projects: snap.Projects,
				Reach: snap.Reach, AcceptsPolicies: snap.AcceptsPolicies}, local)
			row := assistant.SessionRow{ID: s.ID, MachineID: m.MachineID, MachineName: machineLabel(m), Reach: snap.Reach}
			if len(rows) == 1 {
				row = rows[0]
			} else {
				// Archived rows are dropped from lists but still addressable:
				// the owner's guard is what refuses them, in its own words.
				row.Name = clampPeerField(s.Name)
			}
			return peerLocation{Machine: m, Session: s, Row: row}, true
		}
	}
	return peerLocation{}, false
}

// refreshed is the catalog minus this machine, with every stale answer
// refreshed (and the cold ones waited on, boundedly), plus this machine's
// projects keyed by remote for naming.
func (p *peerSessions) refreshed(ctx context.Context) ([]store.Machine, map[string]store.Project) {
	catalog, err := p.machines(ctx)
	if err != nil {
		slog.Warn("peer sessions: machine catalog read failed", "error", err)
		return nil, nil
	}
	peers := catalog[:0:0]
	for _, m := range catalog {
		if m.MachineID == "" || m.MachineID == p.selfID {
			continue
		}
		peers = append(peers, m)
	}
	if len(peers) == 0 {
		return nil, nil
	}
	p.forget(peers)
	p.awaitCold(ctx, peers)
	return peers, p.localKey(ctx)
}

// usableLocked answers a machine's snapshot if it is recent and not a failure.
// p.mu must be held.
func (p *peerSessions) usableLocked(m store.Machine) (peerSnapshot, bool) {
	entry := p.entries[m.MachineID]
	if entry == nil || entry.fetchedAt.IsZero() || p.now().Sub(entry.fetchedAt) > peerStaleFor || entry.err != nil {
		return peerSnapshot{}, false
	}
	return entry.snapshot, true
}

// awaitCold starts a refresh for every machine that needs one, and waits —
// boundedly — only for those with nothing usable yet.
func (p *peerSessions) awaitCold(ctx context.Context, peers []store.Machine) {
	var cold []chan struct{}
	p.mu.Lock()
	for _, m := range peers {
		entry := p.entries[m.MachineID]
		if entry == nil {
			entry = &peerEntry{}
			p.entries[m.MachineID] = entry
		}
		age := p.now().Sub(entry.fetchedAt)
		if !entry.fetchedAt.IsZero() && age <= peerFreshFor {
			continue
		}
		// Waiting is for a machine this reader has nothing from, once. One
		// already being refreshed was waited on by the read that started it,
		// and one whose last answer was a failure is presumed still away: an
		// asleep machine must cost one bounded wait, not one on every call.
		wait := entry.inflight == nil && entry.err == nil && (entry.fetchedAt.IsZero() || age > peerStaleFor)
		done := p.refreshLocked(m, entry)
		if wait {
			cold = append(cold, done)
		}
	}
	p.mu.Unlock()
	if len(cold) == 0 {
		return
	}

	timer := time.NewTimer(p.coldBudget)
	defer timer.Stop()
	for _, done := range cold {
		select {
		case <-done:
		case <-timer.C:
			return
		case <-ctx.Done():
			return
		}
	}
}

// Invalidate marks one machine's answer stale, so the next read refreshes it —
// after an action there changed what it would say.
func (p *peerSessions) Invalidate(machineID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if entry := p.entries[machineID]; entry != nil && entry.err == nil && !entry.fetchedAt.IsZero() {
		entry.fetchedAt = p.now().Add(-peerFreshFor - time.Second)
	}
}

// refreshLocked starts one refresh for m unless one already runs, and returns
// the channel that closes when it lands. p.mu must be held.
//
// The fetch runs on a detached, separately bounded context: a read that gave
// up waiting still wants the answer cached for the next one.
func (p *peerSessions) refreshLocked(m store.Machine, entry *peerEntry) chan struct{} {
	if entry.inflight != nil {
		return entry.inflight
	}
	done := make(chan struct{})
	entry.inflight = done
	go func() {
		defer close(done)
		ctx, cancel := context.WithTimeout(context.Background(), peerFetchBudget)
		defer cancel()
		snap, err := p.fetch(ctx, m)
		if err != nil {
			slog.Debug("peer sessions: machine did not answer", "machine", m.MachineID, "error", err)
		}

		p.mu.Lock()
		defer p.mu.Unlock()
		entry.inflight = nil
		entry.fetchedAt = p.now()
		entry.err = err
		// A failed refresh drops what the machine said before. Its sessions are
		// unknown now, and a "running" read from minutes ago would be said as
		// though it were current.
		if err != nil {
			entry.snapshot = peerSnapshot{}
			return
		}
		entry.snapshot = snap
	}()
	return done
}

// forget drops the cache of a machine that left the catalog, so an unpaired
// machine's sessions stop being described the moment it is removed.
func (p *peerSessions) forget(peers []store.Machine) {
	keep := make(map[string]bool, len(peers))
	for _, m := range peers {
		keep[m.MachineID] = true
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for id := range p.entries {
		if !keep[id] {
			delete(p.entries, id)
		}
	}
}

// peerRows turns one machine's answer into speakable rows.
//
// The project is named the way this host names it where it can be: a repository
// checked out here too is one logical project, and its presentation belongs to
// the host whose surface is asking (docs/multi-machine.md). Where it is not
// checked out here, the remote's own name is all there is.
func peerRows(m store.Machine, snap peerSnapshot, local map[string]store.Project) []assistant.SessionRow {
	projects := make(map[string]peer.ProjectWire, len(snap.Projects))
	for _, project := range snap.Projects {
		projects[project.ID] = project
	}

	label := machineLabel(m)
	rows := make([]assistant.SessionRow, 0, len(snap.Sessions))
	for _, s := range snap.Sessions {
		if s.ID == "" || s.ArchivedAt != "" {
			continue
		}
		row := assistant.SessionRow{
			ID:           clampPeerField(s.ID),
			Name:         clampPeerField(s.Name),
			MachineID:    m.MachineID,
			MachineName:  label,
			State:        clampPeerField(s.State),
			Attention:    peerAttention(s),
			Branch:       clampPeerField(s.WorktreeBranch),
			Model:        providers.ModelFamilyName(s.Model),
			LastActivity: clampPeerField(firstNonEmptyOf(s.LastQueryAt, s.UpdatedAt, s.CreatedAt)),
			// ProjectID stays empty: a remote project id means nothing here, and
			// it is what files a memory scope or a journal subject.
			Reach:           snap.Reach,
			AcceptsPolicies: snap.AcceptsPolicies,
		}
		if project, ok := projects[s.ProjectID]; ok {
			row.ProjectName, row.ProjectSlug = presentProject(project, local)
		}
		rows = append(rows, row)
	}
	return rows
}

// peerProjectRows turns one machine's projects into rows a session could be
// created in, when its reach allows.
func peerProjectRows(m store.Machine, snap peerSnapshot, local map[string]store.Project) []assistant.ProjectRow {
	lastWork := make(map[string]string)
	for _, s := range snap.Sessions {
		at := firstNonEmptyOf(s.LastQueryAt, s.UpdatedAt, s.CreatedAt)
		if at > lastWork[s.ProjectID] {
			lastWork[s.ProjectID] = at
		}
	}
	label := machineLabel(m)
	rows := make([]assistant.ProjectRow, 0, len(snap.Projects))
	for _, project := range snap.Projects {
		name, slug := presentProject(project, local)
		rows = append(rows, assistant.ProjectRow{
			// The owner's own id: it is what the owner's create resolves, and it
			// is only ever sent back to that machine.
			ID:              clampPeerField(project.ID),
			Name:            name,
			Slug:            slug,
			LastActivity:    clampPeerField(lastWork[project.ID]),
			MachineID:       m.MachineID,
			MachineName:     label,
			RemoteURL:       clampPeerField(project.RemoteURL),
			Reach:           snap.Reach,
			AcceptsPolicies: snap.AcceptsPolicies,
		})
	}
	return rows
}

func presentProject(project peer.ProjectWire, local map[string]store.Project) (string, string) {
	if here, ok := local[project.RemoteURL]; ok && project.RemoteURL != "" {
		return here.Name, here.Slug
	}
	return clampPeerField(project.Name), clampPeerField(project.Slug)
}

// peerAttention is [attentionOf] for a remote row: the same three reasons in
// the same order, read from the wire.
func peerAttention(s peer.SessionWire) string {
	if s.PendingApproval {
		return assistant.AttentionApproval
	}
	if s.PendingQuestion {
		return assistant.AttentionQuestion
	}
	if s.UnseenCompletedAt != "" && s.State != assistant.StateRunning {
		return assistant.AttentionUnread
	}
	return ""
}

func present(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed != "" && trimmed != "null"
}

// machineLabel is what to call a paired machine out loud.
func machineLabel(m store.Machine) string {
	if label := clampPeerField(m.Label); label != "" {
		return label
	}
	return "a paired machine"
}

// localProjectsByRemote indexes this machine's projects by canonical git
// remote, the key cross-machine grouping already merges rows by.
func localProjectsByRemote(ctx context.Context, queries *store.Queries) map[string]store.Project {
	list, err := queries.ListProjects(ctx)
	if err != nil {
		return nil
	}
	out := make(map[string]store.Project, len(list))
	for _, project := range list {
		if project.RemoteUrl == "" {
			continue
		}
		if _, taken := out[project.RemoteUrl]; !taken {
			out[project.RemoteUrl] = project
		}
	}
	return out
}

// clampPeerField bounds one remote string on a rune boundary and folds its
// whitespace, because it is another machine's text on its way into a prompt.
func clampPeerField(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= peerMaxField {
		return s
	}
	return string([]rune(s)[:peerMaxField])
}
