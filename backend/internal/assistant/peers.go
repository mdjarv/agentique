package assistant

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mdjarv/agentique/backend/internal/store"
)

// Locator is implemented by a [Directory] that can find a session on any
// machine this server can reach, not only its own (docs/peers.md).
//
// Locate answers the freshest row the directory has, with its [Reach], and is
// the lookup every verb that ACTS uses. [Directory.SessionBrief] stays the test
// for "this machine's own", which is a different question: a journal subject, a
// memory scope and a local runtime hook each need that one.
type Locator interface {
	Locate(ctx context.Context, sessionID string) (SessionRow, bool)
}

// RemoteFollower is implemented by a [Directory] that can subscribe this
// server to a paired machine's news about one of its sessions.
type RemoteFollower interface {
	FollowRemote(ctx context.Context, machineID, sessionID string) error
}

// PeerSessionStates is implemented by a [Directory] that can say whether
// sessions on paired machines are still unfinished — the half of a policy's
// in-flight budget this server's own sessions table cannot see.
type PeerSessionStates interface {
	// Unfinished reports, for each session, whether its machine says it is
	// unfinished (not archived, not done, not failed), and whether the machine
	// answered at all. A session its machine did not answer for is unknown.
	Unfinished(ctx context.Context, machineID, sessionID string) (unfinished, known bool)
}

// RefusedError is an owner's guard, or this server's own routing, saying no
// to an action, with a sentence written to be relayed.
//
// A collaborator returns it instead of a bare error so the head says the actual
// reason — "zbook does not accept work from other machines" — rather than a
// generic failure, which invites a retry that can only fail the same way.
type RefusedError struct {
	Reason string
	Say    string
}

func (e *RefusedError) Error() string { return e.Reason + ": " + e.Say }

// refusedSay returns the relayable sentence of a [RefusedError] in err, if any.
func refusedSay(err error) (string, string, bool) {
	var refused *RefusedError
	if errors.As(err, &refused) {
		return refused.Reason, refused.Say, true
	}
	return "", "", false
}

// locate finds a session wherever it runs: the directory's own lookup where it
// has one, else the lists.
func (s *Service) locate(ctx context.Context, sessionID string) (SessionRow, bool) {
	if s.dir == nil || sessionID == "" {
		return SessionRow{}, false
	}
	if loc, ok := s.dir.(Locator); ok {
		return loc.Locate(ctx, sessionID)
	}
	if row, local := s.dir.SessionBrief(ctx, sessionID); local {
		if row.Reach == ReachView {
			row.Reach = ReachLocal
		}
		return row, true
	}
	for _, row := range s.dir.ListSessions(ctx, FilterAll) {
		if row.ID == sessionID {
			return row, true
		}
	}
	return SessionRow{}, false
}

// cannotAct is the refusal for a row whose reach does not allow the action,
// naming the machine and the reason in words.
func cannotAct(reasonPrefix string, row SessionRow, what string) map[string]any {
	machine := machineOf(row)
	switch row.Reach {
	case ReachPeerOff:
		return refuse(reasonPrefix+"-peer-off", fmt.Sprintf("NOTHING WAS DONE: %s runs on %s, which does not "+
			"accept %s from other machines. Say so; turning it on is `[peer] accept-actions` in that "+
			"machine's own config.", DisplayFor(row), machine, what))
	case ReachPeerOld:
		return refuse(reasonPrefix+"-peer-old", fmt.Sprintf("NOTHING WAS DONE: %s runs on %s, whose release is "+
			"too old to take %s from another machine. Say so; it needs updating first.",
			DisplayFor(row), machine, what))
	default:
		return refuse(reasonPrefix+"-not-local", fmt.Sprintf("NOTHING WAS DONE: %s runs on %s, and this server "+
			"cannot reach it to do that. Say which machine it is on.", DisplayFor(row), machine))
	}
}

// FollowRow puts a session on the watch list wherever it runs.
//
// A paired machine's session goes on the remote watch list, and that machine is
// asked to send its news here; the remote half failing is logged, never fatal,
// because the local row is what makes a report that does arrive count.
func (s *Service) FollowRow(ctx context.Context, row SessionRow, source string) error {
	if !row.Reach.Remote() {
		return s.Follow(ctx, row.ID, source)
	}
	if strings.TrimSpace(row.ID) == "" || row.MachineID == "" {
		return errors.New("assistant: no remote session to follow")
	}
	if err := s.store.UpsertAssistantPeerFollow(ctx, store.UpsertAssistantPeerFollowParams{
		MachineID: row.MachineID, SessionID: row.ID, Since: formatTime(s.now()), Source: source,
	}); err != nil {
		return fmt.Errorf("follow %s on %s: %w", row.ID, row.MachineID, err)
	}
	if follower, ok := s.dir.(RemoteFollower); ok {
		if err := follower.FollowRemote(ctx, row.MachineID, row.ID); err != nil {
			s.log.Warn("assistant: remote follow not recorded on the owner", "session", row.ID,
				"machine", row.MachineID, "error", err)
		}
	}
	return nil
}

// followingRemote reports whether a paired machine's session is on the watch
// list.
func (s *Service) followingRemote(ctx context.Context, sessionID string) bool {
	follows, err := s.store.ListAssistantPeerFollows(ctx)
	if err != nil {
		s.log.Warn("assistant remote follow list unreadable", "error", err)
		return false
	}
	for _, f := range follows {
		if f.SessionID == sessionID {
			return true
		}
	}
	return false
}

// IngestPeerReport files a report a paired machine's session wrote, as that
// machine relayed it (docs/peers.md, the event feed).
//
// The owner already enforced the report budget; this side applies the watch
// list, the same gate [Service.Report] applies locally: a report about a
// session nobody here follows is not news here, and it is dropped without a
// journal row. What is kept is marked untrusted, because it is agent-written
// text about a repository nobody here authored.
func (s *Service) IngestPeerReport(ctx context.Context, machineName, sessionID string, report Report) {
	listening := s.reg.Listening(sessionID)
	if !listening && !s.Following(ctx, sessionID) {
		return
	}
	payload := map[string]any{"kind": string(report.Kind)}
	if machineName != "" {
		payload["machine"] = machineName
	}
	if _, err := s.appendJournal(ctx, journalWrite{
		Kind:      JournalReport,
		SessionID: sessionID,
		Summary:   report.Headline,
		Payload:   payload,
		Untrusted: true,
	}); err != nil {
		s.log.Warn("assistant: peer report not journaled", "session", sessionID, "error", err)
	}
	s.deliver(ctx, Item{Kind: ItemReport, SessionID: sessionID, Report: &report})
	if listening {
		if _, err := s.reg.Deliver(sessionID, report); err != nil {
			s.log.Warn("assistant: peer report not delivered", "session", sessionID, "error", err)
		}
	}
}

// IngestPeerTurnEnd records how a paired machine's session's turn ended —
// finished, failed, or stopped on something only a person can answer — for a
// session this server follows, exactly as a local turn end is recorded.
func (s *Service) IngestPeerTurnEnd(ctx context.Context, machineName, sessionID, name string, notice Notice) {
	if !s.reg.Listening(sessionID) && !s.Following(ctx, sessionID) {
		return
	}
	var extra map[string]any
	if machineName != "" {
		extra = map[string]any{"machine": machineName}
		if name != "" {
			name = name + " on " + machineName
		}
	}
	// No project: a remote project id means nothing here, and it is what a
	// memory scope is filed under.
	s.recordNoticeWith(ctx, sessionID, "", notice, journalKindFor(notice.Kind), name, extra)
}
