package brain

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mdjarv/agentique/backend/internal/memory"
	"github.com/mdjarv/agentique/backend/internal/memory/chroma"
	"github.com/mdjarv/agentique/backend/internal/memory/embedhttp"
)

// semanticConfig is the part of Config that names a vector backend. It is kept for the life of
// the Service because attaching is not a one-off: the backend can come up after boot, go away,
// and come back, and every attempt is built from these.
type semanticConfig struct {
	chromaURL   string
	embedURL    string
	embedModel  string
	embedAPIKey string
	collection  string
	// cosThresh and vetoScore are the operator's explicit thresholds; 0 means unset.
	cosThresh float64
	vetoScore float64
	calibrate bool
}

// configured reports whether a vector backend is named at all. Unconfigured is keyword mode by
// choice; configured-and-detached is keyword mode by failure, and only the second is news.
func (c semanticConfig) configured() bool {
	return c.chromaURL != "" && c.embedURL != "" && c.embedModel != ""
}

// semanticBackend is one attached vector backend and everything derived for it: the Chroma
// decorator over the shared cachestore, the embedder, and the thresholds that only mean
// something with an embedder. The Service holds exactly one through an atomic pointer, so a
// reader takes a single consistent snapshot — a store and the thresholds calibrated for it —
// and an attach or a detach replaces the whole set at once. Nothing in it is written after it
// is published except the synchronised state at the bottom.
type semanticBackend struct {
	// store is the chroma decorator. nil only in tests that exercise embedding alone, where
	// [Service.storeFor] falls back to the bare cache.
	store    memory.Store
	indexer  staleIndexer
	client   *chroma.Client
	embedder memory.Embedder
	// warmSrc loads existing vectors to seed the embed cache; nil when there is nothing to warm.
	warmSrc vectorWarmSource

	// cosThresh is the cosine link threshold and recall vouch bar; vetoScore the recall veto
	// floor; edgeThreshold the graph's kNN floor (the configured one, else cosThresh).
	cosThresh     float64
	vetoScore     float64
	edgeThreshold float64

	// dirty is set when a best-effort index write failed, so the next healthy probe catches the
	// collection up rather than leaving that fact unsearchable until a manual reindex.
	dirty atomic.Bool
	// warmMu serialises warmEmbedCache; warmed latches once a warm from this backend succeeded.
	warmMu sync.Mutex
	warmed bool
}

// staleIndexer is the catch-up capability the chroma decorator exposes.
type staleIndexer interface {
	IndexStale(ctx context.Context) (int, error)
}

// SemanticState is where semantic recall stands, as a closed set.
type SemanticState string

const (
	// SemanticOff: no vector backend is configured. Keyword recall is the chosen mode.
	SemanticOff SemanticState = "off"
	// SemanticConnecting: a backend is configured and an attach is under way — the boot
	// attempt, or one that reached the backend and is catching its index up.
	SemanticConnecting SemanticState = "connecting"
	// SemanticOn: attached; recall is hybrid.
	SemanticOn SemanticState = "on"
	// SemanticUnreachable: configured, not attached, retrying. Recall is keyword-only.
	SemanticUnreachable SemanticState = "unreachable"
)

// SemanticReason says which half of the backend failed, as a closed set. The error itself goes
// to the log; a surface gets the reason, because a status field is not the place for a dial
// error's text.
type SemanticReason string

const (
	ReasonChromaUnreachable   SemanticReason = "chroma-unreachable"
	ReasonEmbedderUnreachable SemanticReason = "embedder-unreachable"
	ReasonCollectionFailed    SemanticReason = "collection-failed"
	ReasonIndexFailed         SemanticReason = "index-failed"
)

// SemanticStatus is the truth about semantic recall right now.
type SemanticStatus struct {
	State SemanticState
	// Reason is set while State is unreachable.
	Reason SemanticReason
	// Since is when State last changed.
	Since time.Time
	// DownSince is when semantic recall was last lost — boot, when it has never attached. Zero
	// while on, and while off (nothing was lost).
	DownSince time.Time
}

