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
	PhaseReplacing   Phase = "replacing"
	PhaseRestarting  Phase = "restarting"
	PhaseFailed      Phase = "failed"
	PhaseCancelled   Phase = "cancelled"
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
	case PhaseQueued, PhaseDownloading, PhaseVerifying:
		return true
	default:
		return false
	}
}

// Terminal reports whether the upgrade is over, one way or another.
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
	// Target is the tag being installed.
	Target string `json:"target"`
	// From is the version being replaced.
	From string `json:"from"`
	// Downloaded/Total are bytes, and belong only to `downloading` — the one
	// phase where "is it hung?" arises.
	Downloaded int64 `json:"downloaded"`
	Total      int64 `json:"total"`
	// Cancellable mirrors Phase.Cancellable so a client need not re-derive it.
	Cancellable bool `json:"cancellable"`
	// Error is set on PhaseFailed.
	Error     string `json:"error,omitempty"`
	StartedAt string `json:"startedAt"`
	UpdatedAt string `json:"updatedAt"`
}

func nowStamp() string { return time.Now().UTC().Format(time.RFC3339) }
