package session

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/allbin/agentkit/runtime"
	claudecli "github.com/allbin/claudecli-go"
)

// makeBroadcastHook returns a runtime BroadcastFunc bound to the given
// agentique Session. The hook fans out events:
//
//   - CLIEvent      → pipeline.ProcessEvent (persistence + UI broadcast)
//   - StateChange   → DB persist + UI snapshot, plus completedAt on Done
//   - WatchdogEvent → log + UI error broadcast (fatal kinds also surface a
//     state transition via the StateChange that follows)
//   - PendingChange → check shouldBypassPermission for "auto" mode and
//     auto-resolve, otherwise broadcast tool-permission to the UI.
//
// All hook handlers must be non-blocking; long work is offloaded to goroutines.
func makeBroadcastHook(s *Session) runtime.BroadcastFunc {
	return func(_ context.Context, e runtime.Event) {
		switch ev := e.(type) {
		case runtime.CLIEvent:
			safeProcessEvent(s, ev.Event)
		case runtime.StateChangeEvent:
			handleRuntimeStateChange(s, ev)
		case runtime.WatchdogEvent:
			handleWatchdogEvent(s, ev)
		case runtime.PendingChangeEvent:
			go handlePendingChange(s, ev)
		}
	}
}

// safeProcessEvent runs the pipeline with panic recovery so a malformed event
// can't kill the runtime event-loop goroutine.
func safeProcessEvent(s *Session, event claudecli.Event) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic in pipeline.ProcessEvent", "session_id", s.ID, "panic", r)
			s.broadcast("session.event", PushSessionEvent{
				SessionID: s.ID,
				Event: WireErrorEvent{
					Type:    "error",
					Content: fmt.Sprintf("internal error processing event: %v", r),
					Fatal:   false,
				},
			})
		}
	}()
	s.pipeline.ProcessEvent(event)
}

// handleRuntimeStateChange mirrors a runtime state transition into agentique:
// updates the in-memory state field, persists to DB, sets completedAt on Done,
// and broadcasts a snapshot. While a git operation is in progress, the merging
// dance owns the visible state — runtime transitions are dropped here.
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
	if ev.To == runtime.StateDone && s.completedAt == "" {
		s.completedAt = nowUTC()
	}
	s.state = target
	s.mu.Unlock()

	s.persistState(target)
	if ev.To == runtime.StateDone {
		if err := s.queries.SetSessionCompleted(context.Background(), s.ID); err != nil {
			slog.Error("persist session completed failed", "session_id", s.ID, "error", err)
		}
	}

	select {
	case s.stateChangedCh <- struct{}{}:
	default:
	}

	s.broadcastState(target)
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
	} else {
		slog.Warn("watchdog warning", "session_id", s.ID, "kind", ev.Kind, "message", msg, "elapsed", ev.Elapsed)
	}

	s.broadcast("session.event", PushSessionEvent{
		SessionID: s.ID,
		Event: WireErrorEvent{
			Type:    "error",
			Content: msg,
			Fatal:   fatal,
		},
	})
}

// handlePendingChange resolves auto-bypassable approvals immediately and
// broadcasts the rest to the UI as session.tool-permission. Runs on a
// goroutine — runtime fires PendingChangeEvent inline and we don't want the
// SubmitApproval round-trip to block runtime's broadcast loop.
func handlePendingChange(s *Session, ev runtime.PendingChangeEvent) {
	s.mu.Lock()
	rt := s.rt
	autoMode := s.autoApproveMode
	permMode := s.permissionMode
	s.mu.Unlock()
	if rt == nil {
		return
	}

	rtA, rtQ := rt.PendingState()

	if rtA != nil {
		if shouldBypassPermission(autoMode, permMode, rtA.ToolName) {
			if err := rt.SubmitApproval(rtA.ID, runtime.Decision{Allow: true}); err != nil && err != runtime.ErrPendingNotFound {
				slog.Warn("auto-resolve approval failed", "session_id", s.ID, "approval_id", rtA.ID, "error", err)
			}
			s.broadcast("session.approval-auto-resolved", PushApprovalResolved{SessionID: s.ID, ApprovalID: rtA.ID})
		} else {
			s.broadcast("session.tool-permission", PushToolPermission{
				SessionID:  s.ID,
				ApprovalID: rtA.ID,
				ToolName:   rtA.ToolName,
				Input:      rtA.Input,
			})
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
