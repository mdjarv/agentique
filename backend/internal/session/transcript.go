package session

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mdjarv/agentique/backend/internal/store"
)

// TranscriptEvents is the one read [RecentTranscript] needs.
type TranscriptEvents interface {
	ListRecentEventsBySession(ctx context.Context, arg store.ListRecentEventsBySessionParams) ([]store.SessionEvent, error)
}

// RecentTranscript renders a session's last few turns as plain text: the
// prompts and the agent's own words, nothing else.
//
// One rendering for two readers — the summariser on this machine and a paired
// server asking for the same text over the peer surface — so a summary of a
// session reads the same whichever machine wrote it. Tool calls and results are
// left out: they are volume without meaning at this altitude. The result is
// bounded to maxBytes and is agent-written text about repository content, so
// every reader treats it as data.
func RecentTranscript(ctx context.Context, q TranscriptEvents, sessionID string, turns int64, maxBytes int) (string, error) {
	events, err := q.ListRecentEventsBySession(ctx, store.ListRecentEventsBySessionParams{
		SessionID: sessionID,
		Column2:   turns,
	})
	if err != nil {
		return "", fmt.Errorf("recent events for %s: %w", sessionID, err)
	}

	var b strings.Builder
	for _, ev := range events {
		var who string
		switch ev.Type {
		case "prompt":
			who = "User"
		case "text":
			who = "Agent"
		default:
			continue
		}

		var payload struct {
			Content string `json:"content"`
			Text    string `json:"text"`
			Prompt  string `json:"prompt"`
		}
		if err := json.Unmarshal([]byte(ev.Data), &payload); err != nil {
			continue
		}
		text := strings.TrimSpace(firstNonEmpty(payload.Content, payload.Text, payload.Prompt))
		if text == "" {
			continue
		}
		fmt.Fprintf(&b, "%s: %s\n\n", who, text)
		if b.Len() > maxBytes {
			break
		}
	}

	out := strings.TrimSpace(b.String())
	if len(out) > maxBytes {
		out = out[:maxBytes]
	}
	return out, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
