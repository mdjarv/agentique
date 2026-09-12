package assistant

import (
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Report budget. The prompt asks for two or three calls in a ten-minute run;
// this is the ceiling that catches a worker which ignores that, not the target.
//
// A bucket rather than a flat rate because reports cluster honestly: a run
// often finds two surprises in its first minute and then nothing for ten.
const (
	reportBurst      = 3
	reportRefillEach = 3 * time.Minute
)

// Follower receives news about the sessions it is following.
//
// Every delivery names its session. A follower may be bound to several, and
// which run just finished is the first thing its listener needs to know.
type Follower interface {
	// Notify delivers one agent-written report. It must not block: the caller
	// is an MCP tool handler with an agent waiting on the other end.
	Notify(sessionID string, r Report) error
	// NotifyRuntime delivers one runtime fact — the three things the agent
	// cannot report about itself.
	NotifyRuntime(sessionID string, n Notice) error
}

// Registry routes a working session's reports to whatever is following it.
//
// It outlives any one surface. A report that arrives between calls still has a
// home, because the assistant journals it whether or not anyone is live; this
// is only the live half.
//
// Reports flow the opposite way from everything else here: the
// worker decides what is worth saying and pushes it, rather than a watcher
// inferring salience from an event stream. The worker is the only party that
// knows it just found the tests were already broken, so the judgement lives
// where the knowledge is — and the inference layer that would otherwise be
// needed does not exist.
type Registry struct {
	mu        sync.Mutex
	followers map[string]map[Follower]struct{} // sessionID -> followers
	buckets   map[string]*bucket               // sessionID -> report budget
	now       func() time.Time
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		followers: make(map[string]map[Follower]struct{}),
		buckets:   make(map[string]*bucket),
		now:       time.Now,
	}
}

// Follow subscribes f to sessionID's reports and returns an unsubscribe func.
// Unsubscribing twice is safe.
func (r *Registry) Follow(sessionID string, f Follower) func() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.followers[sessionID] == nil {
		r.followers[sessionID] = make(map[Follower]struct{})
	}
	r.followers[sessionID][f] = struct{}{}

	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			delete(r.followers[sessionID], f)
			if len(r.followers[sessionID]) == 0 {
				delete(r.followers, sessionID)
				// Drop the budget with the last listener: a later call starts
				// fresh rather than inheriting a bucket the previous one spent.
				delete(r.buckets, sessionID)
			}
		})
	}
}

// Listening reports whether anyone is following sessionID.
func (r *Registry) Listening(sessionID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.followers[sessionID]) > 0
}

// Report delivers a worker's report to every call following sessionID and
// returns the message handed back to the worker.
//
// The three outcomes are all reported honestly rather than silently swallowed,
// because each one tells the worker something different about whether to keep
// calling: nobody is listening, you are going too fast, or it was delivered.
func (r *Registry) Report(sessionID, kind, headline string) (string, error) {
	report, err := ParseReport(kind, headline)
	if err != nil {
		return "", err
	}

	if !r.Listening(sessionID) {
		return "Nobody is on the call for this session, so that was not spoken. You can stop reporting for this run.", nil
	}
	if !r.Take(sessionID) {
		return "Reporting too often — that one was dropped. Save the next call for something that changes what the listener would do.", nil
	}
	return r.Deliver(sessionID, report)
}

// Take spends one report from sessionID's budget, answering false when it is
// empty.
//
// Separate from [Registry.Deliver] because the budget is not the delivery's:
// the assistant keeps a followed run's report in the journal whether or not
// anybody is live, and that durable write needs the same one ceiling. Two
// budgets would be two answers to "how often may this run report".
func (r *Registry) Take(sessionID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.takeToken(sessionID)
}

// Deliver hands a parsed report to every follower of sessionID and returns the
// message for the worker. It spends no budget: the caller has already taken one.
func (r *Registry) Deliver(sessionID string, report Report) (string, error) {
	r.mu.Lock()
	followers := make([]Follower, 0, len(r.followers[sessionID]))
	for f := range r.followers[sessionID] {
		followers = append(followers, f)
	}
	r.mu.Unlock()

	if len(followers) == 0 {
		return "Nobody is on the call for this session, so that was not spoken. You can stop reporting for this run.", nil
	}

	var failures int
	for _, f := range followers {
		if err := f.Notify(sessionID, report); err != nil {
			failures++
		}
	}
	if failures == len(followers) {
		return "", fmt.Errorf("no follower accepted the report")
	}
	return "Spoken to the listener.", nil
}

// Notice delivers a runtime fact to every call following sessionID.
//
// Unlike [Registry.Report] this is **not** rate limited and cannot be dropped.
// The budget exists to stop a chatty agent monopolising the listener's
// attention; these three are the events the listener is actually waiting for,
// and there is no version of "you are reporting too often" that should apply to
// "the run failed".
//
// It is also silent about having no audience: nothing is waiting on the answer,
// because the runtime is not an agent deciding whether to keep going.
func (r *Registry) Notice(sessionID string, notice Notice) {
	r.mu.Lock()
	followers := make([]Follower, 0, len(r.followers[sessionID]))
	for f := range r.followers[sessionID] {
		followers = append(followers, f)
	}
	r.mu.Unlock()

	for _, f := range followers {
		if err := f.NotifyRuntime(sessionID, notice); err != nil {
			slog.Warn("voice notice not delivered", "session", sessionID, "kind", notice.Kind, "error", err)
		}
	}
}

// takeToken spends one report from sessionID's budget. Caller holds r.mu.
func (r *Registry) takeToken(sessionID string) bool {
	b := r.buckets[sessionID]
	if b == nil {
		b = &bucket{tokens: reportBurst, last: r.now()}
		r.buckets[sessionID] = b
	}
	return b.take(r.now())
}

// bucket is a token bucket with whole-token refill.
type bucket struct {
	tokens int
	last   time.Time
}

func (b *bucket) take(now time.Time) bool {
	if refilled := int(now.Sub(b.last) / reportRefillEach); refilled > 0 {
		b.tokens = min(reportBurst, b.tokens+refilled)
		b.last = b.last.Add(time.Duration(refilled) * reportRefillEach)
	}
	if b.tokens <= 0 {
		return false
	}
	b.tokens--
	return true
}
