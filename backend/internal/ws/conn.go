package ws

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/allbin/agentkit/eventbus"
	"github.com/gorilla/websocket"
	"github.com/mdjarv/agentique/backend/internal/logging"
	"github.com/mdjarv/agentique/backend/internal/persona"
	"github.com/mdjarv/agentique/backend/internal/project"
	"github.com/mdjarv/agentique/backend/internal/providers"
	"github.com/mdjarv/agentique/backend/internal/schedule"
	"github.com/mdjarv/agentique/backend/internal/session"
	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/mdjarv/agentique/backend/internal/team"
)

const (
	writeTimeout           = 10 * time.Second
	pongTimeout            = 60 * time.Second
	pingInterval           = 30 * time.Second
	sendBufSize            = 256
	dispatchBufSize        = 256
	defaultMaxMessageBytes = 32 << 20
)

type conn struct {
	ctx             context.Context
	cancel          context.CancelFunc
	ws              *websocket.Conn
	svc             *session.Service
	gitSvc          *session.GitService
	projectGitSvc   *project.GitService
	queries         *store.Queries
	bus             *eventbus.Bus
	teamSvc         *team.Service           // nil when experimental teams is disabled
	personaSvc      *persona.Service        // nil when experimental teams is disabled
	browserSvc      *session.BrowserService // nil when browser support is unavailable
	scheduleSvc     *schedule.Scheduler     // nil when the scheduler is disabled
	catalog         *providers.Catalog      // model catalog; nil = base aliases only
	sendCh          chan any
	dispatchCh      chan ClientMessage
	readSlots       chan struct{} // read-lane semaphore; see dispatchLoop
	maxMessageBytes int64
	mu              sync.Mutex
	closeOnce       sync.Once

	// One multi-topic subscription per conn. Membership is owned by the
	// Subscription itself (AddTopic/RemoveTopic), so each Publish or
	// Broadcast delivers to OnEvent at most once regardless of how many
	// projects are joined.
	sub *eventbus.Subscription
}

func newConn(parentCtx context.Context, ws *websocket.Conn, svc *session.Service, gitSvc *session.GitService, projectGitSvc *project.GitService, queries *store.Queries, bus *eventbus.Bus, teamSvc *team.Service, personaSvc *persona.Service, browserSvc *session.BrowserService, scheduleSvc *schedule.Scheduler, catalog *providers.Catalog, maxMessageBytes int64) *conn {
	ctx, cancel := context.WithCancel(parentCtx)
	if maxMessageBytes <= 0 {
		maxMessageBytes = defaultMaxMessageBytes
	}
	c := &conn{
		ctx:             ctx,
		cancel:          cancel,
		ws:              ws,
		svc:             svc,
		gitSvc:          gitSvc,
		projectGitSvc:   projectGitSvc,
		queries:         queries,
		bus:             bus,
		teamSvc:         teamSvc,
		personaSvc:      personaSvc,
		browserSvc:      browserSvc,
		scheduleSvc:     scheduleSvc,
		catalog:         catalog,
		sendCh:          make(chan any, sendBufSize),
		dispatchCh:      make(chan ClientMessage, dispatchBufSize),
		readSlots:       make(chan struct{}, maxConcurrentReads),
		maxMessageBytes: maxMessageBytes,
	}
	c.sub = bus.SubscribeTopics(nil, &connSubscriber{c: c})
	// Join the empty/global topic: project-less events (web-only discussion
	// channels, whose channels.project_id is NULL → published on topic "") fan
	// out to every connected client, since such discussions belong to no project
	// the client subscribes to individually. Per-project topics are added on
	// demand via subscribeProject.
	c.sub.AddTopic("")
	return c
}

// subscribeProject joins projectID's topic so events Published to it are
// forwarded by connSubscriber. AddTopic is idempotent.
func (c *conn) subscribeProject(projectID string) {
	c.sub.AddTopic(projectID)
}

func (c *conn) unsubscribe() {
	if c.sub != nil {
		c.sub.Unsubscribe()
		c.sub = nil
	}
}

