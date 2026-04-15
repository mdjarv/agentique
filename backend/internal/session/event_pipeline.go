package session

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	claudecli "github.com/allbin/claudecli-go"
)

// EventSink bundles the two universal outputs of event processing.
type EventSink struct {
	Persist   func(turnIndex, seq int, wireType string, data []byte)
	Broadcast func(pushType string, payload any)
}

// PipelineConfig holds dependencies for constructing an EventPipeline.
type PipelineConfig struct {
	SessionID        string
	Model            string
	Sink             EventSink
	InitialTurnIndex int

	// Callbacks for side effects triggered by specific event types.
	// All are optional (nil-safe).
	OnClaudeSessionID func(id string)
	OnPlanTransition  func(mode string)
	OnExitPlanMode    func(input json.RawMessage)
	OnWriteToolResult func()
	OnTurnComplete    func()
	OnFatalError      func(err error)
	OnSendMessage     func(toolUseID, targetName, content, msgType string)
}

// EventPipeline processes raw CLI events through a linear sequence of stages:
// init capture, wire conversion, transient filtering, persistence, tool tracking,
// broadcasting, and state transitions.
//
// It owns turn/seq numbering and tool category tracking. The event loop goroutine
// and watchdog stay in Session — they are lifecycle concerns, not event processing.
type EventPipeline struct {
	sessionID string
	model     string
	sink      EventSink

	mu                sync.Mutex
	claudeSessionID   string
	turnIndex         int
	seqInTurn         int
	toolCategories    map[string]string
	pendingMessageIDs []string // FIFO queue of messageIds awaiting replay confirmation

	onClaudeSessionID func(string)
	onPlanTransition  func(string)
	onExitPlanMode    func(json.RawMessage)
	onWriteToolResult func()
	onTurnComplete    func()
	onFatalError      func(error)
	onSendMessage     func(string, string, string, string)
}

// NewEventPipeline creates an event pipeline. Does not start any goroutines.
func NewEventPipeline(cfg PipelineConfig) *EventPipeline {
	return &EventPipeline{
		sessionID:         cfg.SessionID,
		model:             cfg.Model,
		sink:              cfg.Sink,
		turnIndex:         cfg.InitialTurnIndex,
		toolCategories:    make(map[string]string),
		onClaudeSessionID: cfg.OnClaudeSessionID,
		onPlanTransition:  cfg.OnPlanTransition,
		onExitPlanMode:    cfg.OnExitPlanMode,
		onWriteToolResult: cfg.OnWriteToolResult,
		onTurnComplete:    cfg.OnTurnComplete,
		onFatalError:      cfg.OnFatalError,
		onSendMessage:     cfg.OnSendMessage,
	}
}

// ProcessEvent handles a single CLI event through the pipeline stages.
func (p *EventPipeline) ProcessEvent(event claudecli.Event) {
	// Stage 1: Init capture (early return).
	if p.handleInit(event) {
		return
	}

	// Stage 1.5: UnknownEvent — log and drop.
	if unk, ok := event.(*claudecli.UnknownEvent); ok {
		slog.Debug("unknown CLI event type", "session_id", p.sessionID, "type", unk.Type)
		return
	}

	// Log raw rate_limit events for investigation (utilization field presence).
	if rle, ok := event.(*claudecli.RateLimitEvent); ok {
		slog.Info("rate_limit_event raw",
			"session_id", p.sessionID,
			"status", rle.Status,
			"utilization", rle.Utilization,
			"resets_at", rle.ResetsAt,
			"type", rle.RateLimitType,
			"raw", rle.Raw,
		)
	}

	// UserEvent: may produce multiple wire events (tool results + agent result).
	// Handled separately because a single UserEvent can yield N wire events.
	if ue, ok := event.(*claudecli.UserEvent); ok {
		p.processUserEvent(ue)
		return
	}

	// Stage 2: Convert to wire format.
	wireEvent := ToWireEvent(event, p.model)
	if wireEvent == nil {
		return
	}

	p.emitWireEvent(wireEvent)

	// Stage 8: State transitions on result/fatal-error.
	p.handleTerminalEvents(event)
}

// emitWireEvent runs stages 3–7 for a single wire event: transient filtering,
// persistence, tool tracking, and broadcasting.
func (p *EventPipeline) emitWireEvent(wireEvent any) {
	// Stamp result events with the current time so the frontend can show
	// when a turn completed. The same timestamp flows to DB and broadcast.
	if re, ok := wireEvent.(WireResultEvent); ok && re.Timestamp == 0 {
		re.Timestamp = time.Now().UnixMilli()
		wireEvent = re
	}

	// Stage 3: Transient events — broadcast only, skip DB.
	if isTransient(wireEvent) {
		p.sink.Broadcast("session.event", PushSessionEvent{SessionID: p.sessionID, Event: wireEvent})
		return
	}

	// Stage 4: Persist to DB (with truncation for tool results).
	p.persistEvent(wireEvent)

	// Stage 5: Track tool categories + detect plan mode transitions.
	p.trackToolUse(wireEvent)

	// Stage 6: Detect write-tool results, trigger git refresh.
	p.trackToolResult(wireEvent)

	// Stage 7: Broadcast to all project clients.
	p.sink.Broadcast("session.event", PushSessionEvent{SessionID: p.sessionID, Event: wireEvent})
}

