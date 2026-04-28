package ws

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/mdjarv/agentique/backend/internal/logging"
	"github.com/mdjarv/agentique/backend/internal/persona"
	"github.com/mdjarv/agentique/backend/internal/project"
	"github.com/mdjarv/agentique/backend/internal/session"
	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/mdjarv/agentique/backend/internal/team"
	"github.com/gorilla/websocket"
)

const (
	writeTimeout = 10 * time.Second
	pongTimeout  = 60 * time.Second
	pingInterval = 30 * time.Second
	sendBufSize  = 256
)

type conn struct {
	ctx           context.Context
	cancel        context.CancelFunc
	ws            *websocket.Conn
	svc           *session.Service
	gitSvc        *session.GitService
	projectGitSvc *project.GitService
	queries       *store.Queries
	hub           *Hub
	teamSvc       *team.Service           // nil when experimental teams is disabled
	personaSvc    *persona.Service         // nil when experimental teams is disabled
	browserSvc    *session.BrowserService  // nil when browser support is unavailable
	sendCh        chan any
	mu            sync.Mutex
}

func newConn(parentCtx context.Context, ws *websocket.Conn, svc *session.Service, gitSvc *session.GitService, projectGitSvc *project.GitService, queries *store.Queries, hub *Hub, teamSvc *team.Service, personaSvc *persona.Service, browserSvc *session.BrowserService) *conn {
	ctx, cancel := context.WithCancel(parentCtx)
	return &conn{
		ctx:           ctx,
		cancel:        cancel,
		ws:            ws,
		svc:           svc,
		gitSvc:        gitSvc,
		projectGitSvc: projectGitSvc,
		queries:       queries,
		hub:           hub,
		teamSvc:       teamSvc,
		personaSvc:    personaSvc,
		browserSvc:    browserSvc,
		sendCh:        make(chan any, sendBufSize),
	}
}

func (c *conn) run() {
	defer func() {
		c.hub.Unsubscribe(c)
		c.cancel()
		c.ws.Close()
	}()

	go c.writeLoop()
	c.readLoop()
}

func (c *conn) readLoop() {
	c.ws.SetReadLimit(128 << 20) // 128 MB – must accommodate base64-encoded image attachments
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
		c.dispatch(msg)
	}
}

func (c *conn) writeLoop() {
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
	"project.activity":                (*conn).handleProjectActivity,

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
	"session.merge":                   (*conn).handleSessionMerge,
	"session.create-pr":               (*conn).handleSessionCreatePR,
	"session.commit":                  (*conn).handleSessionCommit,
	"session.rename":                  (*conn).handleSessionRename,
	"session.delete":                  (*conn).handleSessionDelete,
	"session.delete-bulk":             (*conn).handleSessionDeleteBulk,
	"session.set-model":               (*conn).handleSessionSetModel,
	"session.set-permission":          (*conn).handleSessionSetPermission,
	"session.set-auto-approve":        (*conn).handleSessionSetAutoApprove,
	"session.resolve-approval":        (*conn).handleSessionResolveApproval,
	"session.resolve-question":        (*conn).handleSessionResolveQuestion,
	"session.rebase":                  (*conn).handleSessionRebase,
	"session.generate-pr-description": (*conn).handleSessionGeneratePRDesc,
	"session.mark-done":               (*conn).handleSessionMarkDone,
	"session.clean":                   (*conn).handleSessionClean,
	"session.refresh-git":             (*conn).handleSessionRefreshGit,
	"session.generate-commit-message": (*conn).handleSessionGenerateCommitMsg,
	"session.generate-name":           (*conn).handleSessionGenerateName,
	"session.commit-log":              (*conn).handleSessionCommitLog,
	"session.uncommitted-files":       (*conn).handleSessionUncommittedFiles,
	"session.uncommitted-diff":        (*conn).handleSessionUncommittedDiff,
	"session.pr-status":               (*conn).handleSessionPRStatus,
	"session.enqueue":                 (*conn).handleSessionEnqueue,

	// browser.*
	"browser.status":   (*conn).handleBrowserStatus,
	"browser.launch":   (*conn).handleBrowserLaunch,
	"browser.stop":     (*conn).handleBrowserStop,
	"browser.input":    (*conn).handleBrowserInput,
	"browser.navigate": (*conn).handleBrowserNavigate,

	// channel.*
	"channel.create":         (*conn).handleChannelCreate,
	"channel.delete":         (*conn).handleChannelDelete,
	"channel.dissolve":       (*conn).handleChannelDissolve,
	"channel.dissolve-keep":  (*conn).handleChannelDissolveKeep,
	"channel.join":           (*conn).handleChannelJoin,
	"channel.leave":          (*conn).handleChannelLeave,
	"channel.list":           (*conn).handleChannelList,
	"channel.info":           (*conn).handleChannelInfo,
	"channel.timeline":       (*conn).handleChannelTimeline,
	"channel.send-message":   (*conn).handleChannelSendMessage,
	"channel.broadcast":      (*conn).handleChannelBroadcast,
	"channel.create-swarm":   (*conn).handleChannelCreateSwarm,

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
// the connection is closed (the client can't keep up).
func (c *conn) send(msg any) {
	select {
	case c.sendCh <- msg:
	case <-c.ctx.Done():
	default:
		slog.Warn("ws send buffer full, closing connection")
		c.cancel()
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
