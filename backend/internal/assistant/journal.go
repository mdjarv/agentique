package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mdjarv/agentique/backend/internal/store"
)

// timeFormat pins every assistant timestamp to UTC RFC3339 seconds, as the
// scheduler's are. SQLite compares TEXT lexicographically, so mixed precision
// or offsets would break the journal's own ordering.
const timeFormat = "2006-01-02T15:04:05Z"

func formatTime(t time.Time) string { return t.UTC().Truncate(time.Second).Format(timeFormat) }

// JournalKind is what happened. The set is closed: a new kind is a decision
// about what the digest ranks and what compaction folds, not a free string.
type JournalKind string

const (
	// JournalSessionFinished — a turn ended cleanly.
	JournalSessionFinished JournalKind = "session_finished"
	// JournalSessionFailed — a turn ended badly.
	JournalSessionFailed JournalKind = "session_failed"
	// JournalSessionBlocked — a run is waiting on something only a screen can
	// give it. Ordered before the other two, as the notices are.
	JournalSessionBlocked JournalKind = "session_blocked"
	// JournalSessionMerged — the branch went in.
	JournalSessionMerged JournalKind = "session_merged"
	// JournalSessionArchived — the operator filed it away.
	JournalSessionArchived JournalKind = "session_archived"
	// JournalLoopPaused — a scheduled loop auto-paused on repeated failures
	// and stays paused until a person acts. Written by the scheduler's own
	// observer (M4); the kind is named here so the closed set is the closed
	// set.
	JournalLoopPaused JournalKind = "loop_paused"
	// JournalReport — a worker reported something. Always untrusted: it is
	// agent-written text about content nobody here authored.
	JournalReport JournalKind = "report"
	// JournalDispatched — the assistant sent a prompt somewhere.
	JournalDispatched JournalKind = "dispatched"
	// JournalSessionCreated — the assistant made a session.
	JournalSessionCreated JournalKind = "session_created"
	// JournalNote — something the assistant or the operator wanted kept.
	//
	// Not in docs/assistant.md's original list, and added deliberately: the
	// contract's own verb table has `note` as a contained verb that "writes a
	// journal entry", and none of the other kinds describes it. `report` is the
	// nearest and is wrong in the one way that matters — a report is untrusted
	// by construction, where a note is the assistant's own sentence.
	JournalNote JournalKind = "note"
	// JournalDaySummary — a day's raw rows, folded (M4).
	JournalDaySummary JournalKind = "day_summary"
)

// journalKinds is the closed set, in no particular order. Exported through
// [JournalKinds] so a test can assert the set rather than a list of literals.
var journalKinds = []JournalKind{
	JournalSessionFinished,
	JournalSessionFailed,
	JournalSessionBlocked,
	JournalSessionMerged,
	JournalSessionArchived,
	JournalLoopPaused,
	JournalReport,
	JournalDispatched,
	JournalSessionCreated,
	JournalNote,
	JournalDaySummary,
}

// JournalKinds returns the closed kind set.
func JournalKinds() []JournalKind {
	out := make([]JournalKind, len(journalKinds))
	copy(out, journalKinds)
	return out
}

// Valid reports whether k is one of the closed set.
func (k JournalKind) Valid() bool {
	for _, known := range journalKinds {
		if k == known {
			return true
		}
	}
	return false
}

// JournalEntry is one row of the journal on the wire.
//
// Every field is optional, as every wire field here is: the generated Zod
// schema mirrors these tags, and a required field makes a client reject the
// whole payload from a peer that does not send it.
type JournalEntry struct {
	ID   int64       `json:"id,omitempty"`
	At   string      `json:"at,omitempty"`
	Kind JournalKind `json:"kind,omitempty"`
	// SessionID and ProjectID are the subjects, where the entry has them.
	SessionID string `json:"sessionId,omitempty"`
	ProjectID string `json:"projectId,omitempty"`
	// Summary is the one line a digest or a preamble prints.
	Summary string `json:"summary,omitempty"`
	// Payload is whatever the writer kept.
	Payload map[string]any `json:"payload,omitempty"`
	// Untrusted marks a summary that is agent-written text about repository
	// content. A surface renders it as a quotation; nothing acts on it.
	Untrusted bool `json:"untrusted,omitempty"`
	// Notable marks what consolidation should look at, and exempts the row
	// from compaction.
	Notable bool `json:"notable,omitempty"`
}