func (c *conn) run() {
	defer func() {
		c.unsubscribe()
		c.close()
	}()

	go c.writeLoop()
	go c.dispatchLoop()
	c.readLoop()
}

func (c *conn) close() {
	c.closeOnce.Do(func() {
		c.cancel()
		// nil only for test conns constructed without a socket.
		if c.ws != nil {
			_ = c.ws.Close()
		}
	})
}

func (c *conn) readLoop() {
	c.ws.SetReadLimit(c.maxMessageBytes)
	c.ws.SetReadDeadline(time.Now().Add(pongTimeout))
	c.ws.SetPongHandler(func(string) error {
		c.ws.SetReadDeadline(time.Now().Add(pongTimeout))
		return nil
	})
	for {
		var msg ClientMessage
		if err := c.ws.ReadJSON(&msg); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				slog.Warn("ws read error", "error", err)
			}
			return
		}
		lvl := slog.LevelDebug
		if isTraceType(msg.Type) {
			lvl = logging.LevelTrace
		}
		slog.Log(context.Background(), lvl, "ws recv", "type", msg.Type, "id", msg.ID)
		if !c.enqueueDispatch(msg) {
			return
		}
	}
}

// enqueueDispatch hands a message to the dispatch loop. The read loop must
// never run a handler itself: while one is in flight nothing reads the
// socket, so pongs sit unread and the read deadline (refreshed only inside
// ReadJSON) goes stale — a handler slower than pongTimeout made the server
// tear down its own healthy socket the moment the handler returned, and no
// other RPC on the socket (a stop, an approval answer) was even read while
// it ran.
//
// Reports false when the read loop should stop. A full queue closes the
// connection rather than blocking (which would recreate the stale-deadline
// fault) or dropping (an RPC that silently never answers wedges its caller)
// — the same contract send() applies to a client that cannot keep up.
func (c *conn) enqueueDispatch(msg ClientMessage) bool {
	select {
	case c.dispatchCh <- msg:
		return true
	case <-c.ctx.Done():
		return false
	default:
		slog.Warn("ws dispatch queue full, closing connection", "type", msg.Type, "id", msg.ID)
		c.close()
		return false
	}
}

// dispatchLoop executes handlers off the read loop, in arrival order.
//
// Two lanes. A **mutation** runs on this loop, serially, so what the client
// sent in order lands in order: a set-model before an enqueue, a subscribe
// before the list that expects its pushes. A **read** (concurrentOps) runs
// on its own goroutine, bounded by readSlots, because a read has no ordering
// contract with its neighbours — responses match by request id — and a slow
// one on the serial lane stalled every RPC behind it: a `project.fetch`
// waiting on the network held up a session history for 1.6s, and at boot
// the open session's history sat behind twenty other projects' lists. A
// mutation still waits for at most one read slot to free, never for a whole
// flood, and it never overtakes a read sent after it. Handlers known to block
// for tens of seconds (the msggen family) additionally leave this loop via
// handleRequestAsync.
func (c *conn) dispatchLoop() {
	for {
		select {
		case <-c.ctx.Done():
			return
		case msg := <-c.dispatchCh:
			if concurrentOps[msg.Type] {
				c.dispatchConcurrently(msg)
				continue
			}
			c.dispatch(msg)
		}
	}
}

// maxConcurrentReads bounds the read lane per connection. Enough that a
// boot's lists and histories overlap; few enough that thirty of them cannot
// fork git thirty ways at once.
const maxConcurrentReads = 8

// concurrentOps are the handlers that run on the read lane. Membership is a
// claim that the handler mutates nothing a later request could observe out
// of order; a handler that writes stays off this list, whatever it costs.
var concurrentOps = map[string]bool{
	"session.list":              true,
	"session.history":           true,
	"session.diff":              true,
	"session.commit-log":        true,
	"session.uncommitted-files": true,
	"session.uncommitted-diff":  true,
	"session.pr-status":         true,
	"project.git-status":        true,
	"project.fetch":             true,
	"project.list-branches":     true,
	"project.tracked-files":     true,
	"project.commands":          true,
	"project.uncommitted-files": true,
	"project.activity":          true,
	"wire.list":                 true,
	"channel.list":              true,
	"channel.info":              true,
	"channel.timeline":          true,
	"schedule.list":             true,
	"schedule.runs":             true,
	"team.list":                 true,
	"agent-profile.list":        true,
	"persona.list":              true,
	"providers.models":          true,
	"browser.status":            true,
}

