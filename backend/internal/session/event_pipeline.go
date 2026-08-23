package session

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/allbin/agentkit/runtime"
	claudecli "github.com/allbin/claudecli-go"
)

// pipelineEpochCounter mints a process-global, collision-free epoch for each
// EventPipeline. A counter (not a timestamp) is used deliberately: two
// evict+resume cycles within the same millisecond would mint a duplicate
// timestamp epoch, so the frontend would miss the pipeline change and keep
// dropping the resumed stream's low sequence numbers as stale — a silent
// freeze. A monotonic counter can never collide.
var pipelineEpochCounter atomic.Int64

func nextPipelineEpoch() int64 { return pipelineEpochCounter.Add(1) }

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
	// OnResolvedModel fires once per pipeline with the concrete model ID the
	// provider reported for the configured slug (e.g. "opus" → "claude-opus-5").
	// It is what keeps the model catalog current without a new release.
	OnResolvedModel   func(id string)
	OnPlanTransition  func(mode string)
	OnExitPlanMode    func(input json.RawMessage)
	OnWriteToolResult func()
	// OnTurnComplete fires once per completed turn with the full outcome —
	// turn identity, status, final text (provider-independent, see
	// TurnOutcome.FinalText), and the classified error kind. Dispatched from
	// the event-loop goroutine; implementations must not block.
	OnTurnComplete  func(TurnOutcome)
	OnFatalError    func(err error)
	OnSendMessage   func(toolUseID, targetName, content, msgType string)
	OnActivityEvent func(wireEvent any) // called for result/error events (activity feed)
	// OnContextStale fires when the per-turn context-window number stops
	// describing the session: a turn completed, or the provider compacted the
	// transcript. Dispatched from the event-loop goroutine, so it must not
	// block — contextMeter.Refresh is the intended implementation.
	OnContextStale func()
}

// pulseState holds in-memory activity counters for the session pulse broadcast.
// Protected by EventPipeline.mu.
type pulseState struct {
	lastToolCategory string
	lastFilePath     string
	toolCallCount    int
	commitCount      int
	errorCount       int
	turnStartedAt    int64 // epoch ms
	dirty            bool  // true when state changed since last broadcast
	todoTotal        int   // total unique tasks tracked this session
	todoCompleted    int   // tasks with completed status
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

	// epoch is fixed for this pipeline's lifetime; wireSeq is the per-session
	// monotonic wire-sequence counter (1-based) stamped on every broadcast.
	// Both feed the frontend's gap/dedup/resync logic. Independent of the DB
	// (turnIndex, seqInTurn) used for persistence ordering.
	epoch   int64
	wireSeq atomic.Int64

	// emitMu serializes wire-seq allocation with the immediately-following
	// publish so broadcast order always matches wire-seq order — even when
	// session.events originate on different goroutines (the event loop,
	// Session.SendMessage/QueuePendingMessage, the async channel-message
	// routing spawned from trackToolUse). Without it, a goroutine could
	// allocate seq N and publish it after the loop has already published N+1,
	// which the frontend reads as a gap (spurious RESYNC) or a stale drop (a
	// non-persisted transient like the codex "queued" echo lost permanently).
	emitMu sync.Mutex

	mu              sync.Mutex
	claudeSessionID string
	resolvedModel   string // upstream-reported model ID from the first InitEvent (e.g. "claude-opus-4-8")
	// cliCapabilities are the optional protocol features the provider CLI
	// advertised in its init event (e.g. "interrupt_cancel_queued_v1"). The
	// init event is the only place they are reported, so latch them here.
	// Gate optional behavior on HasCLICapability — never on a version string.
	cliCapabilities   []string
	turnIndex         int
	seqInTurn         int
	toolCategories    map[string]string
	pendingMessageIDs []string // FIFO queue of messageIds awaiting replay confirmation
	pulse             pulseState
	pulseTimer        *time.Timer     // debounce timer for pulse broadcast
	pulseStopped      bool            // set on close; blocks re-arming the timer
	taskStatus        map[string]bool // taskID → completed; survives turn boundaries

	// Per-turn outcome accumulation (guarded by mu, reset in AdvanceTurn):
	// the last top-level assistant text (codex leaves TurnCompletedEvent.Text
	// empty — this is its fallback), the strongest classified error kind, and
	// the reset time of a rejected rate-limit event observed during the turn.
	turnFinalText     string
	turnErrorKind     string
	turnRateLimitedAt int64

	// turnOpen tracks a started turn whose completion has not yet been
	// processed; turnClosed is closed when it drains. The runtime flips Idle
	// and fires the state hook BEFORE the pipeline sees the
	// TurnCompletedEvent (agentkit's finishTurn broadcasts the StateChange
	// from inside the completion's dispatch), so an idle-boundary caller can
	// try to start a turn while the previous outcome is unprocessed —
	// advancing turnIndex under it and misattributing the outcome. Turn
	// starts wait on WaitTurnClosed. Guarded by mu.
	turnOpen   bool
	turnClosed chan struct{}

	onClaudeSessionID func(string)
	onResolvedModel   func(string)
	onPlanTransition  func(string)
	onExitPlanMode    func(json.RawMessage)
	onWriteToolResult func()
	onTurnComplete    func(TurnOutcome)
	onFatalError      func(error)
	onSendMessage     func(string, string, string, string)
	onActivityEvent   func(any)
	onContextStale    func()
}

