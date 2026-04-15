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

func (c *conn) dispatch(msg ClientMessage) {
	switch msg.Type {
	case "project.subscribe":
		c.handleProjectSubscribe(msg)
	case "session.create":
		c.handleSessionCreate(msg)
	case "session.query":
		c.handleSessionQuery(msg)
	case "session.list":
		c.handleSessionList(msg)
	case "session.stop":
		c.handleSessionStop(msg)
	case "session.resume":
		c.handleSessionResume(msg)
	case "session.reset-conversation":
		c.handleSessionResetConversation(msg)
	case "session.history":
		c.handleSessionHistory(msg)
	case "session.diff":
		c.handleSessionDiff(msg)
	case "session.interrupt":
		c.handleSessionInterrupt(msg)
	case "session.merge":
		c.handleSessionMerge(msg)
	case "session.create-pr":
		c.handleSessionCreatePR(msg)
	case "session.commit":
		c.handleSessionCommit(msg)
	case "session.rename":
		c.handleSessionRename(msg)
	case "session.delete":
		c.handleSessionDelete(msg)
	case "session.delete-bulk":
		c.handleSessionDeleteBulk(msg)
	case "session.set-model":
		c.handleSessionSetModel(msg)
	case "session.set-permission":
		c.handleSessionSetPermission(msg)
	case "session.set-auto-approve":
		c.handleSessionSetAutoApprove(msg)
	case "session.resolve-approval":
		c.handleSessionResolveApproval(msg)
	case "session.resolve-question":
		c.handleSessionResolveQuestion(msg)
	case "session.rebase":
		c.handleSessionRebase(msg)
	case "session.generate-pr-description":
		c.handleSessionGeneratePRDesc(msg)
	case "session.mark-done":
		c.handleSessionMarkDone(msg)
	case "session.clean":
		c.handleSessionClean(msg)
	case "session.refresh-git":
		c.handleSessionRefreshGit(msg)
	case "session.generate-commit-message":
		c.handleSessionGenerateCommitMsg(msg)
	case "session.commit-log":
		c.handleSessionCommitLog(msg)
	case "session.uncommitted-files":
		c.handleSessionUncommittedFiles(msg)
	case "session.uncommitted-diff":
		c.handleSessionUncommittedDiff(msg)
	case "session.pr-status":
		c.handleSessionPRStatus(msg)
	case "session.enqueue":
		c.handleSessionEnqueue(msg)
	case "project.git-status":
		c.handleProjectGitStatus(msg)
	case "project.fetch":
		c.handleProjectFetch(msg)
	case "project.push":
		c.handleProjectPush(msg)
	case "project.commit":
		c.handleProjectCommit(msg)
	case "project.list-branches":
		c.handleProjectListBranches(msg)
	case "project.checkout":
		c.handleProjectCheckout(msg)
	case "project.pull":
		c.handleProjectPull(msg)
	case "project.tracked-files":
		c.handleProjectTrackedFiles(msg)
	case "project.commands":
		c.handleProjectCommands(msg)
	case "project.uncommitted-files":
		c.handleProjectUncommittedFiles(msg)
	case "project.discard":
		c.handleProjectDiscard(msg)
	case "project.generate-commit-message":
		c.handleProjectGenerateCommitMsg(msg)
	case "project.reorder":
		c.handleProjectReorder(msg)
	case "project.set-favorite":
		c.handleProjectSetFavorite(msg)
	case "browser.status":
		c.handleBrowserStatus(msg)
	case "browser.launch":
		c.handleBrowserLaunch(msg)
	case "browser.stop":
		c.handleBrowserStop(msg)
	case "browser.input":
		c.handleBrowserInput(msg)
	case "browser.navigate":
		c.handleBrowserNavigate(msg)
	case "channel.create":
		c.handleChannelCreate(msg)
	case "channel.delete":
		c.handleChannelDelete(msg)
	case "channel.dissolve":
		c.handleChannelDissolve(msg)
	case "channel.dissolve-keep":
		c.handleChannelDissolveKeep(msg)
	case "channel.join":
		c.handleChannelJoin(msg)
	case "channel.leave":
		c.handleChannelLeave(msg)
	case "channel.list":
		c.handleChannelList(msg)
	case "channel.info":
		c.handleChannelInfo(msg)
	case "channel.timeline":
		c.handleChannelTimeline(msg)
	case "channel.send-message":
		c.handleChannelSendMessage(msg)
	case "channel.broadcast":
		c.handleChannelBroadcast(msg)
	case "channel.create-swarm":
		c.handleChannelCreateSwarm(msg)
	case "agent-profile.list":
		c.handleAgentProfileList(msg)
	case "agent-profile.create":
		c.handleAgentProfileCreate(msg)
	case "agent-profile.update":
		c.handleAgentProfileUpdate(msg)
	case "agent-profile.delete":
		c.handleAgentProfileDelete(msg)
	case "team.list":
		c.handleTeamList(msg)
	case "team.create":
		c.handleTeamCreate(msg)
	case "team.update":
		c.handleTeamUpdate(msg)
	case "team.delete":
		c.handleTeamDelete(msg)
	case "team.add-member":
		c.handleTeamAddMember(msg)
	case "team.remove-member":
		c.handleTeamRemoveMember(msg)
	case "persona.query":
		c.handlePersonaQuery(msg)
	case "persona.list":
		c.handlePersonaList(msg)
	case "agent-profile.generate":
		c.handleProfileGenerate(msg)
	case "ping":
		c.respond(msg.ID, map[string]string{"status": "ok"}, "")
	default:
		slog.Warn("ws unknown message type", "type", msg.Type, "id", msg.ID)
		c.respond(msg.ID, nil, "unknown message type: "+msg.Type)
	}
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
