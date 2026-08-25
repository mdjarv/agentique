package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"unicode/utf8"

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
	registry *voice.Registry
	svc      *session.Service
	queries  *store.Queries
}

func newVoiceTurnWatcher(registry *voice.Registry, svc *session.Service, queries *store.Queries) *voiceTurnWatcher {
	return &voiceTurnWatcher{registry: registry, svc: svc, queries: queries}
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
