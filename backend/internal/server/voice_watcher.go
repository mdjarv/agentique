package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/mdjarv/agentique/backend/internal/mcphttp"
	"github.com/mdjarv/agentique/backend/internal/session"
	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/mdjarv/agentique/backend/internal/voice"
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

// voiceDispatcher hands a voice-drafted prompt to a session.
//
// It is deliberately thin: the prompt goes down the *same* path the composer's
// send button uses, so there is one route into the session pipeline whether the
// gesture was a click or a sentence.
type voiceDispatcher struct {
	svc        *session.Service
	queries    *store.Queries
	summarizer *sessionSummarizer
}

// Dispatch implements voice.Dispatcher.
//
// The reporting instruction rides along only when the operator said they were
// staying on the line. A run nobody is listening to carries none of it — no
// instruction, no tool calls, no overhead — which is the whole reason the
// handoff asks instead of assuming.
func (d *voiceDispatcher) Dispatch(ctx context.Context, sessionID, prompt string, withReporting bool) (voice.Delivery, error) {
	if withReporting {
		prompt += "\n\n" + voice.ReportingInstructions(mcphttp.VoiceReportToolFullName)
	}

	delivery, err := d.svc.EnqueueMessage(ctx, sessionID, prompt, nil)
	if err != nil {
		return "", err
	}
	// Mapped rather than cast: the voice vocabulary is its own, so a rename on
	// either side is a compile error here instead of a silently wrong sentence.
	switch delivery {
	case session.DeliveryMidTurn:
		return voice.DeliveryMidTurn, nil
	case session.DeliveryQueued:
		return voice.DeliveryQueued, nil
	case session.DeliveryTurn:
		return voice.DeliveryTurn, nil
	default:
		return voice.DeliveryTurn, nil
	}
}

// AutoRunnable implements voice.Dispatcher.
//
// Live voice has no spoken approval, so a session that would stop and ask is
// refused at the handoff. The alternative is a run that stalls invisibly while
// the call sounds perfectly healthy.
func (d *voiceDispatcher) AutoRunnable(ctx context.Context, sessionID string) (bool, string, error) {
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

// ProjectContext implements voice.Dispatcher.
//
// The drafter needs enough to ask sharp questions and name files — not the file
// tree, not the history. What it gets is the session's own identity plus the
// head of the project's CLAUDE.md, which is where a repository explains itself.
func (d *voiceDispatcher) ProjectContext(ctx context.Context, sessionID string) string {
	info, err := d.svc.GetSessionInfo(ctx, sessionID)
	if err != nil {
		slog.Warn("voice: no project context", "session", sessionID, "error", err)
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
	if summary := d.summarizer.Summary(ctx, sessionID); summary != "" {
		b.WriteString("\n\nWhat this session has been working on:\n\n")
		b.WriteString(summary)
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

// voiceTurnWatcher pushes the three things a working agent cannot report about
// itself — that it is blocked, that it died, that it finished — to whoever is
// listening on a live call.
//
// It hangs off Manager.AddTurnEndListener, which fires once per turn "on any
// session, after that turn has stopped... completion, a CLI that died, a
// session closed mid-flight". That covers the cases a completion hook would
// miss, which is exactly why the runtime rather than the agent is the source
// here: a suspended or dead agent cannot call a tool.
type voiceTurnWatcher struct {
	registry   *voice.Registry
	svc        *session.Service
	queries    *store.Queries
	summarizer *sessionSummarizer
}

func newVoiceTurnWatcher(registry *voice.Registry, svc *session.Service, queries *store.Queries, summarizer *sessionSummarizer) *voiceTurnWatcher {
	return &voiceTurnWatcher{registry: registry, svc: svc, queries: queries, summarizer: summarizer}
}

// OnTurnEnd is the dispatch point. It runs on the event-loop goroutine, so the
// no-listener case must stay cheap and the rest is handed to a goroutine.
func (w *voiceTurnWatcher) OnTurnEnd(sessionID string) {
	// A cached summary describes the session as it was before this turn, so it
	// is stale the moment the turn ends. Dropping it is a map delete, cheap
	// enough to do for every session whether or not anyone is on a call.
	w.summarizer.Forget(sessionID)

	// The overwhelmingly common case: nobody is on a call for this session.
	// One map lookup, then out.
	if !w.registry.Listening(sessionID) {
		return
	}
	go w.push(sessionID)
}

func (w *voiceTurnWatcher) push(sessionID string) {
	ctx := context.Background()

	// Blocked outranks failure, matching lib/session/priority.ts: the thing
	// still holding a process is more urgent than the thing that already
	// stopped.
	if pending := w.svc.PendingHumanInput(sessionID); pending != "" {
		w.registry.Notice(sessionID, voice.Notice{
			Kind:     voice.NoticeBlocked,
			Headline: pending,
		})
		return
	}

	info, err := w.svc.GetSessionInfo(ctx, sessionID)
	if err != nil {
		slog.Warn("voice watcher: session lookup failed", "session", sessionID, "error", err)
		return
	}

	if info.State == string(session.StateFailed) {
		w.registry.Notice(sessionID, voice.Notice{
			Kind:     voice.NoticeFailed,
			Headline: w.closingWords(ctx, sessionID),
		})
		return
	}

	w.registry.Notice(sessionID, voice.Notice{
		Kind:     voice.NoticeFinished,
		Headline: w.closingWords(ctx, sessionID),
	})
}

// closingWords returns the turn's last assistant text, clamped.
//
// Empty is a fine answer: the notice's own preamble already tells the model
// what happened, and a run that ended without saying anything should not have
// words invented for it.
func (w *voiceTurnWatcher) closingWords(ctx context.Context, sessionID string) string {
	events, err := w.queries.ListRecentEventsBySession(ctx, store.ListRecentEventsBySessionParams{
		SessionID: sessionID,
		// Column2 is a count of turns, not of rows.
		Column2: turnsBack,
	})
	if err != nil {
		slog.Warn("voice watcher: event lookup failed", "session", sessionID, "error", err)
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