// PushPendingMessage enqueues a messageId for replay confirmation.
func (p *EventPipeline) PushPendingMessage(id string) {
	p.mu.Lock()
	p.pendingMessageIDs = append(p.pendingMessageIDs, id)
	p.mu.Unlock()
}

// handleReplayConfirmation pops the oldest pending messageId and broadcasts
// a transient delivery confirmation event.
func (p *EventPipeline) handleReplayConfirmation() {
	p.mu.Lock()
	if len(p.pendingMessageIDs) == 0 {
		p.mu.Unlock()
		slog.Debug("replay event with no pending message", "session_id", p.sessionID)
		return
	}
	msgID := p.pendingMessageIDs[0]
	p.pendingMessageIDs = p.pendingMessageIDs[1:]
	p.mu.Unlock()

	p.sink.Broadcast("session.event", PushSessionEvent{
		SessionID: p.sessionID,
		Event: WireMessageDeliveryEvent{
			Type:      "message_delivery",
			Status:    "delivered",
			MessageID: msgID,
		},
	})
}

// processUserEvent extracts wire events from a UserEvent: tool_result content
// blocks become WireToolResultEvent, and agent results become WireAgentResultEvent.
func (p *EventPipeline) processUserEvent(ue *claudecli.UserEvent) {
	if ue.IsReplay {
		p.handleReplayConfirmation()
		return
	}
	for _, c := range ue.Content {
		if c.Type == "tool_result" && c.ToolUseID != "" {
			p.emitWireEvent(WireToolResultEvent{
				Type:            "tool_result",
				ToolID:          c.ToolUseID,
				Content:         convertToolContent(c.Content),
				ParentToolUseID: ue.ParentToolUseID,
			})
		}
	}
	if ue.AgentResult != nil {
		p.emitWireEvent(WireAgentResultEvent{
			Type:              "agent_result",
			ParentToolUseID:   ue.ParentToolUseID,
			Status:            ue.AgentResult.Status,
			AgentID:           ue.AgentResult.AgentID,
			AgentType:         ue.AgentResult.AgentType,
			Content:           convertToolContent(ue.AgentResult.Content),
			TotalDurationMs:   ue.AgentResult.TotalDurationMs,
			TotalTokens:       ue.AgentResult.TotalTokens,
			TotalToolUseCount: ue.AgentResult.TotalToolUseCount,
		})
	}
}

// AdvanceTurn increments the turn index, resets the sequence counter,
// and returns the new turn index. Called by Session.Query().
func (p *EventPipeline) AdvanceTurn() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.turnIndex++
	p.seqInTurn = 0
	return p.turnIndex
}

// SetSeq sets the sequence counter. Called by Session.Query() after
// persisting the prompt at seq 0.
func (p *EventPipeline) SetSeq(seq int) {
	p.mu.Lock()
	p.seqInTurn = seq
	p.mu.Unlock()
}

// AllocSeq atomically allocates a (turnIndex, seq) pair for the current turn.
// Called by Session.SendMessage() for mid-turn user messages.
func (p *EventPipeline) AllocSeq() (turnIndex, seq int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	turnIndex = p.turnIndex
	seq = p.seqInTurn
	p.seqInTurn++
	return turnIndex, seq
}

// TurnIndex returns the current turn index.
func (p *EventPipeline) TurnIndex() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.turnIndex
}

// ClaudeSessionID returns the captured Claude CLI session ID.
func (p *EventPipeline) ClaudeSessionID() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.claudeSessionID
}

// SetClaudeSessionID sets the Claude CLI session ID directly.
// Used by Manager.Resume() to restore the ID from DB.
func (p *EventPipeline) SetClaudeSessionID(id string) {
	p.mu.Lock()
	p.claudeSessionID = id
	p.mu.Unlock()
}

// --- Internal stage methods ---

func (p *EventPipeline) handleInit(event claudecli.Event) bool {
	initEv, ok := event.(*claudecli.InitEvent)
	if !ok {
		return false
	}
	if len(initEv.MCPServers) > 0 {
		names := make([]string, len(initEv.MCPServers))
		for i, s := range initEv.MCPServers {
			names[i] = s.Name + "=" + s.Status
		}
		slog.Debug("mcp servers", "session_id", p.sessionID, "servers", names)
	}

	p.mu.Lock()
	if p.claudeSessionID == "" && initEv.SessionID != "" {
		p.claudeSessionID = initEv.SessionID
		p.mu.Unlock()
		if p.onClaudeSessionID != nil {
			p.onClaudeSessionID(initEv.SessionID)
		}
		slog.Debug("captured claude session ID", "session_id", p.sessionID, "claude_session_id", initEv.SessionID)
		return true
	}
	p.mu.Unlock()
	return true
}