// attachError is a failed attach attempt: the reason a surface shows and the error the log keeps.
type attachError struct {
	reason SemanticReason
	err    error
}

func (e *attachError) Error() string { return fmt.Sprintf("%s: %v", e.reason, e.err) }
func (e *attachError) Unwrap() error { return e.err }

// ErrSemanticNotConfigured is Connect's answer when no vector backend is configured.
var ErrSemanticNotConfigured = errors.New("brain: no vector backend configured (set chroma-url, embed-url and embed-model)")

// ErrSemanticUnavailable refuses a pass that persists similarity-derived structure — Related
// links, communities, areas — while the configured vector backend is detached. Recall degrades
// to keyword because it is on a model's latency path; consolidation is not, and it writes what
// it computes, so a lexical pass during an outage would rewrite the graph without embeddings
// and the next attached pass would rewrite it back.
var ErrSemanticUnavailable = errors.New("brain: the vector backend is configured but unreachable, so consolidation would rewrite links, communities and areas without embeddings")

// semanticTiming is the attach loop's clock. Fields on the Service rather than constants, so a
// test can run the loop in milliseconds.
type semanticTiming struct {
	// probeInterval is how often an attached backend is checked.
	probeInterval time.Duration
	// retryMin and retryMax bound the backoff between attach attempts while detached.
	retryMin, retryMax time.Duration
	// detachAfter is how many consecutive failed probes detach: one blip is not an outage.
	detachAfter int
	// probeTimeout bounds one probe (a heartbeat and one embed).
	probeTimeout time.Duration
}

func defaultSemanticTiming() semanticTiming {
	return semanticTiming{
		probeInterval: 30 * time.Second,
		retryMin:      5 * time.Second,
		retryMax:      2 * time.Minute,
		detachAfter:   2,
		probeTimeout:  10 * time.Second,
	}
}

// probeText is embedded on every probe. Anything short works; the answer is only checked for shape.
const probeText = "semantic recall probe"

// backend returns the attached vector backend, or nil in keyword mode.
func (s *Service) backend() *semanticBackend { return s.sem.Load() }

// storeFor is the store an operation on backend b reads and writes: the chroma decorator when
// attached, else the bare cache. Both front the same filestore, so a swap between two calls of
// one operation changes only whether a write is also indexed.
func (s *Service) storeFor(b *semanticBackend) memory.Store {
	if b != nil && b.store != nil {
		return b.store
	}
	return s.cache
}

// activeStore is storeFor the backend attached right now.
func (s *Service) activeStore() memory.Store { return s.storeFor(s.backend()) }

// SemanticEnabled reports whether vector recall is active right now.
func (s *Service) SemanticEnabled() bool { return s.backend() != nil }

// SemanticConfigured reports whether a vector backend is configured, attached or not.
func (s *Service) SemanticConfigured() bool { return s.semCfg.configured() }

// SemanticStatus reports where semantic recall stands.
func (s *Service) SemanticStatus() SemanticStatus {
	if !s.semCfg.configured() {
		return SemanticStatus{State: SemanticOff}
	}
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	return s.status
}

// setStatus records where semantic recall stands and, when that changed, tells onSemanticChange
// — outside the lock, since a listener is a broadcast.
func (s *Service) setStatus(state SemanticState, reason SemanticReason) {
	s.statusMu.Lock()
	now := time.Now().UTC()
	changed := s.status.State != state || s.status.Reason != reason
	if s.status.State != state {
		s.status.Since = now
	}
	switch {
	case state == SemanticOn:
		s.status.DownSince = time.Time{}
	case s.status.DownSince.IsZero():
		s.status.DownSince = now
	}
	s.status.State = state
	s.status.Reason = reason
	st := s.status
	s.statusMu.Unlock()
	if changed && s.onSemanticChange != nil {
		s.onSemanticChange(st)
	}
}

