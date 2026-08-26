package voice

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// Control message types. Audio never appears here — it rides binary frames.
const (
	// msgReady announces the call is open and carries the sample rates. The
	// client reads its playback rate from this rather than assuming one,
	// because the echo engine and a speech model return audio at different
	// rates and the same client code plays both.
	msgReady = "ready"
	// msgTurnComplete tells the client to flush its playback queue, whether the
	// engine finished or was interrupted.
	msgTurnComplete = "turn_complete"
	// msgTranscript carries recognised text for display and logging.
	msgTranscript = "transcript"
	// msgError reports a non-fatal problem in terms a person can read.
	msgError = "error"
	// msgClosed explains why the server is ending the call.
	msgClosed = "closed"
	// msgReport carries an agent-written progress report from the session being
	// followed.
	msgReport = "report"
	// msgDispatched carries the prompt the voice agent just handed to the
	// session, so there is always a visible record of what was sent.
	msgDispatched = "dispatched"
	// msgNotice carries a runtime fact about the followed session — finished,
	// failed, or blocked. Distinct from msgReport because the source differs
	// and so does the trust: a notice is the server's own words.
	msgNotice = "notice"
	// msgFocus asks the browser to navigate to a session, because the operator
	// asked for it out loud. The call's focus moved first; this is the screen
	// catching up with the conversation.
	msgFocus = "focus"

	// msgStop is the client asking to end the call.
	msgStop = "stop"
	// msgFollow adds a session to the call's follow set, so that session's
	// reports reach it.
	msgFollow = "follow"
	// msgUnfollow releases one session's binding, or all of them when it
	// carries no session id.
	msgUnfollow = "unfollow"
	// msgWorld is the browser's picture of every session the operator can see,
	// including ones on machines this server cannot reach. It is a VIEW, never
	// authority: this machine's own database wins for its own sessions.
	msgWorld = "world"
	// msgViewing says the operator navigated to a session on screen. It is
	// context for the conversation, never a command — a call never follows the
	// screen around by itself.
	msgViewing = "viewing"
)

// maxWorldRows bounds one world snapshot.
//
// Generous: an operator with two hundred live sessions is unusual but not
// wrong, and the cap exists to bound memory on a socket anyone authenticated
// can open, not to second-guess how they work.
const maxWorldRows = 200

// maxWorldField bounds each string in a snapshot row. Long enough for any real
// session or project name, short enough that a hostile client cannot use the
// snapshot as a channel into the speech model's context.
const maxWorldField = 200

// serverMessage is a JSON control frame sent to the browser.
//
// Every field past Type is optional, deliberately. The generated client schema
// mirrors these tags, so dropping omitempty makes a field required and any peer
// that does not send it has its whole payload rejected. An absent field means
// "not set".
type serverMessage struct {
	Type string `json:"type"`

	// ready
	InputSampleRate  int `json:"inputSampleRate,omitempty"`
	OutputSampleRate int `json:"outputSampleRate,omitempty"`

	// turn_complete
	Interrupted bool `json:"interrupted,omitempty"`

	// transcript
	Text   string `json:"text,omitempty"`
	Final  bool   `json:"final,omitempty"`
	Source string `json:"source,omitempty"`

	// error
	Message string `json:"message,omitempty"`

	// closed
	Reason string `json:"reason,omitempty"`

	// report
	Kind      string `json:"kind,omitempty"`
	Headline  string `json:"headline,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
}

// clientMessage is a JSON control frame from the browser.
type clientMessage struct {
	Type string `json:"type"`

	// follow, unfollow, viewing
	SessionID string `json:"sessionId,omitempty"`

	// world
	Sessions []wireSessionRow `json:"sessions,omitempty"`
}

// wireSessionRow is one session in the browser's world snapshot.
//
// Every field is optional, per the wire rule: a peer that omits one means "not
// set", and a required field would let one older client's payload be rejected
// whole. Nothing here is trusted — it is the client's picture of the world,
// used to talk about sessions this server cannot see.
type wireSessionRow struct {
	SessionID      string `json:"sessionId,omitempty"`
	Name           string `json:"name,omitempty"`
	ProjectSlug    string `json:"projectSlug,omitempty"`
	ProjectName    string `json:"projectName,omitempty"`
	MachineID      string `json:"machineId,omitempty"`
	MachineName    string `json:"machineName,omitempty"`
	State          string `json:"state,omitempty"`
	Attention      string `json:"attention,omitempty"`
	Branch         string `json:"branch,omitempty"`
	LastActivityAt string `json:"lastActivityAt,omitempty"`
}

// toRow clamps one snapshot row into the shape the rest of the package uses.
func (w wireSessionRow) toRow() SessionRow {
	return SessionRow{
		ID:           clampField(w.SessionID),
		Name:         clampField(w.Name),
		ProjectSlug:  clampField(w.ProjectSlug),
		ProjectName:  clampField(w.ProjectName),
		MachineID:    clampField(w.MachineID),
		MachineName:  clampField(w.MachineName),
		State:        clampField(w.State),
		Attention:    clampField(w.Attention),
		Branch:       clampField(w.Branch),
		LastActivity: clampField(w.LastActivityAt),
	}
}

// clampField bounds one snapshot string on a rune boundary.
func clampField(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= maxWorldField {
		return s
	}
	return string([]rune(s)[:maxWorldField])
}

// handleControl processes one client control frame and reports whether the call
// should end.
func (c *call) handleControl(payload []byte) (stop bool) {
	var msg clientMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		c.log.Warn("voice control decode failed", "error", err)
		return false
	}

	switch msg.Type {
	case msgStop:
		c.log.Info("voice call stopped by client")
		return true

	case msgFollow:
		if msg.SessionID == "" {
			c.log.Warn("voice follow without a session id")
			return false
		}
		c.follow(msg.SessionID, "")
		return false

	case msgUnfollow:
		// No session id means "stop following everything" — which is what an
		// older client, written when a call followed exactly one session, means
		// by the frame it sends.
		if msg.SessionID == "" {
			c.unfollowAll()
			return false
		}
		c.unfollow(msg.SessionID)
		return false

	case msgWorld:
		c.setWorld(msg.Sessions)
		return false

	case msgViewing:
		c.noteViewing(msg.SessionID)
		return false

	default:
		// Forward compatibility: a newer client may send a control type this
		// build does not know. Ignoring it is correct; closing the call is not.
		c.log.Debug("voice unknown control type", "type", msg.Type)
		return false
	}
}
