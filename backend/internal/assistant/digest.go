package assistant

import (
	"context"
	"fmt"
	"strings"

	"github.com/mdjarv/agentique/backend/internal/store"
)

// The digest.
//
// The notifier's job, and **deterministic**: no model runs. The journal since
// the last digest, grouped by the Needs-you ranking
// (lib/session/priority.ts is the client's copy of the same order the notices
// use), rendered as one conversation message. A one-line narration from the
// head is optional, off, and not built here.
//
// It is a message in the conversation rather than a surface of its own, so it
// is readable from every surface that renders the thread, and the next call's
// greeting reads it as conversation the way it reads everything else.

const (
	// maxDigestEntries bounds the journal read behind one digest. Past this the
	// digest has stopped being a digest, and the journal itself is the surface
	// for the rest.
	maxDigestEntries = 200
	// maxDigestLines bounds one group. A digest is read in one sitting.
	maxDigestLines = 12
	// messageKindDigest is the metadata mark that tells a surface this message
	// is a digest rather than a turn in the conversation.
	messageKindDigest = "digest"
	// digestEmptyText is the one sentence an empty digest is.
	digestEmptyText = "Nothing has happened since the last digest."
)

// Digest composes the digest, posts it as the assistant's own message, and
// stamps the mark it measured from.
//
// The window is INCLUSIVE at its lower boundary, so an entry written in the
// same second as the previous stamp appears in two digests. That is the
// direction the surface marks already chose and for the stronger reason: a
// repeated line is a nuisance where a dropped one is news the operator never
// reads.
//
// The stamp moves whether or not there was anything to say: a digest that found
// nothing still answered the question, and leaving the mark where it was would
// make the next one repeat a window nobody has anything to read in.
func (s *Service) Digest(ctx context.Context) (Message, error) {
	since := s.lastDigestAt(ctx)

	entries, err := s.Journal(ctx, since, maxDigestEntries)
	if err != nil {
		return Message{}, err
	}
	// Open proposals are not "what happened" — they are what is still owed a
	// decision, whenever they were made — so they come from the table rather
	// than from the window.
	open, err := s.OpenProposals(ctx)
	if err != nil {
		s.log.Warn("assistant: digest could not read proposals", "error", err)
	}

	text := s.composeDigest(ctx, since, entries, open)
	at := formatTime(s.now())
	msg, err := s.appendMessage(ctx, senderPersona, "", "", messageKindDigest, text)
	if err != nil {
		return Message{}, err
	}
	// Stamped after the message is stored, not before: a digest nobody can read
	// must not consume the window it was about. The same rule the head follows
	// for its news.
	if err := s.store.SetAssistantDigestAt(ctx, store.SetAssistantDigestAtParams{
		LastDigestAt: at,
		Now:          at,
	}); err != nil {
		s.log.Warn("assistant: digest mark not stamped", "error", err)
	}

	s.deliver(ctx, Item{Kind: ItemMessage, Message: &msg})
	return msg, nil
}

// lastDigestAt reads the mark the last digest left. No state row yet is the
// ordinary first-boot case: everything is news.
func (s *Service) lastDigestAt(ctx context.Context) string {
	state, err := s.store.GetAssistantState(ctx)
	if err != nil {
		return ""
	}
	return state.LastDigestAt
}

// composeDigest renders the message.
//
// The group order is the Needs-you ranking and then the rest: what holds a
// process, what broke, what is owed a decision, then outcomes, then what
// sessions said. Reports come last of the named groups and are QUOTED, because
// they are agent-written text about content nobody here authored.
//
// One kind is in no group on purpose: [JournalHeartbeat] is the assistant's own
// bookkeeping, and a digest listing every tick that decided to do nothing is a
// digest of itself.
func (s *Service) composeDigest(ctx context.Context, since string, entries []JournalEntry, open []Proposal) string {
	var b strings.Builder
	b.WriteString("**Digest**")
	if since != "" {
		fmt.Fprintf(&b, " -- since %s", since)
	}
	b.WriteString("\n")

	groups := []struct {
		heading string
		kinds   []JournalKind
	}{
		{"Waiting on you", []JournalKind{JournalSessionBlocked}},
		{"Failed", []JournalKind{JournalSessionFailed, JournalLoopPaused}},
		{"Machines", []JournalKind{JournalFinding}},
	}
	wrote := false
	for _, group := range groups {
		if section := s.digestSection(ctx, group.heading, entries, group.kinds); section != "" {
			b.WriteString(section)
			wrote = true
		}
	}
	if section := s.digestProposals(open); section != "" {
		b.WriteString(section)
		wrote = true
	}
	for _, group := range []struct {
		heading string
		kinds   []JournalKind
	}{
		{"Finished", []JournalKind{JournalSessionFinished}},
		{"Merged and archived", []JournalKind{JournalSessionMerged, JournalSessionArchived}},
		{"Reported", []JournalKind{JournalReport}},
		{"Also", []JournalKind{
			JournalDispatched, JournalSessionCreated, JournalNote,
			JournalProposalMade, JournalProposalDecided, JournalDaySummary,
			JournalCompaction,
		}},
	} {
		if section := s.digestSection(ctx, group.heading, entries, group.kinds); section != "" {
			b.WriteString(section)
			wrote = true
		}
	}

	if !wrote {
		return digestEmptyText
	}
	return strings.TrimRight(b.String(), "\n")
}