// NewEventPipeline creates an event pipeline. Does not start any goroutines.
func NewEventPipeline(cfg PipelineConfig) *EventPipeline {
	return &EventPipeline{
		sessionID:         cfg.SessionID,
		model:             cfg.Model,
		sink:              cfg.Sink,
		epoch:             nextPipelineEpoch(),
		turnIndex:         cfg.InitialTurnIndex,
		toolCategories:    make(map[string]string),
		taskStatus:        make(map[string]bool),
		onClaudeSessionID: cfg.OnClaudeSessionID,
		onResolvedModel:   cfg.OnResolvedModel,
		onPlanTransition:  cfg.OnPlanTransition,
		onExitPlanMode:    cfg.OnExitPlanMode,
		onWriteToolResult: cfg.OnWriteToolResult,
		onTurnComplete:    cfg.OnTurnComplete,
		onFatalError:      cfg.OnFatalError,
		onSendMessage:     cfg.OnSendMessage,
		onActivityEvent:   cfg.OnActivityEvent,
		onContextStale:    cfg.OnContextStale,
	}
}

// ProcessEvent handles a single CLI event through the pipeline stages.
func (p *EventPipeline) ProcessEvent(event runtime.CLIEvent) {
	// Stage 1: Init capture (early return).
	if p.handleInit(event) {
		return
	}

	// Stage 1.5: UnknownProviderEvent — log and drop.
	if unk, ok := event.(runtime.UnknownProviderEvent); ok {
		slog.Debug("unknown provider event", "session_id", p.sessionID, "provider", unk.Provider, "type", unk.Type)
		return
	}

	// Accumulate the turn's last top-level assistant text as the
	// provider-independent FinalText fallback (subagent output — a non-empty
	// ParentToolUseID — is not the turn's answer).
	if at, ok := event.(runtime.AssistantTextEvent); ok && at.ParentToolUseID == "" && at.Content != "" {
		p.mu.Lock()
		p.turnFinalText = at.Content
		p.mu.Unlock()
	}

	// Log raw rate_limit events for investigation (utilization field presence).
	if rle, ok := event.(runtime.RateLimitEvent); ok {
		slog.Info("rate_limit_event raw",
			"session_id", p.sessionID,
			"status", rle.Status,
			"utilization", rle.Utilization,
			"resets_at", rle.ResetsAt,
			"type", rle.RateLimitType,
			"raw", rle.Raw,
		)
	}

	// UserEcho: may produce multiple wire events (tool results), and also
	// signals replay confirmation. Handled separately because a single
	// UserEcho can yield N wire events.
	if ue, ok := event.(runtime.UserEcho); ok {
		p.processUserEcho(ue)
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

	// Stage 9: a compaction rewrites the transcript out from under the
	// per-turn context number, which only ever describes the last API call —
	// remeasure so the meter drops instead of staying pinned near full.
	if _, ok := event.(runtime.CompactBoundaryEvent); ok && p.onContextStale != nil {
		p.onContextStale()
	}
}

// EmitSessionEvent stamps the wire event with this pipeline's epoch and the
// next monotonic wire-sequence number, then publishes it via the sink — with
// the allocation and the publish held atomically under emitMu so broadcast
// order always equals wire-seq order. This is the single funnel for ALL
// session.event emissions on a session (the event loop, Session-originated
// mid-turn/queued echoes, and channel message routing) so the frontend sees a
// gap-free per-session sequence regardless of the originating goroutine.
func (p *EventPipeline) EmitSessionEvent(wireEvent any) {
	p.emitMu.Lock()
	defer p.emitMu.Unlock()
	p.sink.Broadcast("session.event", PushSessionEvent{
		SessionID: p.sessionID,
		Event:     wireEvent,
		Seq:       p.wireSeq.Add(1),
		Epoch:     p.epoch,
	})
}

// broadcastSessionEvent is the pipeline-internal alias for EmitSessionEvent.
func (p *EventPipeline) broadcastSessionEvent(wireEvent any) {
	p.EmitSessionEvent(wireEvent)
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

	// Track task events for todo progress (before transient filtering so
	// task_progress events are counted even though they skip DB).
	p.trackTaskEvent(wireEvent)

	// Stage 3: Transient events — broadcast only, skip DB.
	if isTransient(wireEvent) {
		p.broadcastSessionEvent(wireEvent)
		return
	}

	// Stage 4: Persist to DB (with truncation for tool results).
	p.persistEvent(wireEvent)

	// Stage 5: Track tool categories + detect plan mode transitions.
	p.trackToolUse(wireEvent)

	// Stage 6: Detect write-tool results, trigger git refresh.
	p.trackToolResult(wireEvent)

	// Stage 7: Broadcast to all project clients.
	p.broadcastSessionEvent(wireEvent)

	// Stage 8: Activity feed — emit for result/error events.
	if p.onActivityEvent != nil {
		switch wireEvent.(type) {
		case WireResultEvent, WireErrorEvent:
			p.onActivityEvent(wireEvent)
		}
	}
}

// PushPendingMessage enqueues a messageId for replay confirmation.
func (p *EventPipeline) PushPendingMessage(id string) {
	p.mu.Lock()
	p.pendingMessageIDs = append(p.pendingMessageIDs, id)
	p.mu.Unlock()
}

// CancelPendingMessages drains the replay-confirmation queue and returns the
// messageIds that will now never be confirmed. Callers broadcast the
// cancellation themselves so the UI's pending bubbles resolve instead of
// hanging. Used by Session.Interrupt when a stop drops the provider's queue.
func (p *EventPipeline) CancelPendingMessages() []string {
	p.mu.Lock()
	ids := p.pendingMessageIDs
	p.pendingMessageIDs = nil
	p.mu.Unlock()
	return ids
}

// HasCLICapability reports whether the provider CLI advertised the named
// optional protocol feature in its init event. Empty capabilities mean the CLI
// advertised none — an older build — so this correctly answers false and the
// caller falls back rather than assuming support.
func (p *EventPipeline) HasCLICapability(name string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.cliCapabilities {
		if c == name {
			return true
		}
	}
	return false
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

	p.broadcastSessionEvent(WireMessageDeliveryEvent{
		Type:      "message_delivery",
		Status:    "delivered",
		MessageID: msgID,
	})
}

// processUserEcho extracts wire events from a UserEcho event. The replay
// confirmation form (keyed on the explicit IsReplay flag) signals that the
// CLI has accepted a previously-injected SendMessage; tool_result entries
// produce WireToolResultEvent. AgentResult metadata flows separately via
// runtime.AgentResultEvent → WireAgentResultEvent through the normal
// ToWireEvent path.
func (p *EventPipeline) processUserEcho(ue runtime.UserEcho) {
	if ue.IsReplay {
		p.handleReplayConfirmation()
		return
	}
	for _, tr := range ue.ToolResults {
		if tr.ToolUseID == "" {
			continue
		}
		p.emitWireEvent(WireToolResultEvent{
			Type:    "tool_result",
			ToolID:  tr.ToolUseID,
			Content: convertToolContent(tr.Content),
		})
	}
}

// AdvanceTurn increments the turn index, resets the sequence counter,
// initializes pulse state for the new turn, and returns the new turn index.
// Called by Session.Query().
func (p *EventPipeline) AdvanceTurn() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.turnIndex++
	p.seqInTurn = 0
	p.turnFinalText = ""
	p.turnErrorKind = ""
	p.turnRateLimitedAt = 0
	p.turnOpen = true
	p.turnClosed = make(chan struct{})
	// Preserve todo counts across turn boundaries — they track session-level task progress.
	p.pulse = pulseState{
		turnStartedAt: time.Now().UnixMilli(),
		todoTotal:     p.pulse.todoTotal,
		todoCompleted: p.pulse.todoCompleted,
	}
	return p.turnIndex
}

