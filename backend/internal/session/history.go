package session

import (
	"context"
	"encoding/json"

	"github.com/allbin/agentique/backend/internal/store"
)

// HistoryTurn represents a single turn (prompt + response events) for replay.
type HistoryTurn struct {
	Prompt string            `json:"prompt"`
	Attachments []QueryAttachment      `json:"attachments,omitempty"`
	Events []json.RawMessage `json:"events"`
}

// HistoryFromDB reconstructs turn history from persisted events.
func HistoryFromDB(ctx context.Context, q *store.Queries, sessionID string) ([]HistoryTurn, error) {
	rows, err := q.ListEventsBySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	turnMap := make(map[int64]*HistoryTurn)
	var turnOrder []int64

	for _, row := range rows {
		t, ok := turnMap[row.TurnIndex]
		if !ok {
			t = &HistoryTurn{}
			turnMap[row.TurnIndex] = t
			turnOrder = append(turnOrder, row.TurnIndex)
		}

		if row.Type == "prompt" {
			var p struct {
				Prompt string       `json:"prompt"`
				Attachments []QueryAttachment `json:"attachments,omitempty"`
			}
			if json.Unmarshal([]byte(row.Data), &p) == nil {
				t.Prompt = p.Prompt
				t.Attachments = p.Attachments
			}
		} else {
			t.Events = append(t.Events, json.RawMessage(row.Data))
		}
	}

	turns := make([]HistoryTurn, 0, len(turnOrder))
	for _, idx := range turnOrder {
		t := turnMap[idx]
		if t.Events == nil {
			t.Events = []json.RawMessage{}
		}
		turns = append(turns, *t)
	}

	return turns, nil
}
