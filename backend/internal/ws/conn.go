package ws

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/allbin/agentique/backend/internal/project"
	"github.com/allbin/agentique/backend/internal/session"
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
	hub           *Hub
	sendCh        chan any
	mu            sync.Mutex
}

func newConn(parentCtx context.Context, ws *websocket.Conn, svc *session.Service, gitSvc *session.GitService, projectGitSvc *project.GitService, hub *Hub) *conn {
	ctx, cancel := context.WithCancel(parentCtx)
	return &conn{
		ctx:           ctx,
		cancel:        cancel,
		ws:            ws,
		svc:           svc,
		gitSvc:        gitSvc,
		projectGitSvc: projectGitSvc,
		hub:           hub,
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
		slog.Debug("ws recv", "type", msg.Type, "id", msg.ID)
		c.dispatch(msg)
	}
}

func (c *conn) writeLoop() {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			// Drain remaining messages before closing.
			c.mu.Lock()
			c.ws.SetWriteDeadline(time.Now().Add(1 * time.Second))
			for {
				select {
				case msg := <-c.sendCh:
					_ = c.ws.WriteJSON(msg)
				default:
					c.mu.Unlock()
					return
				}
			}
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
	case "session.uncommitted-files":
		c.handleSessionUncommittedFiles(msg)
	case "project.git-status":
		c.handleProjectGitStatus(msg)
	case "project.fetch":
		c.handleProjectFetch(msg)
	case "project.push":
		c.handleProjectPush(msg)
	case "project.commit":
		c.handleProjectCommit(msg)
	default:
		slog.Warn("ws unknown message type", "type", msg.Type, "id", msg.ID)
		c.respond(msg.ID, nil, "unknown message type: "+msg.Type)
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
