package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/mdjarv/agentique/backend/internal/assistant"
	"github.com/mdjarv/agentique/backend/internal/peer"
	"github.com/mdjarv/agentique/backend/internal/peerlink"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// The poller's clocks. A poll is held open for pollWait when a machine has
// nothing to say, and returns the moment it does, so news is immediate and an
// idle machine costs one request (plus its identity proof) per wait.
const (
	peerPollWait      = 25 * time.Second
	peerPollRescan    = time.Minute
	peerPollBackoff   = 15 * time.Second
	peerPollMaxBack   = 5 * time.Minute
	peerPollNoSurface = 10 * time.Minute
	// peerMaxHeadline bounds a relayed turn-end headline; a report's is bounded
	// by assistant.ParseReport.
	peerMaxHeadline = 600
)

// peerEventReader is the poll and the cursor it advances.
type peerEventReader interface {
	Events(ctx context.Context, machineID string, since int64, wait time.Duration) (peer.EventsResponse, error)
}

// peerEventSink is where a paired machine's news lands on this server: the
// assistant when it is on, the live-call registry when only voice is.
type peerEventSink interface {
	PeerReport(ctx context.Context, machineName, sessionID string, report assistant.Report)
	PeerTurnEnd(ctx context.Context, machineName, sessionID, name string, notice assistant.Notice)
	PeerFinding(ctx context.Context, finding assistant.Finding)
}

// peerPollerCatalog is the machine rows the poller reads and the cursor it
// writes.
type peerPollerCatalog interface {
	ListMachines(ctx context.Context) ([]store.Machine, error)
	GetMachine(ctx context.Context, machineID string) (store.Machine, error)
	SetMachinePeerCursor(ctx context.Context, arg store.SetMachinePeerCursorParams) (int64, error)
}

// peerPoller reads every paired machine's outbox for this server and applies
// it: the other half of the event feed (docs/peers.md).
//
// One goroutine per machine, started and stopped as the catalog changes, never
// replaced while it runs — the rule the browser's per-machine clients follow.
// The cursor is durable (machines.peer_cursor) and advanced after each event is
// applied, so a restart resumes where it stopped and a replay is at worst one
// event applied twice. A machine read for the first time starts from now: a
// week of news about runs nobody here dispatched is not news.
type peerPoller struct {
	catalog peerPollerCatalog
	events  peerEventReader
	sink    peerEventSink
	selfID  string
	// onTurnEnd runs after a paired machine's turn end is applied, for the
	// caches that turn just made stale.
	onTurnEnd func(machineID, sessionID string)

	wait, rescan, backoff, maxBackoff, noSurface time.Duration
	sleep                                        func(ctx context.Context, d time.Duration) bool
}

func newPeerPoller(catalog peerPollerCatalog, events peerEventReader, sink peerEventSink, selfID string) *peerPoller {
	return &peerPoller{
		catalog: catalog, events: events, sink: sink, selfID: selfID,
		wait: peerPollWait, rescan: peerPollRescan, backoff: peerPollBackoff,
		maxBackoff: peerPollMaxBack, noSurface: peerPollNoSurface, sleep: sleepCtx,
	}
}

// Run polls until ctx ends. Started from serve's production block, never a
// constructor: it dials other machines.
func (p *peerPoller) Run(ctx context.Context) {
	var mu sync.Mutex
	running := make(map[string]context.CancelFunc)
	defer func() {
		mu.Lock()
		for _, cancel := range running {
			cancel()
		}
		mu.Unlock()
	}()

	for {
		machines, err := p.catalog.ListMachines(ctx)
		if err != nil {
			slog.Warn("peer poller: catalog unreadable", "error", err)
		} else {
			present := make(map[string]bool, len(machines))
			mu.Lock()
			for _, m := range machines {
				if m.MachineID == "" || m.MachineID == p.selfID {
					continue
				}
				present[m.MachineID] = true
				if _, ok := running[m.MachineID]; ok {
					continue
				}
				mctx, cancel := context.WithCancel(ctx)
				running[m.MachineID] = cancel
				go p.follow(mctx, m.MachineID)
			}
			// A machine that left the catalog stops being polled.
			for id, cancel := range running {
				if !present[id] {
					cancel()
					delete(running, id)
				}
			}
			mu.Unlock()
		}
		if !p.sleep(ctx, p.rescan) {
			return
		}
	}
}

// follow polls one machine until ctx ends.
func (p *peerPoller) follow(ctx context.Context, machineID string) {
	backoff := p.backoff
	for ctx.Err() == nil {
		err := p.pollOnce(ctx, machineID)
		switch {
		case err == nil:
			backoff = p.backoff
			continue
		case ctx.Err() != nil:
			return
		case errors.Is(err, peerlink.ErrNoPeerSurface), errors.Is(err, peerlink.ErrNotPaired):
			// Not going to change on a retry: an older release or a missing
			// pairing waits for an upgrade or a re-pair.
			slog.Debug("peer poller: machine cannot be polled", "machine", machineID, "error", err)
			if !p.sleep(ctx, p.noSurface) {
				return
			}
		default:
			slog.Debug("peer poller: poll failed", "machine", machineID, "error", err, "retry_in", backoff)
			if !p.sleep(ctx, backoff) {
				return
			}
			backoff = min(backoff*2, p.maxBackoff)
		}
	}
}