// dispatchConcurrently runs a read-lane handler on its own goroutine once a
// slot is free. Waiting for the slot happens on the dispatch loop, which is
// the backpressure: the loop, not the goroutine count, absorbs a flood. A
// conn built without slots (a bare test) runs the handler inline.
func (c *conn) dispatchConcurrently(msg ClientMessage) {
	if c.readSlots == nil {
		c.dispatch(msg)
		return
	}
	select {
	case c.readSlots <- struct{}{}:
	case <-c.ctx.Done():
		return
	}
	go func() {
		defer func() { <-c.readSlots }()
		// The same per-request panic guard handleRequestAsync carries: a
		// panic on a bare goroutine kills the process.
		defer func() {
			if r := recover(); r != nil {
				slog.Error("ws handler panic", "type", msg.Type, "requestID", msg.ID, "panic", r)
				c.respond(msg.ID, nil, "internal error")
			}
		}()
		c.dispatch(msg)
	}()
}

// writeLoop owns all socket writes. Exiting on a write error must go through
// c.close(): a bare return leaves the read loop blocked in ReadJSON on a conn
// nobody will write to again — a zombie that keeps reading and executing RPCs
// whose responses are dropped, until the read deadline expires up to
// pongTimeout later. The deferred close also covers the ctx.Done exit, where
// it is an idempotent no-op (whatever cancelled the ctx already closed).
func (c *conn) writeLoop() {
	defer c.close()
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			c.drainSendBuffer()
			return
		case msg := <-c.sendCh:
			c.mu.Lock()
			c.ws.SetWriteDeadline(time.Now().Add(writeTimeout))
			err := c.ws.WriteJSON(msg)
			c.mu.Unlock()
			if err != nil {
				slog.Warn("ws write error", "error", err)
				return
			}
		case <-ticker.C:
			c.mu.Lock()
			c.ws.SetWriteDeadline(time.Now().Add(writeTimeout))
			err := c.ws.WriteMessage(websocket.PingMessage, nil)
			c.mu.Unlock()
			if err != nil {
				return
			}
		}
	}
}

// handlerFunc is the signature every dispatch handler conforms to.
type handlerFunc func(*conn, ClientMessage)

