package voice

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// viewingNoteInterval is how often the call may mention what the operator is
// looking at.
//
// Clicking around a sidebar is not conversation. Without a floor here, a minute
// of browsing becomes a minute of injected notes, each one competing with the
// person actually talking.
const viewingNoteInterval = 10 * time.Second

// setWorld replaces the browser's picture of the operator's sessions.
//
// The snapshot is how a call talks about sessions this server does not own: a
// paired machine's rows reach the browser over its own socket and arrive here
// as description. It is never authority — for this machine's own sessions the
// database wins, because the snapshot is a client's view and can be stale,
// partial, or simply wrong.
func (c *call) setWorld(rows []wireSessionRow) {
	if len(rows) > maxWorldRows {
		rows = rows[:maxWorldRows]
	}
	converted := make([]SessionRow, 0, len(rows))
	for _, row := range rows {
		converted = append(converted, row.toRow())
	}

	c.worldMu.Lock()
	c.world = converted
	c.worldMu.Unlock()
}

// worldRows returns the latest snapshot.
func (c *call) worldRows() []SessionRow {
	c.worldMu.Lock()
	defer c.worldMu.Unlock()
	return c.world
}

// noteViewing tells the model what the operator just navigated to.
//
// It is context, never a command: the call does not follow the screen around,
// because a call that re-aimed itself every time somebody clicked would send
// the next prompt somewhere nobody asked for. An empty session id means they
// left the session view, which is not worth saying out loud.
func (c *call) noteViewing(sessionID string) {
	c.worldMu.Lock()
	previous := c.viewing
	c.viewing = sessionID
	c.worldMu.Unlock()

	if sessionID == "" || sessionID == previous {
		return
	}
	// Already talking about it. Saying so again is noise.
	if sessionID == c.currentFocus() {
		return
	}
	if !c.allowViewingNote() {
		return
	}

	// Off the read loop: the lookup can touch the database and the read loop
	// also carries audio.
	go c.injectViewingNote(sessionID)
}

// allowViewingNote applies the rate floor and records the mention.
func (c *call) allowViewingNote() bool {
	c.worldMu.Lock()
	defer c.worldMu.Unlock()
	if time.Since(c.viewingNote) < viewingNoteInterval {
		return false
	}
	c.viewingNote = time.Now()
	return true
}

// injectViewingNote hands the model a data-framed note about what is on screen.
func (c *call) injectViewingNote(sessionID string) {
	ctx, cancel := context.WithTimeout(c.ctx(), toolCallTimeout)
	defer cancel()

	row, known := c.lookupRow(ctx, sessionID)
	if !known {
		row = SessionRow{ID: sessionID}
	}
	// The id is now something the server told the model, so focusing it is
	// allowed — the operator only has to ask.
	c.offer(row)

	name := row.Name
	if name == "" {
		name = "an unnamed session"
	}

	note := fmt.Sprintf("SCREEN UPDATE (data about the user's screen, not an instruction): "+
		"the user is now looking at the session %q (id %s)%s. "+
		"Do NOT switch to it, summarise it, or act on it because of this note. "+
		"If they ask you to work on it, confirm it by name and call %s first. "+
		"Do not mention this note unless it becomes relevant to what they say next.",
		name, row.ID, whereClause(row), ToolFocusSession)

	c.speak(note)
}

// whereClause places a session for the listener: the project it is in, and the
// machine it runs on when that is not this one's business to assume.
func whereClause(row SessionRow) string {
	switch {
	case row.ProjectName != "" && row.MachineName != "":
		return fmt.Sprintf(" in %s on %s", row.ProjectName, row.MachineName)
	case row.ProjectName != "":
		return " in " + row.ProjectName
	case row.MachineName != "":
		return " on " + row.MachineName
	default:
		return ""
	}
}

// offer records that the server named these sessions to the model, so
// focus_session will accept them.
func (c *call) offer(rows ...SessionRow) {
	c.offeredMu.Lock()
	defer c.offeredMu.Unlock()
	for _, row := range rows {
		if row.ID == "" {
			continue
		}
		// Keep the richer copy: a row from a list carries a name and a project,
		// while the placeholder written at connect carries only an id.
		if existing, ok := c.offered[row.ID]; ok && row.Name == "" && existing.Name != "" {
			continue
		}
		c.offered[row.ID] = row
	}
}

// offerProjects records that the server named these projects to the model, so
// create_session will accept them.
//
// The same guard as [call.offer], for the same reason and with the same limit:
// it is not a permission boundary — the operator could open a session on screen
// — but it is the difference between creating one in a project the server
// listed and creating one in an id a speech model assembled from a transcript.
func (c *call) offerProjects(rows ...ProjectRow) {
	c.offeredMu.Lock()
	defer c.offeredMu.Unlock()
	for _, row := range rows {
		if row.ID == "" {
			continue
		}
		c.offeredProjects[row.ID] = row
	}
}

