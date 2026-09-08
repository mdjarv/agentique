package session

import (
	"sync"
	"time"
)

// session.list never shells out.
//
// Branch status (ahead, behind, dirty, merge-tree) costs five git subprocesses
// per unmerged worktree session, and a project with thirty sessions paid that
// on every list — half a second on the socket's serial lane, per project,
// before the page could mount. So the list reads this cache and never
// computes: a miss or a stale entry is queued for one background worker,
// which recomputes the session's full snapshot and broadcasts it as
// `session.state`, the push the client already applies. The row is right
// within a second and the list answers in milliseconds either way.
//
// Every path that computes a status fresh (a live session's own refresh, an
// explicit refresh-git, the worker) stores it here, so for a live session the
// cache is current by construction and the TTL only covers what nothing
// reports — the project's HEAD moving under it.

// branchStatusTTL is how long a cached status is served before the worker
// is asked for a fresh one. Serving continues meanwhile: stale beats slow,
// and the refresh corrects the row on its own.
const branchStatusTTL = 60 * time.Second

// branchStatusKey names what a status was computed from. A worktree session
// is keyed by its branch; a local session by the directory it runs in.
type branchStatusKey struct {
	projectPath string
	branch      string
	workDir     string
}

type branchStatusEntry struct {
	key    branchStatusKey
	status branchStatus
	at     time.Time
}

type branchStatusCache struct {
	mu      sync.Mutex
	entries map[string]branchStatusEntry
	queued  map[string]bool
	queue   []string
	wake    chan struct{}
	refresh func(sessionID string)
	once    sync.Once
	now     func() time.Time
}

func newBranchStatusCache() *branchStatusCache {
	return &branchStatusCache{
		entries: make(map[string]branchStatusEntry),
		queued:  make(map[string]bool),
		wake:    make(chan struct{}, 1),
		now:     time.Now,
	}
}

// setRefresher installs what the worker calls for a queued session. Until
// one is set, requests queue and wait.
func (c *branchStatusCache) setRefresher(fn func(sessionID string)) {
	c.mu.Lock()
	c.refresh = fn
	c.mu.Unlock()
	c.kick()
}

// get returns the cached status for a session when it was computed from the
// same inputs.
func (c *branchStatusCache) get(sessionID string, key branchStatusKey) (branchStatusEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[sessionID]
	if !ok || e.key != key {
		return branchStatusEntry{}, false
	}
	return e, true
}

// put records a freshly computed status.
func (c *branchStatusCache) put(sessionID string, key branchStatusKey, status branchStatus) {
	c.mu.Lock()
	c.entries[sessionID] = branchStatusEntry{key: key, status: status, at: c.now()}
	c.mu.Unlock()
}

// forget drops a session's entry, for a deleted session.
func (c *branchStatusCache) forget(sessionID string) {
	c.mu.Lock()
	delete(c.entries, sessionID)
	c.mu.Unlock()
}

// stale reports whether an entry is older than the TTL.
func (c *branchStatusCache) stale(e branchStatusEntry) bool {
	return c.now().Sub(e.at) > branchStatusTTL
}

// request queues a background refresh for a session. Idempotent while the
// session is already queued, so a list of thirty sessions asked twice costs
// thirty refreshes, not sixty.
func (c *branchStatusCache) request(sessionID string) {
	c.mu.Lock()
	if c.queued[sessionID] {
		c.mu.Unlock()
		return
	}
	c.queued[sessionID] = true
	c.queue = append(c.queue, sessionID)
	c.mu.Unlock()
	c.once.Do(func() { go c.work() })
	c.kick()
}

func (c *branchStatusCache) kick() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// work drains the queue one session at a time. One worker on purpose: the
// point is to take git subprocesses off the request path, not to run thirty
// of them at once.
func (c *branchStatusCache) work() {
	for range c.wake {
		for {
			c.mu.Lock()
			if len(c.queue) == 0 || c.refresh == nil {
				c.mu.Unlock()
				break
			}
			id := c.queue[0]
			c.queue = c.queue[1:]
			delete(c.queued, id)
			refresh := c.refresh
			c.mu.Unlock()
			refresh(id)
		}
	}
}

// pending reports how many sessions wait for a refresh (tests).
func (c *branchStatusCache) pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.queue)
}
