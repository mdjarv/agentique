package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/mdjarv/agentique/backend/internal/assistant"
	"github.com/mdjarv/agentique/backend/internal/machine"
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
	// peerMaxBody bounds one remote list. A session row is a couple of KB, so
	// this is thousands of sessions, and it is another machine's bytes.
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
}

// peerSnapshot is one machine's answer, before it is turned into rows.
type peerSnapshot struct {
	Sessions []peerSessionWire
	Projects []peerProjectWire
}

// peerSessionWire is the part of a remote GET /api/sessions row the assistant
// reads. Narrow on purpose: the remote runs whatever release it runs, and a
// field this server does not need must not be able to fail the decode.
type peerSessionWire struct {
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

// peerProjectWire is the part of a remote GET /api/projects row the assistant
// reads.
type peerProjectWire struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	RemoteURL string `json:"remote_url"`
}

type peerFetchFunc func(ctx context.Context, m store.Machine) (peerSnapshot, error)

type peerEntry struct {
	snapshot  peerSnapshot
	err       error
	fetchedAt time.Time
	// inflight is closed when the running refresh lands; nil when none runs.
	inflight chan struct{}
}

// peerSessions reads the sessions of every paired machine, as this server,
// with the bearer the catalog already holds for it.
//
// It exists because the browser was the only thing that ever asked a paired
// machine what it was running. The sidebar fans out to every machine over its
// own socket, so the operator sees a zbook session beside a local one; the
// assistant asks this server, which read its own database and nothing else,
// so the same session did not exist for it. The voice call papered over that
// with the browser's world snapshot, and the thread has no browser behind it.
//
// Reading is all it does. Rows from here carry no ProjectID and
// [assistantDirectory.SessionBrief] never answers for them, so they can make
// the assistant say things and never do things — the same contract the world
// snapshot has, now held by a source that does not need a tab open.
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
}

// newPeerSessions builds the reader. It does no IO: the first read is what
// reaches out, so constructing it in a test touches nothing.
func newPeerSessions(queries *store.Queries, client *http.Client, selfID string) *peerSessions {
	return &peerSessions{
		machines: queries.ListMachines,
		localKey: func(ctx context.Context) map[string]store.Project {
			return localProjectsByRemote(ctx, queries)
		},
		fetch:      httpPeerFetch(client),
		selfID:     selfID,
		now:        time.Now,
		coldBudget: peerColdBudget,
		entries:    make(map[string]*peerEntry),
	}
}

// httpPeerFetch reads a machine's sessions and projects over its REST API.
// Both are endpoints every release that can pair already serves.
func httpPeerFetch(client *http.Client) peerFetchFunc {
	return func(ctx context.Context, m store.Machine) (peerSnapshot, error) {
		peer := machine.RemotePeer{BaseURL: m.BaseUrl, MachineID: m.MachineID, IdentityKey: m.IdentityKey, Token: m.Token}
		var snap peerSnapshot
		if err := machine.FetchRemoteJSON(ctx, client, peer, "/api/sessions", peerMaxBody, &snap.Sessions); err != nil {
			return peerSnapshot{}, err
		}
		// Projects only name the rows. A machine whose project list fails
		// still has sessions worth listing, so this failure is not the read's.
		if err := machine.FetchRemoteJSON(ctx, client, peer, "/api/projects", peerMaxBody, &snap.Projects); err != nil {
			slog.Debug("peer sessions: project list failed", "machine", m.MachineID, "error", err)
		}
		return snap, nil
	}
}

// View implements peerSource.
func (p *peerSessions) View(ctx context.Context) peerView {
	catalog, err := p.machines(ctx)
	if err != nil {
		slog.Warn("peer sessions: machine catalog read failed", "error", err)
		return peerView{}
	}
	peers := catalog[:0:0]
	for _, m := range catalog {
		if m.MachineID == "" || m.MachineID == p.selfID {
			continue
		}
		peers = append(peers, m)
	}
	if len(peers) == 0 {
		return peerView{}
	}

	p.forget(peers)
	p.awaitCold(ctx, peers)

	local := p.localKey(ctx)
	var view peerView
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, m := range peers {
		entry := p.entries[m.MachineID]
		if entry == nil || entry.fetchedAt.IsZero() || p.now().Sub(entry.fetchedAt) > peerStaleFor || entry.err != nil {
			view.Unreachable = append(view.Unreachable, machineLabel(m))
			continue
		}
		view.Rows = append(view.Rows, peerRows(m, entry.snapshot, local)...)
	}
	return view
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
	projects := make(map[string]peerProjectWire, len(snap.Projects))
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
		}
		if project, ok := projects[s.ProjectID]; ok {
			row.ProjectName = clampPeerField(project.Name)
			row.ProjectSlug = clampPeerField(project.Slug)
			if here, ok := local[project.RemoteURL]; ok && project.RemoteURL != "" {
				row.ProjectName = here.Name
				row.ProjectSlug = here.Slug
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// peerAttention is [attentionOf] for a remote row: the same three reasons in
// the same order, read from the wire.
func peerAttention(s peerSessionWire) string {
	if present(s.PendingApproval) {
		return assistant.AttentionApproval
	}
	if present(s.PendingQuestion) {
		return assistant.AttentionQuestion
	}
	if s.UnseenCompletedAt != nil && *s.UnseenCompletedAt != "" && s.State != assistant.StateRunning {
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