// handlerRegistry maps incoming message types to their handler. Adding a new
// message type means adding one entry here and implementing the corresponding
// method on *conn — no need to edit a switch.
var handlerRegistry = map[string]handlerFunc{
	// project.*
	"project.subscribe":               (*conn).handleProjectSubscribe,
	"project.git-status":              (*conn).handleProjectGitStatus,
	"project.fetch":                   (*conn).handleProjectFetch,
	"project.push":                    (*conn).handleProjectPush,
	"project.commit":                  (*conn).handleProjectCommit,
	"project.list-branches":           (*conn).handleProjectListBranches,
	"project.checkout":                (*conn).handleProjectCheckout,
	"project.pull":                    (*conn).handleProjectPull,
	"project.tracked-files":           (*conn).handleProjectTrackedFiles,
	"project.commands":                (*conn).handleProjectCommands,
	"project.uncommitted-files":       (*conn).handleProjectUncommittedFiles,
	"project.discard":                 (*conn).handleProjectDiscard,
	"project.generate-commit-message": (*conn).handleProjectGenerateCommitMsg,
	"project.reorder":                 (*conn).handleProjectReorder,
	"project.set-favorite":            (*conn).handleProjectSetFavorite,
	"project.set-pinned":              (*conn).handleProjectSetPinned,
	"project.activity":                (*conn).handleProjectActivity,

	// wire.* — global (cross-project) activity feed.
	"wire.list": (*conn).handleWireList,

	// session.*
	"session.create":                  (*conn).handleSessionCreate,
	"session.query":                   (*conn).handleSessionQuery,
	"session.list":                    (*conn).handleSessionList,
	"session.stop":                    (*conn).handleSessionStop,
	"session.resume":                  (*conn).handleSessionResume,
	"session.reset-conversation":      (*conn).handleSessionResetConversation,
	"session.history":                 (*conn).handleSessionHistory,
	"session.diff":                    (*conn).handleSessionDiff,
	"session.interrupt":               (*conn).handleSessionInterrupt,
	"session.attention":               (*conn).handleSessionAttention,
	"session.markSeen":                (*conn).handleSessionMarkSeen,
	"session.merge":                   (*conn).handleSessionMerge,
	"session.create-pr":               (*conn).handleSessionCreatePR,
	"session.commit":                  (*conn).handleSessionCommit,
	"session.rename":                  (*conn).handleSessionRename,
	"session.set-pinned":              (*conn).handleSessionSetPinned,
	"session.delete":                  (*conn).handleSessionDelete,
	"session.delete-bulk":             (*conn).handleSessionDeleteBulk,
	"session.set-model":               (*conn).handleSessionSetModel,
	"session.set-permission":          (*conn).handleSessionSetPermission,
	"session.set-auto-approve":        (*conn).handleSessionSetAutoApprove,
	"session.resolve-approval":        (*conn).handleSessionResolveApproval,
	"session.resolve-question":        (*conn).handleSessionResolveQuestion,
	"session.dismiss-question":        (*conn).handleSessionDismissQuestion,
	"session.rebase":                  (*conn).handleSessionRebase,
	"session.generate-pr-description": (*conn).handleSessionGeneratePRDesc,
	"session.archive":                 (*conn).handleSessionArchive,
	"session.unarchive":               (*conn).handleSessionUnarchive,
	// Deprecated aliases. A paired machine can be running an older binary, and
	// sidebar actions on a remote session travel over that machine's socket —
	// so the new client keeps speaking the old names to a peer that only knows
	// them. Remove once no supported release predates the rename.
	"session.mark-done":               (*conn).handleSessionArchive,
	"session.unmark-done":             (*conn).handleSessionUnarchive,
	"session.clean":                   (*conn).handleSessionClean,
	"session.refresh-git":             (*conn).handleSessionRefreshGit,
	"session.generate-commit-message": (*conn).handleSessionGenerateCommitMsg,
	"session.generate-name":           (*conn).handleSessionGenerateName,
	"session.commit-log":              (*conn).handleSessionCommitLog,
	"session.uncommitted-files":       (*conn).handleSessionUncommittedFiles,
	"session.uncommitted-diff":        (*conn).handleSessionUncommittedDiff,
	"session.discard-file":            (*conn).handleSessionDiscardFile,
	"session.pr-status":               (*conn).handleSessionPRStatus,
	"session.enqueue":                 (*conn).handleSessionEnqueue,

	// browser.*
	"browser.status":   (*conn).handleBrowserStatus,
	"browser.launch":   (*conn).handleBrowserLaunch,
	"browser.stop":     (*conn).handleBrowserStop,
	"browser.input":    (*conn).handleBrowserInput,
	"browser.navigate": (*conn).handleBrowserNavigate,

	// channel.*
	"channel.create":        (*conn).handleChannelCreate,
	"channel.delete":        (*conn).handleChannelDelete,
	"channel.dissolve":      (*conn).handleChannelDissolve,
	"channel.dissolve-keep": (*conn).handleChannelDissolveKeep,
	"channel.join":          (*conn).handleChannelJoin,
	"channel.leave":         (*conn).handleChannelLeave,
	"channel.list":          (*conn).handleChannelList,
	"channel.info":          (*conn).handleChannelInfo,
	"channel.timeline":      (*conn).handleChannelTimeline,
	"channel.send-message":  (*conn).handleChannelSendMessage,
	"channel.broadcast":     (*conn).handleChannelBroadcast,
	"channel.create-swarm":  (*conn).handleChannelCreateSwarm,

	// discussion.*
	"discussion.start": (*conn).handleDiscussionStart,
	"discussion.round": (*conn).handleDiscussionRound,
	"discussion.stop":  (*conn).handleDiscussionStop,

	// agent-profile.* / team.* / persona.*
	"agent-profile.list":     (*conn).handleAgentProfileList,
	"agent-profile.create":   (*conn).handleAgentProfileCreate,
	"agent-profile.update":   (*conn).handleAgentProfileUpdate,
	"agent-profile.delete":   (*conn).handleAgentProfileDelete,
	"agent-profile.generate": (*conn).handleProfileGenerate,
	"team.list":              (*conn).handleTeamList,
	"team.create":            (*conn).handleTeamCreate,
	"team.update":            (*conn).handleTeamUpdate,
	"team.delete":            (*conn).handleTeamDelete,
	"team.add-member":        (*conn).handleTeamAddMember,
	"team.remove-member":     (*conn).handleTeamRemoveMember,
	"persona.query":          (*conn).handlePersonaQuery,
	"persona.list":           (*conn).handlePersonaList,

	// providers.*
	"providers.models": (*conn).handleProvidersModels,

	// schedule.* — scheduled loops (docs/scheduled-loops.md)
	"schedule.create":      (*conn).handleScheduleCreate,
	"schedule.list":        (*conn).handleScheduleList,
	"schedule.update":      (*conn).handleScheduleUpdate,
	"schedule.delete":      (*conn).handleScheduleDelete,
	"schedule.pause":       (*conn).handleSchedulePause,
	"schedule.resume":      (*conn).handleScheduleResume,
	"schedule.approve":     (*conn).handleScheduleApprove,
	"schedule.run-now":     (*conn).handleScheduleRunNow,
	"schedule.runs":        (*conn).handleScheduleRuns,
	"schedule.mark-viewed": (*conn).handleScheduleMarkViewed,

	// ping
	"ping": (*conn).handlePing,
}