// EventBrainSemantic is pushed to every tab when semantic recall's state changes, carrying
// [SemanticStatus.Wire], so the Memory page's badge follows an attach or a detach live.
const EventBrainSemantic = "brain.semantic"

// Wire is the status as /api/brain/status and the brain.semantic push carry it. `semantic` is
// whether recall is hybrid, kept for readers that know only it; the rest say why not. Kept in
// sync by hand with brain-api.ts.
func (st SemanticStatus) Wire() map[string]any {
	out := map[string]any{
		"semantic":      st.State == SemanticOn,
		"semanticState": st.State,
	}
	if st.Reason != "" {
		out["semanticReason"] = st.Reason
	}
	if !st.DownSince.IsZero() {
		out["semanticDownSince"] = st.DownSince.UTC().Format(time.RFC3339)
	}
	return out
}

// nextAttach returns a channel closed by the next successful attach. Take it BEFORE checking
// whether the backend is attached, so an attach landing between the two is not missed.
func (s *Service) nextAttach() <-chan struct{} {
	s.attachMu.Lock()
	defer s.attachMu.Unlock()
	if s.attachCh == nil {
		s.attachCh = make(chan struct{})
	}
	return s.attachCh
}

func (s *Service) announceAttach() {
	s.attachMu.Lock()
	defer s.attachMu.Unlock()
	if s.attachCh != nil {
		close(s.attachCh)
	}
	s.attachCh = make(chan struct{})
}

// Connect makes one attempt to attach the configured vector backend, synchronously, and
// performs no index catch-up — the shape a one-shot CLI command wants before it reindexes or
// calibrates. The server does not call it: it runs [Service.RunSemantic], which retries and
// catches up. Returns ErrSemanticNotConfigured when no backend is named, nil when one is
// already attached.
func (s *Service) Connect(ctx context.Context) error {
	if !s.semCfg.configured() {
		return ErrSemanticNotConfigured
	}
	return s.attach(ctx, false)
}

// RunSemantic keeps the configured vector backend attached until ctx ends. The first attempt is
// immediate, so a backend that is up at boot is attached before anything waits on it. While
// detached it retries with backoff; while attached it probes, heals index drift left by failed
// writes, and detaches after consecutive failed probes so the status and recall stop claiming
// a backend that is gone. A no-op when nothing is configured.
//
// Started from serve's production block, never from New: it dials the network and writes to a
// shared collection, and a constructor a test calls must do neither.
func (s *Service) RunSemantic(ctx context.Context) {
	if !s.semCfg.configured() {
		return
	}
	t := s.timing
	backoff := t.retryMin
	failures := 0
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}

		var wait time.Duration
		if b := s.backend(); b == nil {
			err := s.attach(ctx, true)
			switch {
			case ctx.Err() != nil:
				return
			case err != nil:
				wait = backoff
				backoff = min(backoff*2, t.retryMax)
			default:
				backoff, failures = t.retryMin, 0
				wait = t.probeInterval
			}
		} else {
			wait = t.probeInterval
			if err := s.probe(ctx, b); err != nil {
				if ctx.Err() != nil {
					return
				}
				failures++
				slog.Warn("brain: vector backend probe failed", "consecutive", failures, "error", err)
				if failures >= t.detachAfter {
					s.detach(b, err)
					failures, backoff = 0, t.retryMin
					wait = t.retryMin
				}
			} else {
				failures = 0
				s.healIndex(ctx, b)
			}
		}
		timer.Reset(wait)
	}
}

// probe checks both halves of backend b: Chroma answers its heartbeat and the embedder returns
// one vector. The boot probe used to ask Chroma alone, which called a backend with a dead
// embedder semantic and then failed every search.
func (s *Service) probe(ctx context.Context, b *semanticBackend) error {
	return probeBackend(ctx, s.timing.probeTimeout, b.client, b.embedder)
}

