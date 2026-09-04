package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/allbin/agentkit/runtime"
)

// browserToolPrefix is the MCP tool-name prefix for the managed agent browser.
var browserToolPrefix = "mcp__" + MCPServerName + "__"

// isBrowserTool reports whether a tool name belongs to the agent browser MCP.
// This prefix check is the source of truth for the approval-pump path
// (handlePendingChange): it covers every browser tool in the default / acceptEdits
// / plan / auto modes regardless of @playwright/mcp's exact tool set.
func isBrowserTool(toolName string) bool {
	return strings.HasPrefix(toolName, browserToolPrefix)
}

// browserToolNames enumerates the @playwright/mcp tool set exposed under the
// agent browser MCP (server name MCPServerName), without the browserToolPrefix.
//
// It exists solely for the fullAuto fast-path. fullAuto maps to
// runtime.AutoApproveAll, which short-circuits the approval pump before
// PendingChangeEvent fires — so handlePendingChange's prefix-based lazy launch
// never runs in that mode. agentkit's interceptor lookup is keyed by exact tool
// name (no prefix matching), so we register a per-tool launch interceptor for
// each of these. Keep in sync with @playwright/mcp; because the prefix check in
// isBrowserTool still covers the four pump-driven modes, a missing entry here
// only degrades fullAuto (and only until the first listed tool brings Chrome up).
// See Session.interceptBrowserTool.
var browserToolNames = []string{
	"browser_click",
	"browser_close",
	"browser_console_messages",
	"browser_drag",
	"browser_drop",
	"browser_evaluate",
	"browser_file_upload",
	"browser_fill_form",
	"browser_handle_dialog",
	"browser_hover",
	"browser_navigate",
	"browser_navigate_back",
	"browser_network_request",
	"browser_network_requests",
	"browser_press_key",
	"browser_resize",
	"browser_run_code_unsafe",
	"browser_select_option",
	"browser_snapshot",
	"browser_tabs",
	"browser_take_screenshot",
	"browser_type",
	"browser_wait_for",
}

// makeBroadcastHook returns a runtime BroadcastFunc bound to the given
// agentique Session. The hook fans out events:
//
//   - CLIEvent      → pipeline.ProcessEvent (persistence + UI broadcast)
//   - StateChange   → DB persist + UI snapshot (never archivedAt: see below)
//   - WatchdogEvent → log + UI error broadcast (fatal kinds also surface a
//     state transition via the StateChange that follows)
//   - PendingChange → check shouldBypassPermission for "auto" mode and
//     auto-resolve, otherwise broadcast tool-permission to the UI.
//
// All hook handlers must be non-blocking; long work is offloaded to goroutines.
func makeBroadcastHook(s *Session) runtime.BroadcastFunc {
	return func(_ context.Context, e runtime.Event) {
		switch ev := e.(type) {
		case runtime.StateChangeEvent:
			handleRuntimeStateChange(s, ev)
		case runtime.WatchdogEvent:
			handleWatchdogEvent(s, ev)
		case runtime.PendingChangeEvent:
			go handlePendingChange(s, ev)
		default:
			if cli, ok := e.(runtime.CLIEvent); ok {
				safeProcessEvent(s, cli)
			}
		}
	}
}

// safeProcessEvent runs the pipeline with panic recovery so a malformed event
// can't kill the runtime event-loop goroutine.
func safeProcessEvent(s *Session, event runtime.CLIEvent) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic in pipeline.ProcessEvent", "session_id", s.ID, "panic", r)
			s.broadcastSessionEvent(WireErrorEvent{
				Type:    "error",
				Content: fmt.Sprintf("internal error processing event: %v", r),
				Fatal:   false,
			})
		}
	}()
	s.pipeline.ProcessEvent(event)
}