func (p *EventPipeline) persistEvent(wireEvent any) {
	p.mu.Lock()
	seq := p.seqInTurn
	turnIdx := p.turnIndex
	p.seqInTurn++
	p.mu.Unlock()

	dbEvent := wireEvent
	if tr, ok := wireEvent.(WireToolResultEvent); ok {
		origLen := len(toolResultText(tr.Content))
		dbEvent = truncateToolResult(tr)
		if origLen > maxToolResultDBSize {
			slog.Warn("tool result truncated for DB storage",
				"session_id", p.sessionID,
				"tool_id", tr.ToolID,
				"original_bytes", origLen,
				"truncated_to", maxToolResultDBSize,
			)
		}
	}

	data, err := json.Marshal(dbEvent)
	if err != nil {
		slog.Error("marshal event failed", "session_id", p.sessionID, "error", err)
		return
	}

	typed, ok := wireEvent.(interface{ WireType() string })
	if !ok {
		slog.Warn("event missing WireType, skipping persistence", "session_id", p.sessionID)
		return
	}
	p.sink.Persist(turnIdx, seq, typed.WireType(), data)
}

func (p *EventPipeline) trackToolUse(wireEvent any) {
	tue, ok := wireEvent.(WireToolUseEvent)
	if !ok {
		return
	}
	p.mu.Lock()
	p.toolCategories[tue.ToolID] = tue.Category
	p.mu.Unlock()

	// Subagent tool uses don't affect parent session plan mode.
	if tue.ParentToolUseID != "" {
		return
	}

	switch tue.ToolName {
	case "EnterPlanMode":
		if p.onPlanTransition != nil {
			p.onPlanTransition("plan")
		}
	case "ExitPlanMode":
		if p.onExitPlanMode != nil {
			p.onExitPlanMode(tue.ToolInput)
		} else if p.onPlanTransition != nil {
			p.onPlanTransition("default")
		}
	case "SendMessage":
		if p.onSendMessage != nil {
			to, body, msgType, err := parseSendMessageInput(tue.ToolInput)
			if err != nil {
				slog.Warn("pipeline: SendMessage parse failed",
					"session_id", p.sessionID, "error", err)
			} else if to != "@spawn" && to != "@dissolve" {
				go p.onSendMessage(tue.ToolID, to, body, msgType)
			}
		}
	}
}

func (p *EventPipeline) trackToolResult(wireEvent any) {
	tr, ok := wireEvent.(WireToolResultEvent)
	if !ok {
		return
	}
	p.mu.Lock()
	cat := p.toolCategories[tr.ToolID]
	delete(p.toolCategories, tr.ToolID)
	p.mu.Unlock()

	if (cat == "command" || cat == "file_write") && p.onWriteToolResult != nil {
		p.onWriteToolResult()
	}
}

func (p *EventPipeline) handleTerminalEvents(event claudecli.Event) {
	if _, ok := event.(*claudecli.ResultEvent); ok {
		p.mu.Lock()
		p.toolCategories = make(map[string]string)
		p.mu.Unlock()
		if p.onTurnComplete != nil {
			p.onTurnComplete()
		}
	}

	if errEv, ok := event.(*claudecli.ErrorEvent); ok {
		lvl := slog.LevelWarn
		if errEv.Fatal {
			lvl = slog.LevelError
		}
		slog.Log(context.Background(), lvl, "claude API error",
			"session_id", p.sessionID,
			"fatal", errEv.Fatal,
			"error", errEv.Error(),
		)
		if errEv.Fatal && p.onFatalError != nil {
			p.onFatalError(errEv.Err)
		}
	}
}

// --- Pure helpers ---

// isTransient returns true for event types that are broadcast-only (skip DB).
func isTransient(wireEvent any) bool {
	switch e := wireEvent.(type) {
	case WireRateLimitEvent, WireCompactStatusEvent,
		WireContextManagementEvent, WireStreamEvent,
		WireMessageDeliveryEvent:
		return true
	case WireTaskEvent:
		return e.Subtype == "task_progress"
	}
	return false
}

// truncateToolResult returns a copy with large text blocks truncated for DB storage.
func truncateToolResult(tr WireToolResultEvent) WireToolResultEvent {
	text := toolResultText(tr.Content)
	if len(text) <= maxToolResultDBSize {
		return tr
	}
	truncated := text[:toolResultKeepHead] + "\n...[truncated]...\n" + text[len(text)-toolResultKeepTail:]
	blocks := make([]WireContentBlock, 0, len(tr.Content))
	replaced := false
	for _, b := range tr.Content {
		if b.Type == "text" && !replaced {
			blocks = append(blocks, WireContentBlock{Type: "text", Text: truncated})
			replaced = true
		} else if b.Type != "text" {
			blocks = append(blocks, b)
		}
	}
	tr.Content = blocks
	return tr
}