// classifyErrorKind maps a provider error to one of the ErrorKind* constants.
// Adapters do not populate ErrorEvent.Kind today (tracked as an upstream
// hand-off), so a non-empty Kind wins and message sniffing is the fallback.
func classifyErrorKind(errEv runtime.ErrorEvent) string {
	switch errEv.Kind {
	case "rate_limit":
		return ErrorKindRateLimit
	case "overloaded":
		return ErrorKindOverloaded
	case "context":
		return ErrorKindContext
	}
	msg := ""
	if errEv.Err != nil {
		msg = strings.ToLower(errEv.Err.Error())
	}
	switch {
	case strings.Contains(msg, "rate limit") || strings.Contains(msg, "rate_limit") ||
		strings.Contains(msg, "usage limit") || strings.Contains(msg, "429"):
		return ErrorKindRateLimit
	case strings.Contains(msg, "overloaded") || strings.Contains(msg, "529"):
		return ErrorKindOverloaded
	case strings.Contains(msg, "context window") || strings.Contains(msg, "context_window") ||
		strings.Contains(msg, "prompt is too long"):
		return ErrorKindContext
	default:
		return ErrorKindOther
	}
}

// strongerErrorKind keeps the more actionable of two classified kinds: a
// specific transient/context classification beats "other", which beats "".
func strongerErrorKind(current, next string) string {
	rank := func(k string) int {
		switch k {
		case ErrorKindRateLimit, ErrorKindOverloaded, ErrorKindContext:
			return 2
		case ErrorKindOther:
			return 1
		default:
			return 0
		}
	}
	if rank(next) > rank(current) {
		return next
	}
	return current
}