// handleRuntimeStateChange mirrors a runtime state transition into agentique:
// updates the in-memory state field, persists to DB, and broadcasts a snapshot.
// While a git operation is in progress, the merging dance owns the visible state
// — runtime transitions are dropped here.
//
// It touches state and nothing else. The runtime owns the process lifecycle;
// archivedAt belongs to the user.
func handleRuntimeStateChange(s *Session, ev runtime.StateChangeEvent) {
	target := mapRuntimeState(ev.To)
	s.mu.Lock()
	if s.state == StateMerging {
		// Merging dance owns visible state; ignore runtime transitions.
		s.mu.Unlock()
		return
	}
	// Preserve a fatal classification that pipeline.OnFatalError already
	// recorded. The runtime emits its own Done transition right after a
	// fatal CLI exit (handleEventChannelClose → setState(StateDone) when
	// the runtime side was idle), and that would otherwise clobber Failed
	// — losing the fatal signal in DB and UI.
	if s.state == StateFailed && ev.To == runtime.StateDone {
		s.mu.Unlock()
		return
	}
	s.state = target
	s.mu.Unlock()

	if target == StateStopped || target == StateDone || target == StateFailed {
		// Subagents are children of the CLI process: whatever ended it took
		// them too — stop, eviction, crash alike. Reset before the broadcast
		// below so the push announcing the stop is the one carrying the zero.
		s.resetAgentsInFlight()
	}

	s.persistState(target)
	if ev.To == runtime.StateDone {
		// Deliberately NOT archived here. A clean CLI exit is a process fact, not
		// user intent, and archivedAt is the sidebar's "the user filed this away"
		// bit — stamping it here let a subprocess exiting hide a session in the
		// collapsed Archived section with nobody asking. Same line
		// SetOnSessionFinished draws; the terminal state alone tells this story.

		// Learn-on-completion (M3): a clean CLI exit is a learning boundary, so fire the
		// per-session completion hook once, async, so fresh captures flow without the
		// session being deleted. Snapshot under lock then `go` (hook handlers must not
		// block the runtime broadcast loop). The two early returns above (StateMerging,
		// Failed→Done) intentionally skip this — the delete path nets those.
		s.mu.Lock()
		cb := s.onComplete
		s.mu.Unlock()
		if cb != nil {
			go cb()
		}
	}

	select {
	case s.stateChangedCh <- struct{}{}:
	default:
	}

	s.broadcastState(target)

	// Replay any messages buffered during the turn (providers without native
	// mid-turn injection). No-op when the queue is empty, which is every
	// transition for native-mid-turn providers. Offloaded to a goroutine —
	// flushPendingMessages calls Query, and hook handlers must not block the
	// runtime broadcast loop.
	if target == StateIdle {
		go s.flushPendingMessages()
		// Scheduler idle-boundary delivery: queued scheduled runs fire here
		// instead of waiting for the next tick. Async — hook handlers must
		// not block the runtime broadcast loop.
		s.mu.Lock()
		idleCb := s.onIdle
		s.mu.Unlock()
		if idleCb != nil {
			go idleCb()
		}
	}
}

// handleWatchdogEvent translates runtime watchdog events to agentique error
// broadcasts. The runtime emits a state transition for fatal kinds, so we
// don't change state here.
func handleWatchdogEvent(s *Session, ev runtime.WatchdogEvent) {
	fatal := ev.Kind == runtime.WatchdogThinkingFail ||
		ev.Kind == runtime.WatchdogToolStallFail ||
		ev.Kind == runtime.WatchdogCLIDead

	msg := ev.Message
	if msg == "" {
		msg = fmt.Sprintf("watchdog: %s", ev.Kind)
	}

	if fatal {
		slog.Error("watchdog fatal", "session_id", s.ID, "kind", ev.Kind, "message", msg)
		// A fatal watchdog verdict comes with a runtime StateFailed transition
		// but no CLIEvent, so the pipeline's open turn would never close: the
		// next turn start burns WaitTurnClosed's full timeout, and turn
		// subscribers (the scheduler's waitForOutcome has no timeout) wait
		// forever. Abort the turn and deliver the failure. Idempotent against
		// the pipeline's own fatal-error abort — whichever runs second finds
		// the turn already closed.
		if outcome, ok := s.pipeline.AbortTurn(msg); ok {
			s.turnReg.Deliver(outcome)
		}
	} else {
		slog.Warn("watchdog warning", "session_id", s.ID, "kind", ev.Kind, "message", msg, "elapsed", ev.Elapsed)
	}

	s.broadcastSessionEvent(WireErrorEvent{
		Type:    "error",
		Content: msg,
		Fatal:   fatal,
	})
}