func (c *conn) dispatch(msg ClientMessage) {
	if h, ok := handlerRegistry[msg.Type]; ok {
		h(c, msg)
		return
	}
	slog.Warn("ws unknown message type", "type", msg.Type, "id", msg.ID)
	c.respond(msg.ID, nil, "unknown message type: "+msg.Type)
}

func (c *conn) handlePing(msg ClientMessage) {
	c.respond(msg.ID, map[string]string{"status": "ok"}, "")
}

var traceTypes = map[string]bool{
	"project.git-status": true,
	"ping":               true,
}

func isTraceType(t string) bool {
	return traceTypes[t]
}

// drainSendBuffer writes any remaining queued messages before the connection closes.
func (c *conn) drainSendBuffer() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ws.SetWriteDeadline(time.Now().Add(1 * time.Second))
	for {
		select {
		case msg := <-c.sendCh:
			_ = c.ws.WriteJSON(msg)
		default:
			return
		}
	}
}

// send enqueues a message for writing. Non-blocking: if the buffer is full,
// the connection is closed (the client can't keep up). Closed via c.close(),
// not a bare cancel — the read loop never observes the ctx (it is blocked in
// ReadJSON), so only closing the socket actually stops it; cancel alone left
// a zombie conn reading and executing RPCs whose responses were dropped for
// up to pongTimeout.
func (c *conn) send(msg any) {
	select {
	case c.sendCh <- msg:
	case <-c.ctx.Done():
	default:
		slog.Warn("ws send buffer full, closing connection")
		c.close()
	}
}

func (c *conn) respond(id string, payload any, errMsg string) {
	resp := ServerResponse{
		ID:   id,
		Type: "response",
	}
	if errMsg != "" {
		resp.Error = &ErrorBody{Message: errMsg}
	} else {
		resp.Payload = payload
	}
	c.send(resp)
}

func (c *conn) push(pushType string, payload any) {
	c.send(ServerPush{Type: pushType, Payload: payload})
}