// SetSeq sets the sequence counter. Called by Session.Query() after
// persisting the prompt at seq 0.
func (p *EventPipeline) SetSeq(seq int) {
	p.mu.Lock()
	p.seqInTurn = seq
	p.mu.Unlock()
}

// TurnOpen reports whether a turn is in flight — started and not yet drained.
// This is the turn-lifecycle fact, the same one that gates outcome delivery;
// nothing here reads session state, which lags it (docs/upgrades.md: anything
// that restarts the server must consult the turn registry first).
func (p *EventPipeline) TurnOpen() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.turnOpen
}

// CloseTurn releases a turn that will never complete — a Query the runtime
// refused outright, or a session being torn down. Without it the turn stays
// "open" forever: the next turn start burns WaitTurnClosed's full timeout, and
// the drain gate reads the session as permanently busy.
func (p *EventPipeline) CloseTurn() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closeTurnLocked()
}

// closeTurnLocked marks the current turn's completion as processed and
// releases WaitTurnClosed waiters. Caller holds p.mu.
func (p *EventPipeline) closeTurnLocked() {
	if !p.turnOpen {
		return
	}
	p.turnOpen = false
	close(p.turnClosed)
}

// WaitTurnClosed blocks until the previous turn's completion has drained
// through the pipeline (or the timeout elapses; false on timeout). Turn
// starts call this so a fresh AdvanceTurn can never slip under an
// unprocessed TurnCompletedEvent and steal its outcome attribution — the
// runtime fires the Idle state hook before the completion event reaches the
// pipeline, so "state says idle" does not imply "outcome processed".
func (p *EventPipeline) WaitTurnClosed(timeout time.Duration) bool {
	p.mu.Lock()
	if !p.turnOpen {
		p.mu.Unlock()
		return true
	}
	ch := p.turnClosed
	p.mu.Unlock()
	select {
	case <-ch:
		return true
	case <-time.After(timeout):
		return false
	}
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

// Epoch returns this pipeline's lifetime identifier, stamped on every
// session.event so the frontend can detect a resume/rebuild (epoch change).
func (p *EventPipeline) Epoch() int64 { return p.epoch }

// CurrentWireSeq returns the highest wire-sequence number allocated so far
// (0 before the first event). Used as the high-water mark in history responses
// so a force-reload can reseed the frontend's last-seen seq.
func (p *EventPipeline) CurrentWireSeq() int64 { return p.wireSeq.Load() }

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

// ResolvedModel returns the upstream-reported model ID captured from the first
// InitEvent (e.g. "claude-opus-4-8"). Differs from the configured slug
// (e.g. "opus") when the provider resolves the alias to a specific version.
// Empty until the first InitEvent arrives.
func (p *EventPipeline) ResolvedModel() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.resolvedModel
}

