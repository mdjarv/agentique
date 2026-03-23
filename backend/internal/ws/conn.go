package ws

import (
	"context"
	"log"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/allbin/agentique/backend/internal/session"
)

type conn struct {
	ctx    context.Context
	cancel context.CancelFunc
	ws     *websocket.Conn
	svc    *session.Service
	hub    *Hub
	sendCh chan any
	mu     sync.Mutex
}

func newConn(parentCtx context.Context, ws *websocket.Conn, svc *session.Service, hub *Hub) *conn {
	ctx, cancel := context.WithCancel(parentCtx)
	return &conn{
		ctx:    ctx,
		cancel: cancel,
		ws:     ws,
		svc:    svc,
		hub:    hub,
		sendCh: make(chan any, 64),
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
	for {
		var msg ClientMessage
		if err := c.ws.ReadJSON(&msg); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("ws read error: %v", err)
			}
			return
		}
		c.dispatch(msg)
	}
}

func (c *conn) writeLoop() {
	for {
		select {
		case <-c.ctx.Done():
			return
		case msg := <-c.sendCh:
			c.mu.Lock()
			err := c.ws.WriteJSON(msg)
			c.mu.Unlock()
			if err != nil {
				log.Printf("ws write error: %v", err)
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
	default:
		c.respond(msg.ID, nil, "unknown message type: "+msg.Type)
	}
}

func (c *conn) send(msg any) {
	select {
	case c.sendCh <- msg:
	case <-c.ctx.Done():
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