// digestSection renders one group, oldest first, or "" when it has nothing.
func (s *Service) digestSection(ctx context.Context, heading string, entries []JournalEntry, kinds []JournalKind) string {
	want := make(map[JournalKind]bool, len(kinds))
	for _, kind := range kinds {
		want[kind] = true
	}

	// Journal() answers newest first; a digest reads as a sequence.
	lines := make([]string, 0, maxDigestLines)
	omitted := 0
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if !want[entry.Kind] {
			continue
		}
		if len(lines) >= maxDigestLines {
			omitted++
			continue
		}
		lines = append(lines, s.digestLine(ctx, entry))
	}
	if len(lines) == 0 {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\n**%s**\n", heading)
	for _, line := range lines {
		b.WriteString("- ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	if omitted > 0 {
		fmt.Fprintf(&b, "- and %d more\n", omitted)
	}
	return b.String()
}

// digestLine is one entry: the session named in its project, then what
// happened. Untrusted text is quoted and said to be quoted, in the line itself
// rather than in a footnote — the mark has to survive being read out of order.
func (s *Service) digestLine(ctx context.Context, entry JournalEntry) string {
	subject := s.digestSubject(ctx, entry)
	what := digestVerb(entry.Kind)

	line := what
	if subject != "" {
		line = subject + " " + what
	}
	if entry.Summary == "" {
		return line
	}
	if entry.Untrusted {
		return fmt.Sprintf("%s, quoting it: %q", line, entry.Summary)
	}
	if what == "" {
		return strings.TrimSpace(line + " " + entry.Summary)
	}
	return fmt.Sprintf("%s -- %s", line, entry.Summary)
}

// digestSubject names the session an entry is about, the way every other
// surface names one: its own name and the project it is in.
//
// The directory first, because it is current: a session renamed since the entry
// was written is named by what it is called now. The payload's recorded name
// second, which is what survives the session being deleted, and the short id
// last — which is what the rest of this app calls a session it cannot name.
func (s *Service) digestSubject(ctx context.Context, entry JournalEntry) string {
	if entry.SessionID == "" {
		return ""
	}
	if s.dir != nil {
		if row, local := s.dir.SessionBrief(ctx, entry.SessionID); local && row.Name != "" {
			return DisplayFor(row)
		}
	}
	if name, ok := entry.Payload["name"].(string); ok && name != "" {
		return name
	}
	if len(entry.SessionID) > 8 {
		return entry.SessionID[:8]
	}
	return entry.SessionID
}

// digestVerb is what a kind did, in the digest's own words.
func digestVerb(kind JournalKind) string {
	switch kind {
	case JournalSessionFinished:
		return "finished"
	case JournalSessionFailed:
		return "failed"
	case JournalSessionBlocked:
		return "is stuck waiting on you"
	case JournalSessionMerged:
		return "was merged"
	case JournalSessionArchived:
		return "was archived"
	case JournalSessionCreated:
		return "was created"
	case JournalDispatched:
		return "was sent a prompt"
	case JournalLoopPaused:
		return "has a paused loop"
	case JournalReport:
		return "reported"
	case JournalNote, JournalDaySummary, JournalProposalMade, JournalProposalDecided,
		JournalHeartbeat, JournalCompaction, JournalFinding:
		// The summary is the whole entry; a verb in front of it would be the
		// sentence twice.
		return ""
	default:
		return string(kind)
	}
}

// digestProposals is the one group that is not a window: an open proposal is
// owed a decision whenever it was made.
func (s *Service) digestProposals(open []Proposal) string {
	if len(open) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n**Waiting for a yes**\n")
	for i, p := range open {
		if i >= maxDigestLines {
			fmt.Fprintf(&b, "- and %d more\n", len(open)-maxDigestLines)
			break
		}
		line := proposalSubjectLine(p)
		if p.Rationale != "" {
			line += " -- " + p.Rationale
		}
		b.WriteString("- ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// verbDigest is the head's own way to post one.
//
// Contained rather than read: it writes a message into the conversation, which
// is a write everybody can see, and it is the operator's own ask that puts it
// there.
func (s *Service) verbDigest(ctx context.Context, _ map[string]any) (map[string]any, error) {
	msg, err := s.Digest(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"posted": true,
		"digest": msg.Text,
		"note": "It is already in the conversation in front of them, so do not repeat it. Say one " +
			"line about what stands out in it, or nothing at all if it is empty.",
	}, nil
}