// --- Internal stage methods ---

func (p *EventPipeline) handleInit(event runtime.CLIEvent) bool {
	initEv, ok := event.(runtime.SessionInitEvent)
	if !ok {
		return false
	}

	p.mu.Lock()
	captureSession := p.claudeSessionID == "" && initEv.SessionID != ""
	captureModel := p.resolvedModel == "" && initEv.Model != ""
	if captureSession {
		p.claudeSessionID = initEv.SessionID
	}
	if captureModel {
		p.resolvedModel = initEv.Model
	}
	// Always replace: a resume/reconnect can land on a different CLI build, and
	// the newest init event is the only truth about what it can do. An empty
	// list means "advertised nothing", not "supports nothing".
	p.cliCapabilities = initEv.Capabilities
	p.mu.Unlock()

	if captureSession {
		if p.onClaudeSessionID != nil {
			p.onClaudeSessionID(initEv.SessionID)
		}
		slog.Debug("captured provider session ID",
			"session_id", p.sessionID,
			"provider_session_id", initEv.SessionID,
		)
	}
	if captureModel {
		if p.onResolvedModel != nil {
			p.onResolvedModel(initEv.Model)
		}
		// ModelDisplayName fails safe — returns the input unchanged for
		// unrecognized IDs (e.g. codex's "gpt-5"), so calling it from this
		// provider-neutral layer is harmless.
		slog.Info("session model resolved",
			"session_id", p.sessionID,
			"model_id", initEv.Model,
			"model_display_name", claudecli.ModelDisplayName(initEv.Model),
		)
	}
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
	// Pulse: count tool calls, track last category and file path.
	if tue.ParentToolUseID == "" {
		p.pulse.toolCallCount++
		p.pulse.lastToolCategory = tue.Category
		if tue.Category == "file_write" {
			if fp := extractFilePath(tue.ToolInput); fp != "" {
				p.pulse.lastFilePath = fp
			}
		}
		p.pulse.dirty = true
	}
	p.mu.Unlock()
	p.schedulePulseBroadcast()

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
	case ChannelSendMessageTool, AgentiqueSendMessageTool:
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

	// Pulse: detect git commits from command tool results.
	if cat == "command" {
		text := toolResultText(tr.Content)
		if looksLikeCommit(text) {
			p.mu.Lock()
			p.pulse.commitCount++
			p.pulse.dirty = true
			p.mu.Unlock()
			p.schedulePulseBroadcast()
		}
	}
}