// pollOnce reads one batch and applies it.
func (p *peerPoller) pollOnce(ctx context.Context, machineID string) error {
	m, err := p.catalog.GetMachine(ctx, machineID)
	if err != nil {
		return err
	}

	if m.PeerCursor < 0 {
		// First contact: start from now. since past any seq returns nothing,
		// and latest is where the cursor goes.
		head, err := p.events.Events(ctx, machineID, math.MaxInt64, 0)
		if err != nil {
			return err
		}
		return p.setCursor(ctx, machineID, head.Latest)
	}

	batch, err := p.events.Events(ctx, machineID, m.PeerCursor, p.wait)
	if err != nil {
		return err
	}
	// The owner's log starting over — a wiped database, a fresh install at the
	// same identity — would otherwise leave this cursor ahead of every row it
	// will ever write, and nothing would arrive again.
	if len(batch.Events) == 0 && batch.Latest < m.PeerCursor {
		slog.Info("peer poller: machine's event log restarted; following from its head",
			"machine", machineID, "cursor", m.PeerCursor, "latest", batch.Latest)
		return p.setCursor(ctx, machineID, batch.Latest)
	}
	name := machineLabel(m)
	for _, ev := range batch.Events {
		p.apply(ctx, machineID, name, ev)
		if err := p.setCursor(ctx, machineID, ev.Seq); err != nil {
			return err
		}
	}
	return nil
}

// apply routes one event. An event of a kind this release does not know, or
// with a payload it cannot read, is skipped: the owner may be newer.
func (p *peerPoller) apply(ctx context.Context, machineID, machineName string, ev peer.Event) {
	var payload struct {
		Kind     string `json:"kind"`
		Headline string `json:"headline"`
		Name     string `json:"name"`
	}
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		slog.Warn("peer poller: unreadable event", "machine", machineID, "seq", ev.Seq, "error", err)
		return
	}
	sessionID := clampPeerField(ev.SessionID)
	switch ev.Kind {
	case peer.EventReport:
		report, err := assistant.ParseReport(payload.Kind, payload.Headline)
		if err != nil {
			slog.Warn("peer poller: invalid report", "machine", machineID, "seq", ev.Seq, "error", err)
			return
		}
		p.sink.PeerReport(ctx, machineName, sessionID, report)
	case peer.EventFinding:
		var finding struct {
			Kind     string         `json:"kind"`
			Subject  string         `json:"subject"`
			Severity string         `json:"severity"`
			Remedy   string         `json:"remedy"`
			Facts    map[string]any `json:"facts"`
			Opened   bool           `json:"opened"`
		}
		if err := json.Unmarshal(ev.Payload, &finding); err != nil || finding.Kind == "" {
			return
		}
		p.sink.PeerFinding(ctx, assistant.Finding{
			Kind: clampPeerField(finding.Kind), Subject: clampPeerField(finding.Subject),
			Severity: clampPeerField(finding.Severity), Remedy: clampPeerField(finding.Remedy),
			Facts: clampFacts(finding.Facts), Opened: finding.Opened, Machine: machineName,
		})
	case peer.EventTurnEnd:
		kind := assistant.NoticeKind(payload.Kind)
		switch kind {
		case assistant.NoticeFinished, assistant.NoticeFailed, assistant.NoticeBlocked:
		default:
			return
		}
		notice := assistant.Notice{Kind: kind, Headline: clampHeadline(payload.Headline)}
		p.sink.PeerTurnEnd(ctx, machineName, sessionID, clampPeerField(payload.Name), notice)
		if p.onTurnEnd != nil {
			p.onTurnEnd(machineID, sessionID)
		}
	}
}

func (p *peerPoller) setCursor(ctx context.Context, machineID string, seq int64) error {
	_, err := p.catalog.SetMachinePeerCursor(ctx, store.SetMachinePeerCursorParams{PeerCursor: seq, MachineID: machineID})
	return err
}

// clampFacts keeps a finding's facts to short scalars: they are another
// machine's values on their way into a journal row and a sentence.
func clampFacts(facts map[string]any) map[string]any {
	out := make(map[string]any, len(facts))
	for k, v := range facts {
		if len(out) >= 12 {
			break
		}
		switch t := v.(type) {
		case string:
			out[clampPeerField(k)] = clampPeerField(t)
		case float64, bool:
			out[clampPeerField(k)] = t
		}
	}
	return out
}

func clampHeadline(s string) string {
	if utf8.RuneCountInString(s) <= peerMaxHeadline {
		return s
	}
	return string([]rune(s)[:peerMaxHeadline]) + "…"
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// assistantPeerSink applies peer news to the assistant.
type assistantPeerSink struct{ svc *assistant.Service }

func (s assistantPeerSink) PeerReport(ctx context.Context, machineName, sessionID string, report assistant.Report) {
	s.svc.IngestPeerReport(ctx, machineName, sessionID, report)
}

func (s assistantPeerSink) PeerFinding(ctx context.Context, finding assistant.Finding) {
	s.svc.IngestFinding(ctx, finding)
}

func (s assistantPeerSink) PeerTurnEnd(ctx context.Context, machineName, sessionID, name string, notice assistant.Notice) {
	s.svc.IngestPeerTurnEnd(ctx, machineName, sessionID, name, notice)
}

// registryPeerSink applies peer news to a live call when the assistant itself
// is off: the call follows through the registry alone.
type registryPeerSink struct{ reg *assistant.Registry }

func (s registryPeerSink) PeerReport(_ context.Context, _, sessionID string, report assistant.Report) {
	if !s.reg.Listening(sessionID) {
		return
	}
	if _, err := s.reg.Deliver(sessionID, report); err != nil {
		slog.Warn("peer poller: report not delivered to the call", "session", sessionID, "error", err)
	}
}

// PeerFinding is not a live call's business: a call follows sessions, and a
// finding is about a machine. It reaches the assistant's journal when there is
// one.
func (s registryPeerSink) PeerFinding(context.Context, assistant.Finding) {}

func (s registryPeerSink) PeerTurnEnd(_ context.Context, _, sessionID, _ string, notice assistant.Notice) {
	s.reg.Notice(sessionID, notice)
}