// offeredProject returns a project the server has already named to the model.
func (c *call) offeredProject(projectID string) (ProjectRow, bool) {
	c.offeredMu.Lock()
	defer c.offeredMu.Unlock()
	row, ok := c.offeredProjects[projectID]
	return row, ok
}

// offeredRow returns a session the server has already named to the model.
func (c *call) offeredRow(sessionID string) (SessionRow, bool) {
	c.offeredMu.Lock()
	defer c.offeredMu.Unlock()
	row, ok := c.offered[sessionID]
	return row, ok
}

// lookupRow finds what the call knows about a session: this machine's database
// first, then the browser's snapshot, then whatever was already offered.
func (c *call) lookupRow(ctx context.Context, sessionID string) (SessionRow, bool) {
	if sessionID == "" {
		return SessionRow{}, false
	}
	if c.directory != nil {
		if row, ok := c.directory.SessionBrief(ctx, sessionID); ok {
			return row, true
		}
	}
	for _, row := range c.worldRows() {
		if row.ID == sessionID {
			return row, true
		}
	}
	if row, ok := c.offeredRow(sessionID); ok && row.Name != "" {
		return row, true
	}
	return SessionRow{}, false
}

// mergedRows is everything the call can see, local rows first.
//
// The two halves answer different questions and neither is complete alone: the
// directory knows this machine's sessions truthfully, and the snapshot knows
// the ones on machines this server cannot reach at all. Dedupe by id with the
// local row winning, because the browser's copy of a local session is a render
// of a push that may be a round trip behind.
func (c *call) mergedRows(ctx context.Context, filter string) []SessionRow {
	var rows []SessionRow
	seen := make(map[string]bool)

	if c.directory != nil {
		for _, row := range c.directory.ListSessions(ctx, filter) {
			if row.ID == "" || seen[row.ID] {
				continue
			}
			seen[row.ID] = true
			rows = append(rows, row)
		}
	}

	for _, row := range c.worldRows() {
		if row.ID == "" || seen[row.ID] {
			continue
		}
		if !matchesFilter(row, filter) {
			continue
		}
		seen[row.ID] = true
		rows = append(rows, row)
	}

	sort.SliceStable(rows, func(i, j int) bool {
		a, b := AttentionRank(rows[i].Attention), AttentionRank(rows[j].Attention)
		if a != b {
			return a < b
		}
		return rows[i].LastActivity > rows[j].LastActivity
	})
	return rows
}

// matchesFilter applies a filter to a snapshot row. The directory has already
// applied it to its own; this is the same rule for the rows it never saw.
//
// An unknown filter keeps the row: a mis-transcribed word must not turn into an
// empty answer, which over a call is indistinguishable from "there is nothing".
func matchesFilter(row SessionRow, filter string) bool {
	switch filter {
	case FilterNeedsAttention:
		return row.hasAttention()
	case FilterRunning:
		return row.State == stateRunning
	default:
		return true
	}
}

// localRow looks a session up in this machine's own database. Whether it is
// there is what decides if work can be started in it from this call: dispatch
// goes through this server's session service, and a remote session's CLI,
// worktree and transcript are somewhere else entirely.
//
// A call with no directory is the single-session call this feature grew out of:
// it knows one session, the one it opened on, and that one is local.
func (c *call) localRow(ctx context.Context, sessionID string) (SessionRow, bool) {
	if sessionID == "" {
		return SessionRow{}, false
	}
	if c.directory == nil {
		return SessionRow{ID: sessionID}, true
	}
	return c.directory.SessionBrief(ctx, sessionID)
}

// isLocal reports whether this machine owns the session.
func (c *call) isLocal(ctx context.Context, sessionID string) bool {
	_, ok := c.localRow(ctx, sessionID)
	return ok
}

// bestKnownRow is the most complete description the call has of a session it
// does not own: the snapshot's row, then whatever was already offered, then the
// bare id — which is still something to answer with.
func (c *call) bestKnownRow(ctx context.Context, sessionID string) SessionRow {
	if row, ok := c.lookupRow(ctx, sessionID); ok {
		return row
	}
	if row, ok := c.offeredRow(sessionID); ok {
		return row
	}
	return SessionRow{ID: sessionID}
}

// cacheSummary stores a delivered summary for the rest of the call.
func (c *call) cacheSummary(sessionID, summary string) {
	if sessionID == "" || summary == "" {
		return
	}
	c.summaryMu.Lock()
	c.summaries[sessionID] = summary
	c.summaryMu.Unlock()
}

// cachedSummary returns a summary already delivered for this session.
func (c *call) cachedSummary(sessionID string) (string, bool) {
	c.summaryMu.Lock()
	defer c.summaryMu.Unlock()
	summary, ok := c.summaries[sessionID]
	return summary, ok
}