func (p *EventPipeline) trackTaskEvent(wireEvent any) {
	te, ok := wireEvent.(WireTaskEvent)
	if !ok {
		return
	}
	if te.TaskID == "" {
		return
	}

	p.mu.Lock()
	switch te.Subtype {
	case "task_started":
		if _, seen := p.taskStatus[te.TaskID]; !seen {
			p.taskStatus[te.TaskID] = false
		}
	case "task_progress", "task_notification":
		if te.Status == "completed" || te.Status == "done" {
			p.taskStatus[te.TaskID] = true
		}
	}

	// Recompute pulse todo counts.
	total, completed := 0, 0
	for _, done := range p.taskStatus {
		total++
		if done {
			completed++
		}
	}
	p.pulse.todoTotal = total
	p.pulse.todoCompleted = completed
	p.pulse.dirty = true
	p.mu.Unlock()
	p.schedulePulseBroadcast()
}

func (p *EventPipeline) handleTerminalEvents(event runtime.CLIEvent) {
	if tc, ok := event.(runtime.TurnCompletedEvent); ok {
		// A dynamic workflow emits a placeholder "running in the background"
		// turn-completion on launch; the real answer arrives in a later
		// (non-pending) completion. agentkit keeps the session in StateRunning
		// across it, but the event still flows here — skip all turn-end side
		// effects (pulse reset, turn-complete hook) so the workflow keeps running.
		if tc.WorkflowPending {
			return
		}
		p.mu.Lock()
		p.toolCategories = make(map[string]string)
		outcome := TurnOutcome{
			TurnIndex:         p.turnIndex,
			Status:            tc.Status,
			FinalText:         tc.Text,
			ErrorKind:         p.turnErrorKind,
			Duration:          tc.Duration,
			Usage:             tc.Usage,
			ContextWindow:     tc.ContextWindow,
			RateLimitResetsAt: p.turnRateLimitedAt,
		}
		if outcome.FinalText == "" {
			outcome.FinalText = p.turnFinalText
		}
		p.turnFinalText = ""
		p.turnErrorKind = ""
		p.turnRateLimitedAt = 0
		p.closeTurnLocked()
		p.mu.Unlock()
		// Broadcast final pulse before resetting, then clear.
		p.broadcastPulseNow()
		p.resetPulse()
		if p.onTurnComplete != nil {
			p.onTurnComplete(outcome)
		}
		// outcome.ContextWindow is the last API call's window, not the
		// session's — remeasure now that the turn is settled.
		if p.onContextStale != nil {
			p.onContextStale()
		}
	}

	if rle, ok := event.(runtime.RateLimitEvent); ok && rle.Status == "rejected" {
		p.mu.Lock()
		p.turnErrorKind = strongerErrorKind(p.turnErrorKind, ErrorKindRateLimit)
		if rle.ResetsAt > p.turnRateLimitedAt {
			p.turnRateLimitedAt = rle.ResetsAt
		}
		p.mu.Unlock()
	}

	if errEv, ok := event.(runtime.ErrorEvent); ok {
		p.mu.Lock()
		p.turnErrorKind = strongerErrorKind(p.turnErrorKind, classifyErrorKind(errEv))
		if errEv.Fatal {
			// A fatal error means no TurnCompletedEvent will ever arrive for
			// this turn — release waiters instead of forcing their timeout.
			p.closeTurnLocked()
		}
		p.mu.Unlock()
		lvl := slog.LevelWarn
		if errEv.Fatal {
			lvl = slog.LevelError
		}
		errMsg := ""
		if errEv.Err != nil {
			errMsg = errEv.Err.Error()
		}
		slog.Log(context.Background(), lvl, "provider API error",
			"session_id", p.sessionID,
			"fatal", errEv.Fatal,
			"kind", errEv.Kind,
			"error", errMsg,
		)
		// Pulse: count errors.
		p.mu.Lock()
		p.pulse.errorCount++
		p.pulse.dirty = true
		p.mu.Unlock()
		p.schedulePulseBroadcast()

		if errEv.Fatal && p.onFatalError != nil {
			p.onFatalError(errEv.Err)
		}
	}
}

// --- Pulse helpers ---

