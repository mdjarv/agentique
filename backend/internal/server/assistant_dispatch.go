package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mdjarv/agentique/backend/internal/assistant"
	"github.com/mdjarv/agentique/backend/internal/mcphttp"
	"github.com/mdjarv/agentique/backend/internal/session"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// maxSpokenSummary bounds the finished-run summary handed to the speaking
// model. It is a prompt for one spoken sentence, not the answer itself — the
// listener can read the full text on screen, and feeding a whole essay in
// produces a monologue nobody wants in a car.
const maxSpokenSummary = 600

// turnsBack is how many turns of history to scan for the closing words. One:
// the turn that just ended. Looking further would risk speaking the *previous*
// turn's answer when this one ended without saying anything.
const turnsBack = 1

// assistantDispatcher hands a drafted prompt to the session that does the work.
//
// It is deliberately thin: the prompt goes down the *same* path the composer's
// send button uses, so there is one route into the session pipeline whether the
// gesture was a click, a sentence in the thread, or a sentence on a call.
type assistantDispatcher struct {
	svc        *session.Service
	queries    *store.Queries
	summarizer *sessionSummarizer
}

// Dispatch implements assistant.Dispatcher.
//
// The reporting instruction rides along only when the operator said they were
// staying on the line. A run nobody is listening to carries none of it — no
// instruction, no tool calls, no overhead — which is the whole reason the
// handoff asks instead of assuming.
func (d *assistantDispatcher) Dispatch(ctx context.Context, sessionID, prompt string, withReporting bool) (assistant.Delivery, error) {
	if withReporting {
		prompt += "\n\n" + assistant.ReportingInstructions(mcphttp.AssistantReportToolFullName)
	}

	delivery, err := d.svc.EnqueueMessage(ctx, sessionID, prompt, nil)
	if err != nil {
		return "", err
	}
	// Mapped rather than cast: the assistant's vocabulary is its own, so a
	// rename on either side is a compile error here instead of a silently wrong
	// sentence.
	switch delivery {
	case session.DeliveryMidTurn:
		return assistant.DeliveryMidTurn, nil
	case session.DeliveryQueued:
		return assistant.DeliveryQueued, nil
	case session.DeliveryTurn:
		return assistant.DeliveryTurn, nil
	default:
		return assistant.DeliveryTurn, nil
	}
}

// AutoRunnable implements assistant.Dispatcher.
//
// Live voice has no spoken approval, so a session that would stop and ask is
// refused at the handoff. The alternative is a run that stalls invisibly while
// the call sounds perfectly healthy.
func (d *assistantDispatcher) AutoRunnable(ctx context.Context, sessionID string) (bool, string, error) {
	info, err := d.svc.GetSessionInfo(ctx, sessionID)
	if err != nil {
		return false, "", err
	}
	// "Some auto" is not enough. Under accept-edits a Bash prompt still blocks,
	// and with no way to answer it the run simply stops with nobody told.
	if info.AutoApproveMode == autoApproveAll {
		return true, "", nil
	}
	return false, fmt.Sprintf("It is currently set to %q.", info.AutoApproveMode), nil
}

// autoApproveAll is the only mode that never stops for a prompt: it maps to
// runtime.AutoApproveAll, which bypasses the permission pump entirely
// (see runtimeAutoApproveMode). Every other mode can block on a tool.
const autoApproveAll = "fullAuto"

// maxProjectContext bounds what the drafter is told about the project.
//
// A budget rather than a truncation accident: everything here is sent to the
// speech vendor on every call, and a drafter given the whole of CLAUDE.md asks
// worse questions than one given its opening summary, not better.
const maxProjectContext = 4000

// ProjectContext implements assistant.Dispatcher.
//
// The drafter needs enough to ask sharp questions and name files — not the file
// tree, not the history. What it gets is the session's own identity plus the
// head of the project's CLAUDE.md, which is where a repository explains itself.
func (d *assistantDispatcher) ProjectContext(ctx context.Context, sessionID string) string {
	info, err := d.svc.GetSessionInfo(ctx, sessionID)
	if err != nil {
		slog.Warn("assistant: no project context", "session", sessionID, "error", err)
		return ""
	}

	var b strings.Builder
	if info.Name != "" {
		fmt.Fprintf(&b, "The session is called %q.\n", info.Name)
	}
	if info.WorktreeBranch != "" {
		fmt.Fprintf(&b, "It is working on branch %s.\n", info.WorktreeBranch)
	}

	project, err := d.queries.GetProject(ctx, info.ProjectID)
	if err == nil {
		if project.Name != "" {
			fmt.Fprintf(&b, "The project is %s.\n", project.Name)
		}
		if guide := readProjectGuide(project.Path); guide != "" {
			b.WriteString("\nFrom the project's CLAUDE.md:\n\n")
			b.WriteString(guide)
		}
	}

	// What the session has been doing, distilled locally rather than shipped
	// raw. The transcript never leaves the machine; only this paragraph does.
	//
	// Cached only, with a background warm otherwise. This runs while the
	// operator is waiting to be heard, and summarising spawns a provider-CLI
	// subprocess that on a long session regularly outlasts any budget worth
	// waiting out. A summary is worth having when it is already there and never
	// worth a silent microphone, so a cold session opens without one and the
	// next call has it. That is the same trade docs/voice.md already makes for a
	// summariser that misses its budget — moved off the critical path rather
	// than timed out on it.
	if summary := d.summarizer.Cached(sessionID); summary != "" {
		b.WriteString("\n\nWhat this session has been working on:\n\n")
		b.WriteString(summary)
	} else {
		d.summarizer.Warm(ctx, sessionID)
	}
	return strings.TrimSpace(b.String())
}