// journalWrite is one entry to append.
type journalWrite struct {
	Kind      JournalKind
	SessionID string
	ProjectID string
	Summary   string
	Payload   map[string]any
	Untrusted bool
	Notable   bool
	// At overrides the write time. Empty means now, which is what everything
	// but a compaction wants.
	At string
}

// appendJournal writes one entry, broadcasts it, and returns it.
//
// Every journal write goes through here, which is what makes "every entry is
// announced" a property of the store rather than a discipline at eleven call
// sites. The push is a Broadcast on the global topic, not a Publish: the
// journal has no project.
func (s *Service) appendJournal(ctx context.Context, w journalWrite) (JournalEntry, error) {
	if !w.Kind.Valid() {
		return JournalEntry{}, fmt.Errorf("journal kind %q is not one of the closed set", w.Kind)
	}

	at := w.At
	if at == "" {
		at = formatTime(s.now())
	}
	payload := "{}"
	if len(w.Payload) > 0 {
		encoded, err := json.Marshal(w.Payload)
		if err != nil {
			// A payload that will not marshal must not cost the entry: what
			// happened still happened, and the summary is the part that is read.
			s.log.Warn("assistant journal payload dropped", "kind", w.Kind, "error", err)
		} else {
			payload = string(encoded)
		}
	}

	row, err := s.store.InsertAssistantJournalEntry(ctx, store.InsertAssistantJournalEntryParams{
		At:        at,
		Kind:      string(w.Kind),
		SessionID: w.SessionID,
		ProjectID: w.ProjectID,
		Summary:   w.Summary,
		Payload:   payload,
		Untrusted: boolToInt(w.Untrusted),
		Notable:   boolToInt(w.Notable),
	})
	if err != nil {
		return JournalEntry{}, fmt.Errorf("append journal %s: %w", w.Kind, err)
	}

	entry := journalEntryFrom(row)
	s.broadcast(EventJournal, entry)
	return entry, nil
}

// Journal returns entries at or after since, newest first.
//
// since is UTC RFC3339 seconds, and "" means everything — an empty string
// sorts before every timestamp, which is the whole reason the column is text
// in that one format. limit is clamped to [maxJournalPage].
func (s *Service) Journal(ctx context.Context, since string, limit int) ([]JournalEntry, error) {
	rows, err := s.store.ListAssistantJournalSince(ctx, store.ListAssistantJournalSinceParams{
		Since: since,
		Lim:   int64(clampLimit(limit, maxJournalPage)),
	})
	if err != nil {
		return nil, fmt.Errorf("list journal since %q: %w", since, err)
	}
	return journalEntriesFrom(rows), nil
}

// journalEntryFrom maps a stored row to the wire shape.
func journalEntryFrom(row store.AssistantJournal) JournalEntry {
	entry := JournalEntry{
		ID:        row.ID,
		At:        row.At,
		Kind:      JournalKind(row.Kind),
		SessionID: row.SessionID,
		ProjectID: row.ProjectID,
		Summary:   row.Summary,
		Untrusted: row.Untrusted != 0,
		Notable:   row.Notable != 0,
	}
	if row.Payload != "" && row.Payload != "{}" {
		var payload map[string]any
		if err := json.Unmarshal([]byte(row.Payload), &payload); err == nil {
			entry.Payload = payload
		}
	}
	return entry
}

func journalEntriesFrom(rows []store.AssistantJournal) []JournalEntry {
	out := make([]JournalEntry, 0, len(rows))
	for _, row := range rows {
		out = append(out, journalEntryFrom(row))
	}
	return out
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// clampLimit keeps a caller's page size inside what the server will answer.
// Zero or negative asks for the default, which is the cap.
func clampLimit(limit, cap int) int {
	if limit <= 0 || limit > cap {
		return cap
	}
	return limit
}
