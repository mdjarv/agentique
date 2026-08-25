package voice

import "encoding/json"

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

	// msgStop is the client asking to end the call.
	msgStop = "stop"
)

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
}

// clientMessage is a JSON control frame from the browser.
type clientMessage struct {
	Type string `json:"type"`
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
	default:
		// Forward compatibility: a newer client may send a control type this
		// build does not know. Ignoring it is correct; closing the call is not.
		c.log.Debug("voice unknown control type", "type", msg.Type)
		return false
	}
}
