package update

import "time"

// Phase is where an in-flight upgrade has got to. The client renders each one;
// an unnarrated 30-second binary swap is the version nobody trusts twice.
//
//	queued → downloading (bytes/total) → verifying → replacing → restarting
//	                                                   │
//	                         ─────── socket drops here ┘
//
// After `restarting` nobody is left to report: the process serving the reply
// is the process being replaced. The client treats the drop as expected and
// confirms by re-reading the version.
type Phase string

const (
	PhaseQueued      Phase = "queued"
	PhaseDownloading Phase = "downloading"
	PhaseVerifying   Phase = "verifying"
	// PhaseBuilding is the source channel's long phase: a compile in the local
	// checkout, in place of a download. It has no total to count against, so it
	// narrates with a log tail instead of a byte counter.
	PhaseBuilding Phase = "building"
	// PhaseWaitingIdle is reached only from the source channel. A build takes
	// minutes, so a turn can quite reasonably open while the compiler runs — and
	// the binary is then built but must not be restarted into. This is not
	// terminal: the gate fires it the moment the machine goes idle.
	PhaseWaitingIdle Phase = "waiting-idle"
	PhaseReplacing   Phase = "replacing"
	PhaseRestarting  Phase = "restarting"
	PhaseFailed      Phase = "failed"
	PhaseCancelled   Phase = "cancelled"
)

// Kind is which channel an upgrade came from. Absent on the wire means
// "release", so a client that predates the source channel reads every release
// upgrade exactly as it always did.
type Kind string

const (
	// KindRelease downloads a published asset and verifies its checksum.
	KindRelease Kind = "release"
	// KindSource compiles the local checkout.
	KindSource Kind = "source"
	// KindRestart installs nothing. It is for a binary already sitting at the
	// install path, newer than the process serving you — what `just install`
	// leaves behind, and what a failed restart leaves behind. It still goes
	// through the drain gate, because a restart is still not a pause.
	KindRestart Kind = "restart"
)

// Cancellable reports whether a cancel can still land in this phase.
//
// Up to and including verifying, nothing is installed — the temp file is just
// deleted. `replacing` is a single rename(2), over before a cancel could
// arrive, and after it "cancel" would mean rollback, which is a different and
// deliberate command. The UI hides the button rather than leaving one that
// quietly stops working.
func (p Phase) Cancellable() bool {
	switch p {
	case PhaseQueued, PhaseDownloading, PhaseVerifying, PhaseBuilding, PhaseWaitingIdle:
		return true
	default:
		return false
	}
}

// Terminal reports whether the upgrade is over, one way or another.
//
// `waiting-idle` is deliberately NOT terminal: the binary is built and the
// upgrade is still going to happen, it is only waiting for the turn that opened
// underneath it to end.
func (p Phase) Terminal() bool {
	switch p {
	case PhaseFailed, PhaseCancelled, PhaseRestarting:
		return true
	default:
		return false
	}
}

// Progress is the live state of one upgrade. It is BOTH published on the WS
// global topic and held as server state: events alone strand anyone who
// reloads mid-upgrade or opens a second client, and state alone makes the bar
// lurch on a poll interval. Both; first to arrive wins.
type Progress struct {
	// MachineID identifies which machine this is about — WS subscriptions fan
	// in from every machine's socket, so the payload has to say.
	MachineID string `json:"machineId"`
	Phase     Phase  `json:"phase"`
	// Kind is the channel this upgrade came from. Omitted for a release, so an
	// older client reads the payload exactly as it always did.
	Kind Kind `json:"kind,omitempty"`
	// Target is the tag being installed — or, on the source channel, the commit.
	Target string `json:"target"`
	// From is the version being replaced.
	From string `json:"from"`
	// Downloaded/Total are bytes, and belong only to `downloading` — the one
	// phase where "is it hung?" arises.
	Downloaded int64 `json:"downloaded"`
	Total      int64 `json:"total"`
	// Log is the tail of the build's output, and belongs only to `building` —
	// the source channel's answer to "is it hung?", where the release channel
	// has a byte counter.
	Log []string `json:"log,omitempty"`
	// Cancellable mirrors Phase.Cancellable so a client need not re-derive it.
	Cancellable bool `json:"cancellable"`
	// Error is set on PhaseFailed.
	Error     string `json:"error,omitempty"`
	StartedAt string `json:"startedAt"`
	UpdatedAt string `json:"updatedAt"`
}

func nowStamp() string { return time.Now().UTC().Format(time.RFC3339) }