// readProjectGuide returns the head of a project's CLAUDE.md, or "".
//
// Best effort by design: a project without one is ordinary, and a drafter that
// refuses to work because a file is missing would be worse than a vague one.
func readProjectGuide(projectPath string) string {
	if projectPath == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(projectPath, "CLAUDE.md"))
	if err != nil {
		return ""
	}
	text := string(data)
	if utf8.RuneCountInString(text) <= maxProjectContext {
		return strings.TrimSpace(text)
	}
	runes := []rune(text)
	return strings.TrimSpace(string(runes[:maxProjectContext])) + "\n\n[…truncated]"
}

// assistantTurnFacts answers what the runtime knows about a turn that has just
// ended: the three things a working agent CANNOT report about itself — it is
// blocked, it died, it stopped — which is why they come from here rather than
// from a tool call.
//
// It implements assistant.TurnFacts, the seam that keeps internal/assistant off
// the session pipeline. The reads are the ones the voice watcher made before the
// core existed; what moved is who ranks them ([assistant.TurnNotice]) and who is
// told ([assistant.Service.OnTurnEnd], and the journal behind it).
type assistantTurnFacts struct {
	svc     *session.Service
	queries *store.Queries
}

// PendingHumanInput implements assistant.TurnFacts.
func (f *assistantTurnFacts) PendingHumanInput(sessionID string) string {
	return f.svc.PendingHumanInput(sessionID)
}

// TurnOutcome implements assistant.TurnFacts.
func (f *assistantTurnFacts) TurnOutcome(ctx context.Context, sessionID string) (assistant.TurnOutcome, error) {
	info, err := f.svc.GetSessionInfo(ctx, sessionID)
	if err != nil {
		return assistant.TurnOutcome{}, fmt.Errorf("session %s: %w", sessionID, err)
	}
	return assistant.TurnOutcome{
		Failed:       info.State == string(session.StateFailed),
		ProjectID:    info.ProjectID,
		SessionName:  info.Name,
		ClosingWords: f.closingWords(ctx, sessionID),
	}, nil
}

// voiceTurnWatcher keeps a live call hearing its runtime facts on a server that
// has the assistant itself switched off.
//
// A call is a head on the assistant, so with the core built the one turn-end
// listener is [assistant.Service.OnTurnEnd]: it journals the fact and notifies
// the followers, and the call is one of them. But the core is opt-in and a call
// must not go deaf because it is off, so this is the same fact delivered to the
// same registry with nothing written down. The RANKING is not duplicated — both
// paths read [assistant.TurnNotice] — because a session that says "needs
// approval" in one place cannot say something else in your ear.
type voiceTurnWatcher struct {
	registry *assistant.Registry
	facts    assistant.TurnFacts
}

func newVoiceTurnWatcher(registry *assistant.Registry, facts assistant.TurnFacts) *voiceTurnWatcher {
	return &voiceTurnWatcher{registry: registry, facts: facts}
}

// OnTurnEnd is the dispatch point. It runs on the event-loop goroutine, so the
// no-listener case must stay cheap and the rest is handed to a goroutine.
func (w *voiceTurnWatcher) OnTurnEnd(sessionID string) {
	// The overwhelmingly common case: nobody is on a call for this session.
	// One map lookup, then out.
	if !w.registry.Listening(sessionID) {
		return
	}
	go w.push(sessionID)
}

func (w *voiceTurnWatcher) push(sessionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), turnFactsBudget)
	defer cancel()

	notice, _, err := assistant.TurnNotice(ctx, w.facts, sessionID)
	if err != nil {
		slog.Warn("voice watcher: turn outcome unavailable", "session", sessionID, "error", err)
		return
	}
	w.registry.Notice(sessionID, notice)
}

// turnFactsBudget bounds the reads behind one turn-end notice. The caller is a
// goroutine off the runtime's event loop, so an unbounded read here is a
// goroutine held for the life of the process rather than a stalled turn.
const turnFactsBudget = 15 * time.Second

// closingWords returns the turn's last assistant text, clamped.
//
// Empty is a fine answer: the notice's own preamble already tells the model
// what happened, and a run that ended without saying anything should not have
// words invented for it.
func (f *assistantTurnFacts) closingWords(ctx context.Context, sessionID string) string {
	events, err := f.queries.ListRecentEventsBySession(ctx, store.ListRecentEventsBySessionParams{
		SessionID: sessionID,
		// Column2 is a count of turns, not of rows.
		Column2: turnsBack,
	})
	if err != nil {
		slog.Warn("assistant: event lookup failed", "session", sessionID, "error", err)
		return ""
	}

	// Newest first or oldest first depends on the query; scan from the end
	// backwards and take the first text either way by preferring the highest id.
	var best store.SessionEvent
	for _, ev := range events {
		if ev.Type != "text" {
			continue
		}
		if best.ID == 0 || ev.ID > best.ID {
			best = ev
		}
	}
	if best.ID == 0 {
		return ""
	}

	var payload struct {
		Content string `json:"content"`
		Text    string `json:"text"`
	}
	if err := json.Unmarshal([]byte(best.Data), &payload); err != nil {
		return ""
	}
	text := payload.Content
	if text == "" {
		text = payload.Text
	}
	return clampSpoken(text)
}

// clampSpoken trims a summary to something speakable, on a rune boundary.
func clampSpoken(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) <= maxSpokenSummary {
		return text
	}
	runes := []rune(text)
	return strings.TrimSpace(string(runes[:maxSpokenSummary])) + "…"
}