// handlePendingChange resolves auto-bypassable approvals immediately and
// broadcasts the rest to the UI as session.tool-permission. Runs on a
// goroutine — runtime fires PendingChangeEvent inline and we don't want the
// SubmitApproval round-trip to block runtime's broadcast loop.
//
// Serialized on pendingMu because the goroutines are otherwise unordered: two
// in flight could read PendingState in either order and leave the surfaced-id
// tracking (see resolveWithdrawnPrompts) describing a state that has already
// passed. Taking the lock first and reading PendingState inside it means every
// critical section sees the current truth, so a late goroutine is a no-op
// rather than a rewind.
func handlePendingChange(s *Session, ev runtime.PendingChangeEvent) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()

	s.mu.Lock()
	rt := s.rt
	autoMode := s.autoApproveMode
	permMode := s.permissionMode
	s.mu.Unlock()
	if rt == nil {
		return
	}

	rtA, rtQ := rt.PendingState()

	// A pending prompt can now leave the queue with no user answer — the
	// runtime withdraws it and fires this event, after which SubmitApproval
	// answers ErrPendingNotFound forever. Nothing else tells the UI, so
	// without this a withdrawn prompt leaves its banner up for good and every
	// click on it fails.
	s.resolveWithdrawnPrompts(rtA, rtQ)

	if rtA != nil {
		handled := false
		// Lazy browser launch (pump-driven modes): a browser tool needs Chrome up
		// before it executes. EnsureBrowser is a local op (no CLI control-channel
		// round-trip) — the agent's Playwright MCP connects over CDP when the
		// approved call runs, so having Chrome up before we approve is sufficient.
		// On failure, deny with the actionable message rather than letting the call
		// fail opaquely. This branch covers default/acceptEdits/plan/auto (all
		// runtime.AutoApproveOff, so every tool reaches the pump). fullAuto
		// (runtime.AutoApproveAll) bypasses the pump entirely and is handled instead
		// by Session.interceptBrowserTool, which fires inside the permission callback.
		if isBrowserTool(rtA.ToolName) {
			if err := s.ensureBrowser(); err != nil {
				if e := rt.SubmitApproval(rtA.ID, runtime.Decision{Allow: false, DenyMessage: err.Error()}); e != nil && e != runtime.ErrPendingNotFound {
					slog.Warn("browser-ensure deny failed", "session_id", s.ID, "approval_id", rtA.ID, "error", e)
				}
				s.broadcast("session.approval-auto-resolved", PushApprovalResolved{SessionID: s.ID, ApprovalID: rtA.ID})
				handled = true
			}
		}
		if !handled && shouldBypassPermission(autoMode, permMode, rtA.ToolName) {
			if err := rt.SubmitApproval(rtA.ID, runtime.Decision{Allow: true}); err != nil && err != runtime.ErrPendingNotFound {
				slog.Warn("auto-resolve approval failed", "session_id", s.ID, "approval_id", rtA.ID, "error", err)
			}
			s.broadcast("session.approval-auto-resolved", PushApprovalResolved{SessionID: s.ID, ApprovalID: rtA.ID})
		} else if !handled {
			s.broadcast("session.tool-permission", PushToolPermission{
				SessionID:  s.ID,
				ApprovalID: rtA.ID,
				ToolName:   rtA.ToolName,
				Input:      rtA.Input,
			})
			s.broadcast("project.activity-item", approvalActivityItem(s, rtA.ID, rtA.ToolName, rtA.Input))
		}
	}

	if rtQ != nil {
		wireQs := make([]WireQuestion, len(rtQ.Questions))
		for i, q := range rtQ.Questions {
			opts := make([]WireQuestionOption, len(q.Options))
			for j, o := range q.Options {
				opts[j] = WireQuestionOption{Label: o.Label, Description: o.Description}
			}
			wireQs[i] = WireQuestion{
				Question:    q.Question,
				Header:      q.Header,
				Options:     opts,
				MultiSelect: q.MultiSelect,
			}
		}
		s.broadcast("session.user-question", PushUserQuestion{
			SessionID:  s.ID,
			QuestionID: rtQ.ID,
			Questions:  wireQs,
		})
	}
}

