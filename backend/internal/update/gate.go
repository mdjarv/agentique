package update

import (
	"errors"
	"log/slog"
	"time"
)

// The drain gate (docs/upgrades.md).
//
// A restart is not a pause. On startup the server reaps orphaned CLI process
// groups, so restarting mid-turn does not suspend that turn — the new process
// comes up and kills it. Sessions survive; the current turn does not.
//
// So an upgrade asked for while the machine is busy is ARMED rather than run:
// a one-shot that fires the moment the last turn ends. Three properties make
// that safe to leave lying around:
//
//   - It carries a deadline. An upgrade armed on Tuesday must not fire on
//     Thursday because a lid closed at the wrong moment.
//   - It is in-memory only. If the server restarts for any other reason the
//     arming is forgotten — the fail-safe direction.
//   - It is cancellable for as long as it is armed.

// ErrAlreadyArmed guards against arming twice.
var ErrAlreadyArmed = errors.New("an upgrade is already waiting for this machine to go idle")

// ErrNotArmed is a disarm with nothing armed.
var ErrNotArmed = errors.New("no upgrade is waiting for idle")

// DefaultArmDeadline is how long an armed upgrade waits for idle before giving
// up and saying so.
const DefaultArmDeadline = 4 * time.Hour

// armCheckInterval enforces the deadline, which has no event of its own — an
// arming that waits out its window has to expire on a clock.
//
// It is not how the gate normally fires. The runtime's turn-end hook covers
// every way a turn stops (completed, died with the CLI, closed mid-flight), so
// this is a safety net rather than the path anything depends on.
const armCheckInterval = 30 * time.Second

// Arming is the public shape of an armed upgrade.
type Arming struct {
	// Target is the release it will install.
	Target string `json:"target"`
	// ArmedAt and DeadlineAt are RFC3339 UTC.
	ArmedAt    string `json:"armedAt"`
	DeadlineAt string `json:"deadlineAt"`
}

// armState is the gate's private half of an arming.
type armState struct {
	public   Arming
	deadline time.Time
}

// Arm holds an upgrade until the machine goes idle. Refused when the machine
// could not upgrade at all — no point arming something that can never fire.
func (a *Applier) Arm(expect string, deadline time.Duration) (Arming, error) {
	p, err := a.Preflight()
	if err != nil {
		return Arming{}, err
	}
	if expect != "" && expect != p.target {
		return Arming{}, ErrStale
	}
	if deadline <= 0 {
		deadline = a.armDeadline()
	}

	a.mu.Lock()
	if a.progress != nil && !a.progress.Phase.Terminal() {
		a.mu.Unlock()
		return Arming{}, ErrAlreadyRunning
	}
	if a.arm != nil {
		a.mu.Unlock()
		return Arming{}, ErrAlreadyArmed
	}
	now := time.Now().UTC()
	until := now.Add(deadline)
	a.arm = &armState{
		public: Arming{
			Target:     p.target,
			ArmedAt:    now.Format(time.RFC3339),
			DeadlineAt: until.Format(time.RFC3339),
		},
		deadline: until,
	}
	armed := a.arm.public
	a.mu.Unlock()

	a.startArmWatch()
	slog.Info("update: armed for the next idle boundary", "target", p.target, "deadline", until)

	// Arming on an already-idle machine should not wait for a turn that may
	// never come.
	a.tryFire()
	return armed, nil
}

// Disarm cancels an armed upgrade. Cancellable for as long as it is armed.
func (a *Applier) Disarm() error {
	a.mu.Lock()
	if a.arm == nil {
		a.mu.Unlock()
		return ErrNotArmed
	}
	a.arm = nil
	a.mu.Unlock()
	a.stopArmWatch()
	slog.Info("update: disarmed")
	return nil
}

// Arming returns the armed upgrade, or nil.
func (a *Applier) Arming() *Arming {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.arm == nil {
		return nil
	}
	armed := a.arm.public
	return &armed
}