func probeBackend(ctx context.Context, timeout time.Duration, client *chroma.Client, emb memory.Embedder) error {
	pctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := client.Heartbeat(pctx); err != nil {
		return &attachError{reason: ReasonChromaUnreachable, err: err}
	}
	vecs, err := emb.Embed(pctx, []string{probeText})
	if err == nil && (len(vecs) != 1 || len(vecs[0]) == 0) {
		err = fmt.Errorf("embedder returned %d vectors for one probe text", len(vecs))
	}
	if err != nil {
		return &attachError{reason: ReasonEmbedderUnreachable, err: err}
	}
	return nil
}

// attach makes one attach attempt. With catchUp it indexes whatever the collection is missing
// before publishing — a fresh Chroma holds no vectors, and writes made while detached were never
// indexed — so semantic recall switches on over a complete index, then catches up once more for
// the writes that were in flight across the swap.
func (s *Service) attach(ctx context.Context, catchUp bool) (err error) {
	s.connectMu.Lock()
	defer s.connectMu.Unlock()
	if s.backend() != nil {
		return nil
	}
	defer func() {
		if err == nil || ctx.Err() != nil {
			return
		}
		reason := ReasonChromaUnreachable
		var ae *attachError
		if errors.As(err, &ae) {
			reason = ae.reason
		}
		s.noteAttachFailure(reason, err)
	}()

	cfg := s.semCfg
	client := chroma.NewClient(cfg.chromaURL)
	emb := embedhttp.New(cfg.embedURL, cfg.embedModel, embedhttp.WithAPIKey(cfg.embedAPIKey))
	if err := probeBackend(ctx, s.timing.probeTimeout, client, emb); err != nil {
		return err
	}

	coll := cfg.collection
	if coll == "" {
		coll = defaultCollection
	}
	b := &semanticBackend{client: client, embedder: emb}
	cs, err := chroma.NewStore(ctx, s.cache, client, emb, coll, chroma.WithErrorHandler(func(e error) {
		slog.Warn("brain: vector index degraded", "error", e)
		b.dirty.Store(true)
	}))
	if err != nil {
		return &attachError{reason: ReasonCollectionFailed, err: err}
	}
	b.store, b.indexer, b.warmSrc = cs, cs, cs

	if catchUp {
		s.setStatus(SemanticConnecting, "")
		start := time.Now()
		n, err := cs.IndexStale(ctx)
		if err != nil {
			return &attachError{reason: ReasonIndexFailed, err: err}
		}
		if n > 0 {
			slog.Info("brain: indexed facts the vector collection was missing", "facts", n, "took", time.Since(start).Round(time.Millisecond))
		}
	}

	b.cosThresh = cfg.cosThresh
	if b.cosThresh <= 0 {
		b.cosThresh = memory.DefaultSemanticThreshold
	}
	b.vetoScore = cfg.vetoScore
	if b.vetoScore <= 0 {
		b.vetoScore = memory.DefaultVectorVetoScore
	}
	// Calibration embeds the whole corpus under a two-minute bound, so it runs on the first
	// attach of the process only: a flapping backend must not repeat it on every flap. Later
	// attaches reuse what it derived.
	if cfg.calibrate && !s.calibrationTried {
		s.calibrationTried = true
		s.calibrated = s.calibrateThresholds(ctx, b)
	}
	if th := s.calibrated; th != nil {
		if cfg.cosThresh <= 0 {
			b.cosThresh = th.CosineThreshold
		}
		if cfg.vetoScore <= 0 {
			b.vetoScore = th.VectorVeto
		}
	}
	// An unset graph edge threshold falls back to the (possibly calibrated) recall "related"
	// line, so the graph and recall share one cosine floor.
	b.edgeThreshold = s.graph.EdgeThreshold
	if b.edgeThreshold <= 0 {
		b.edgeThreshold = b.cosThresh
	}

	s.sem.Store(b)
	s.setStatus(SemanticOn, "")
	s.announceAttach()
	s.loggedDown.Store(false)
	slog.Info("brain: semantic recall enabled", "collection", coll, "cosineThreshold", b.cosThresh, "vectorVeto", b.vetoScore)

	if catchUp {
		// Writes that loaded the keyword store before the swap were not indexed. Every writer that
		// holds a store for long holds s.mu, so passing through it waits them out; the rest are
		// single writes, and the next dirty-heal covers any that still slip past.
		s.mu.Lock()
		s.mu.Unlock()
		b.dirty.Store(true)
		s.healIndex(ctx, b)
	}
	return nil
}