const pulseDebounce = 2 * time.Second

// schedulePulseBroadcast schedules a debounced pulse broadcast. Each call
// resets the timer; the broadcast fires once after pulseDebounce of quiet.
func (p *EventPipeline) schedulePulseBroadcast() {
	p.mu.Lock()
	defer p.mu.Unlock()
	// Don't re-arm after close: an in-flight ProcessEvent on the event-loop
	// goroutine can still reach here after StopPulseTimer, leaving a stray timer
	// that fires a pulse broadcast on a torn-down session.
	if p.pulseStopped {
		return
	}
	if p.pulseTimer != nil {
		p.pulseTimer.Stop()
	}
	p.pulseTimer = time.AfterFunc(pulseDebounce, func() {
		p.broadcastPulseNow()
	})
}

// broadcastPulseNow sends the current pulse state immediately if dirty.
func (p *EventPipeline) broadcastPulseNow() {
	p.mu.Lock()
	if !p.pulse.dirty {
		p.mu.Unlock()
		return
	}
	payload := PushSessionPulse{
		SessionID:        p.sessionID,
		LastToolCategory: p.pulse.lastToolCategory,
		LastFilePath:     p.pulse.lastFilePath,
		ToolCallCount:    p.pulse.toolCallCount,
		CommitCount:      p.pulse.commitCount,
		ErrorCount:       p.pulse.errorCount,
		TurnStartedAt:    p.pulse.turnStartedAt,
		TodoTotal:        p.pulse.todoTotal,
		TodoCompleted:    p.pulse.todoCompleted,
	}
	p.pulse.dirty = false
	if p.pulseTimer != nil {
		p.pulseTimer.Stop()
		p.pulseTimer = nil
	}
	p.mu.Unlock()
	p.sink.Broadcast("session.pulse", payload)
}

// resetPulse clears pulse state (called on turn completion).
func (p *EventPipeline) resetPulse() {
	p.mu.Lock()
	if p.pulseTimer != nil {
		p.pulseTimer.Stop()
		p.pulseTimer = nil
	}
	p.pulse = pulseState{}
	p.mu.Unlock()
}

// StopPulseTimer cancels any pending pulse broadcast and blocks future
// re-arming. Called on session close.
func (p *EventPipeline) StopPulseTimer() {
	p.mu.Lock()
	p.pulseStopped = true
	if p.pulseTimer != nil {
		p.pulseTimer.Stop()
		p.pulseTimer = nil
	}
	p.mu.Unlock()
}

// extractFilePath pulls the file_path from a tool_use input JSON.
// Returns "" if the field is absent or not a string.
func extractFilePath(input json.RawMessage) string {
	var obj struct {
		FilePath string `json:"file_path"`
	}
	if json.Unmarshal(input, &obj) == nil && obj.FilePath != "" {
		return obj.FilePath
	}
	return ""
}

// looksLikeCommit checks if a command output contains evidence of a git commit.
func looksLikeCommit(text string) bool {
	// git commit output: "[branch hash] message"
	// Look for the common pattern of a successful commit.
	return strings.Contains(text, "create mode") ||
		strings.Contains(text, "file changed") ||
		strings.Contains(text, "files changed") ||
		strings.Contains(text, "insertions(+)") ||
		strings.Contains(text, "deletions(-)")
}

// --- Pure helpers ---

// isTransient returns true for event types that are broadcast-only (skip DB).
func isTransient(wireEvent any) bool {
	switch e := wireEvent.(type) {
	case WireRateLimitEvent, WireCompactStatusEvent,
		WireContextManagementEvent, WireStreamEvent,
		WireMessageDeliveryEvent, WireContextUsageEvent,
		WireToolOutputDeltaEvent, WireReasoningDeltaEvent,
		WireToolProgressEvent:
		return true
	case WireTaskEvent:
		return e.Subtype == "task_progress"
	case WireResultEvent:
		// The workflow launch placeholder is broadcast-only: skip persistence
		// (no real result row) and the activity feed (no "turn completed" item).
		return e.WorkflowPending
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
