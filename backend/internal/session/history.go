package session

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/allbin/agentique/backend/internal/store"
)

// NormalizeEventJSON rewrites legacy JSON keys to the current wire format.
// Old tool_use events used "id"/"name"/"input"; current format uses "toolId"/"toolName"/"toolInput".
// Old tool_result events used "toolUseId"; current format uses "toolId".
func NormalizeEventJSON(eventType string, data []byte) json.RawMessage {
	if eventType != "tool_use" && eventType != "tool_result" {
		return json.RawMessage(data)
	}

	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return json.RawMessage(data)
	}

	changed := false
	switch eventType {
	case "tool_use":
		if _, ok := m["toolName"]; ok {
			break
		}
		if v, ok := m["id"]; ok {
			m["toolId"] = v
			delete(m, "id")
			changed = true
		}
		if v, ok := m["name"]; ok {
			m["toolName"] = v
			delete(m, "name")
			changed = true
		}
		if v, ok := m["input"]; ok {
			m["toolInput"] = v
			delete(m, "input")
			changed = true
		}
	case "tool_result":
		if v, ok := m["toolUseId"]; ok {
			m["toolId"] = v
			delete(m, "toolUseId")
			changed = true
		}
		// Migrate string content to array of content blocks.
		if s, ok := m["content"].(string); ok {
			m["content"] = []map[string]string{{"type": "text", "text": s}}
			changed = true
		}
	}

	if !changed {
		return json.RawMessage(data)
	}

	out, err := json.Marshal(m)
	if err != nil {
		return json.RawMessage(data)
	}
	return json.RawMessage(out)
}

// HistoryTurn represents a single turn (prompt + response events) for replay.
type HistoryTurn struct {
	Prompt string            `json:"prompt"`
	Attachments []QueryAttachment      `json:"attachments,omitempty"`
	Events []json.RawMessage `json:"events"`
}

// RecentHistoryFromDB returns the most recent N turns from persisted events.
func RecentHistoryFromDB(ctx context.Context, q historyQueries, sessionID string, limit int) ([]HistoryTurn, error) {
	rows, err := q.ListRecentEventsBySession(ctx, store.ListRecentEventsBySessionParams{
		SessionID: sessionID,
		Column2:   int64(limit),
	})
	if err != nil {
		return nil, err
	}
	return buildTurns(rows), nil
}

// HistoryFromDB reconstructs turn history from persisted events.
func HistoryFromDB(ctx context.Context, q historyQueries, sessionID string) ([]HistoryTurn, error) {
	rows, err := q.ListEventsBySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return buildTurns(rows), nil
}

// buildTurns groups ordered event rows into HistoryTurns.
func buildTurns(rows []store.SessionEvent) []HistoryTurn {
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
				Prompt      string            `json:"prompt"`
				Attachments []QueryAttachment `json:"attachments,omitempty"`
			}
			if json.Unmarshal([]byte(row.Data), &p) == nil {
				t.Prompt = p.Prompt
				t.Attachments = p.Attachments
			}
		} else {
			ev := NormalizeEventJSON(row.Type, []byte(row.Data))
			if row.Type == "result" && row.CreatedAt != "" {
				ev = injectTimestamp(ev, row.CreatedAt)
			}
			ev = injectEventID(ev, row.ID)
			t.Events = append(t.Events, ev)
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
	return turns
}

// injectEventID prepends a stable "id" field to an event JSON object.
// Uses the DB autoincrement ID prefixed with "e" (e.g. "e42").
func injectEventID(data json.RawMessage, id int64) json.RawMessage {
	if len(data) < 2 || data[0] != '{' {
		return data
	}
	idField := `"id":"e` + strconv.FormatInt(id, 10) + `"`
	if data[1] != '}' {
		idField += ","
	}
	return json.RawMessage(`{` + idField + string(data[1:]))
}

// injectTimestamp parses a DB created_at string ("2006-01-02T15:04:05.000")
// and injects it as "timestamp" (epoch ms) into the event JSON.
func injectTimestamp(data json.RawMessage, createdAt string) json.RawMessage {
	t, err := time.Parse("2006-01-02T15:04:05.000", createdAt)
	if err != nil {
		return data
	}
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return data
	}
	m["timestamp"] = t.UnixMilli()
	out, err := json.Marshal(m)
	if err != nil {
		return data
	}
	return json.RawMessage(out)
}