// healIndex catches the collection up when a write to it failed since the last catch-up.
func (s *Service) healIndex(ctx context.Context, b *semanticBackend) {
	if b.indexer == nil || !b.dirty.CompareAndSwap(true, false) {
		return
	}
	n, err := b.indexer.IndexStale(ctx)
	if err != nil {
		b.dirty.Store(true)
		slog.Warn("brain: vector index catch-up failed; retrying on the next probe", "error", err)
		return
	}
	if n > 0 {
		slog.Info("brain: re-indexed facts whose vector writes had failed", "facts", n)
	}
}

// detach drops backend b, if it is still the attached one, back to keyword recall.
func (s *Service) detach(b *semanticBackend, cause error) {
	if !s.sem.CompareAndSwap(b, nil) {
		return
	}
	reason := ReasonChromaUnreachable
	var ae *attachError
	if errors.As(cause, &ae) {
		reason = ae.reason
	}
	s.setStatus(SemanticUnreachable, reason)
	s.loggedDown.Store(true)
	slog.Warn("brain: vector backend lost; using keyword recall until it answers again", "reason", reason, "error", cause)
}

// noteAttachFailure records a failed attempt. The first failure of a detached period is a
// warning; the retries after it log at debug, because the status and the steward carry the
// ongoing fact and a warning every two minutes for a night teaches the log's reader to skip it.
func (s *Service) noteAttachFailure(reason SemanticReason, err error) {
	s.setStatus(SemanticUnreachable, reason)
	if s.loggedDown.CompareAndSwap(false, true) {
		slog.Warn("brain: vector backend unreachable; using keyword recall and retrying", "reason", reason, "error", err)
		return
	}
	slog.Debug("brain: vector backend still unreachable", "reason", reason, "error", err)
}

// calibrationTimeout bounds the corpus embed behind auto-calibration so it can't hang an attach
// if the embedder is slow or wedged.
const calibrationTimeout = 2 * time.Minute

// calibrateThresholds derives the model-specific cosine/veto thresholds from the live corpus
// through backend b. Best-effort: any failure (no corpus, thin corpus, embed error) returns nil
// and logs why, and the attach keeps the configured or default thresholds.
func (s *Service) calibrateThresholds(ctx context.Context, b *semanticBackend) *memory.CalibrationThresholds {
	cctx, cancel := context.WithTimeout(ctx, calibrationTimeout)
	defer cancel()
	res, err := s.calibrate(cctx, b)
	if err != nil {
		slog.Warn("brain: auto-calibration failed; keeping thresholds", "error", err)
		return nil
	}
	if !res.OK {
		slog.Warn("brain: auto-calibration skipped (corpus too thin); keeping thresholds", "pairs", res.Sample.Len())
		return nil
	}
	slog.Info("brain: semantic thresholds auto-calibrated",
		"pairs", res.Sample.Len(),
		"p25", res.Sample.Percentile(0.25), "p50", res.Sample.Percentile(0.50),
		"p99", res.Sample.Percentile(0.99),
		"cosineThreshold", res.Thresholds.CosineThreshold, "vectorVeto", res.Thresholds.VectorVeto)
	th := res.Thresholds
	return &th
}

// veto is backend b's recall veto floor, 0 (memory.Recall's default, inert without a
// Searcher) in keyword mode.
func (b *semanticBackend) veto() float64 {
	if b == nil {
		return 0
	}
	return b.vetoScore
}

// vouch is backend b's recall vouch bar, 0 in keyword mode.
func (b *semanticBackend) vouch() float64 {
	if b == nil {
		return 0
	}
	return b.cosThresh
}