// resolveWithdrawnPrompts broadcasts a resolution for any prompt agentique has
// put in front of the user that the runtime no longer has pending, so the UI
// clears it. rtA/rtQ are the live pending state; either being nil — or carrying
// a different id — means whatever was surfaced before is gone.
//
// It fires for ordinary resolutions too (the user answered, or a bypass
// auto-approved), where the RPC has already broadcast the same resolution. That
// duplicate is deliberate: making the clear pending-state-driven rather than
// reply-path-driven is what covers the withdrawal case, which has no reply path
// at all. Callers must hold pendingMu.
func (s *Session) resolveWithdrawnPrompts(rtA *runtime.PendingApproval, rtQ *runtime.PendingQuestion) {
	var liveApprovalID, liveQuestionID string
	if rtA != nil {
		liveApprovalID = rtA.ID
	}
	if rtQ != nil {
		liveQuestionID = rtQ.ID
	}

	s.mu.Lock()
	goneApproval, goneQuestion := "", ""
	if s.surfacedApprovalID != "" && s.surfacedApprovalID != liveApprovalID {
		goneApproval = s.surfacedApprovalID
	}
	if s.surfacedQuestionID != "" && s.surfacedQuestionID != liveQuestionID {
		goneQuestion = s.surfacedQuestionID
	}
	s.surfacedApprovalID = liveApprovalID
	s.surfacedQuestionID = liveQuestionID
	s.mu.Unlock()

	if goneApproval != "" {
		s.broadcast("session.approval-resolved", PushApprovalResolved{SessionID: s.ID, ApprovalID: goneApproval})
	}
	if goneQuestion != "" {
		s.broadcast("session.question-resolved", PushQuestionResolved{SessionID: s.ID, QuestionID: goneQuestion})
	}
}

// approvalActivityItem builds the live wire-feed entry for a pending tool
// approval surfacing to the user. Approvals never reach session_events —
// they are pump-side PushToolPermission broadcasts, not CLI wire events, so
// persistEvent never sees them. These items therefore exist only on the live
// "project.activity-item" push; wire.list backfill will not include them.
func approvalActivityItem(s *Session, approvalID, toolName string, input json.RawMessage) ActivityItem {
	content := toolName
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(input, &fields); err == nil {
		var cmd string
		if raw, ok := fields["command"]; ok && json.Unmarshal(raw, &cmd) == nil && cmd != "" {
			content = truncate(toolName+": "+cmd, 200)
		}
	}
	item := ActivityItem{
		Kind:      "event",
		ItemID:    "appr-" + approvalID,
		SourceID:  s.ID,
		Content:   content,
		EventType: "approval",
		CreatedAt: time.Now().UTC().Format("2006-01-02T15:04:05.000"),
	}
	if dbSess, err := s.queries.GetSession(context.Background(), s.ID); err == nil {
		item.SourceName = dbSess.Name
	}
	if proj, err := s.queries.GetProject(context.Background(), s.ProjectID); err == nil {
		item.ProjectSlug = proj.Slug
	}
	return item
}