// OnTurnEnd is the gate's dispatch point, wired to Manager.AddTurnEndListener.
// It runs on the event-loop goroutine, so it does no work beyond a flag read
// and a spawn.
func (a *Applier) OnTurnEnd(string) {
	a.mu.Lock()
	armed := a.arm != nil
	a.mu.Unlock()
	if !armed {
		return
	}
	go a.tryFire()
}

// tryFire installs if the machine is now idle, disarms if the deadline has
// passed, and otherwise keeps waiting.
func (a *Applier) tryFire() {
	a.mu.Lock()
	if a.arm == nil {
		a.mu.Unlock()
		return
	}
	expired := time.Now().After(a.arm.deadline)
	target := a.arm.public.Target
	a.mu.Unlock()

	if expired {
		a.expire(target)
		return
	}
	// Busy is the turn registry's answer, read fresh — by the time a turn-end
	// listener runs, that turn is closed, so a lone remaining turn here is a
	// real one.
	if len(a.Busy()) > 0 {
		return
	}

	// Claim the arming before starting, so two racing fires cannot both go.
	a.mu.Lock()
	if a.arm == nil {
		a.mu.Unlock()
		return
	}
	a.arm = nil
	a.mu.Unlock()
	a.stopArmWatch()

	slog.Info("update: machine went idle, applying armed upgrade", "target", target)
	if err := a.Start(target, false); err != nil {
		// Losing the race back to busy is not a failure — re-arm rather than
		// dropping an upgrade the operator asked for.
		if errors.Is(err, ErrBusy) {
			slog.Info("update: re-arming, machine went busy again", "target", target)
			if _, aerr := a.Arm(target, 0); aerr != nil {
				slog.Warn("update: could not re-arm", "error", aerr)
			}
			return
		}
		slog.Error("update: armed upgrade failed to start", "target", target, "error", err)
		a.recordArmFailure(target, err)
	}
}

// expire drops an arming that waited too long, and says so rather than going
// quiet.
func (a *Applier) expire(target string) {
	a.mu.Lock()
	if a.arm == nil {
		a.mu.Unlock()
		return
	}
	a.arm = nil
	a.mu.Unlock()
	a.stopArmWatch()
	slog.Warn("update: armed upgrade expired without the machine going idle", "target", target)
	a.publishTerminal(target, "the machine never went idle before the deadline — upgrade when you can")
}

func (a *Applier) recordArmFailure(target string, err error) {
	a.publishTerminal(target, err.Error())
}

// publishTerminal reports a gate outcome through the same progress channel the
// upgrade itself uses, so a client watching one place hears about both.
func (a *Applier) publishTerminal(target, message string) {
	now := nowStamp()
	p := Progress{
		MachineID: a.deps.MachineID,
		Phase:     PhaseFailed,
		Target:    target,
		From:      a.checker.Version(),
		Error:     message,
		StartedAt: now,
		UpdatedAt: now,
	}
	a.mu.Lock()
	a.progress = &p
	a.mu.Unlock()
	a.emit(p)
}

// --- the backstop ticker ---

func (a *Applier) armDeadline() time.Duration {
	if a.deps.ArmDeadline > 0 {
		return a.deps.ArmDeadline
	}
	return DefaultArmDeadline
}

func (a *Applier) startArmWatch() {
	a.watchMu.Lock()
	defer a.watchMu.Unlock()
	if a.watchStop != nil {
		return
	}
	stop := make(chan struct{})
	a.watchStop = stop
	a.watchWG.Add(1)
	go func() {
		defer a.watchWG.Done()
		t := time.NewTicker(armCheckInterval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				a.tryFire()
			}
		}
	}()
}

func (a *Applier) stopArmWatch() {
	a.watchMu.Lock()
	stop := a.watchStop
	a.watchStop = nil
	a.watchMu.Unlock()
	if stop != nil {
		close(stop)
	}
}

// StopArmWatch parks the backstop goroutine. Called from server shutdown.
func (a *Applier) StopArmWatch() {
	a.stopArmWatch()
	a.watchWG.Wait()
}
